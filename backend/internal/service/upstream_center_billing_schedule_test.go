//go:build unit

package service

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type upstreamScheduledBillingRepo struct {
	UpstreamFinanceRepository
	target   *UpstreamFinanceTarget
	next     time.Time
	token    string
	snapshot *UpstreamBalanceSnapshot
}

func (r *upstreamScheduledBillingRepo) GetTarget(context.Context, int64) (*UpstreamFinanceTarget, error) {
	return r.target, nil
}
func (r *upstreamScheduledBillingRepo) DueBalanceTargetIDs(_ context.Context, now time.Time, _ int) ([]int64, error) {
	if r.token == "" && !r.next.After(now) {
		return []int64{r.target.ID}, nil
	}
	return nil, nil
}
func (r *upstreamScheduledBillingRepo) ClaimBalance(_ context.Context, _ int64, token string, _, _ time.Time) (bool, error) {
	if r.token != "" {
		return false, nil
	}
	r.token = token
	return true, nil
}
func (r *upstreamScheduledBillingRepo) SaveBalance(_ context.Context, _ *UpstreamFinanceTarget, _ string, snapshot *UpstreamBalanceSnapshot, token string, next time.Time) error {
	if r.token != token {
		return fmt.Errorf("unexpected lease token")
	}
	r.snapshot, r.next, r.token = snapshot, next, ""
	return nil
}
func (r *upstreamScheduledBillingRepo) ReleaseBalance(_ context.Context, _ int64, token string, next time.Time) error {
	if r.token == token {
		r.next, r.token = next, ""
	}
	return nil
}
func (r *upstreamScheduledBillingRepo) LatestBalance(context.Context, int64, string) (*UpstreamBalanceSnapshot, error) {
	return r.snapshot, nil
}
func (r *upstreamScheduledBillingRepo) Summary(context.Context, UpstreamFinanceQuery) (*UpstreamFinanceSummary, error) {
	return &UpstreamFinanceSummary{}, nil
}

type upstreamBillingOverviewRepo struct {
	UpstreamCenterRepository
	target *UpstreamTarget
}

func (r *upstreamBillingOverviewRepo) ListSuppliers(context.Context) ([]*UpstreamSupplier, error) {
	return []*UpstreamSupplier{}, nil
}
func (r *upstreamBillingOverviewRepo) ListTargets(context.Context) ([]*UpstreamTarget, error) {
	copy := *r.target
	return []*UpstreamTarget{&copy}, nil
}
func (r *upstreamBillingOverviewRepo) PopulateStatistics(context.Context, []*UpstreamTarget, time.Time) error {
	return nil
}

func TestUpstreamIndependentScheduledBillingAndReadOnlyOverview(t *testing.T) {
	for _, embedded := range []bool{false, true} {
		t.Run(fmt.Sprintf("embedded_billing_%t", embedded), func(t *testing.T) {
			now := time.Now().UTC()
			repo := &upstreamScheduledBillingRepo{target: &UpstreamFinanceTarget{ID: 7, Provider: "openai", Endpoint: "https://8.8.8.8", APIKeyEncrypted: "test-secret", WalletRef: "default"}, next: now}
			finance := NewUpstreamFinanceService(repo, financeTestCipher{}, nil, nil, nil)
			finance.now = func() time.Time { return now }
			rate := .42
			paths := []string{}
			finance.client.Transport = financeRoundTrip(func(req *http.Request) (*http.Response, error) {
				if req.Header.Get("Authorization") != "Bearer test-secret" || req.Method != http.MethodGet {
					return nil, fmt.Errorf("unexpected billing request authentication or method")
				}
				paths = append(paths, req.URL.Path)
				body := ""
				switch req.URL.Path {
				case "/v1/usage":
					body = `{"balance":10}`
					if embedded {
						body = `{"balance":10,"billing":` + remoteBillingFixture(rate, now) + `}`
					}
				case "/v1/sub2api/billing":
					body = remoteBillingFixture(rate, now)
				default:
					return nil, fmt.Errorf("unexpected outbound path %s", req.URL.Path)
				}
				return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}, nil
			})
			center := NewUpstreamCenterService(&upstreamBillingOverviewRepo{target: &UpstreamTarget{ID: 7, APIKeyEncrypted: "test-secret", Models: []string{"gpt-5.6-sol"}}}, financeTestCipher{}, nil, finance)
			before, err := center.Overview(context.Background(), "24h")
			require.NoError(t, err)
			require.Len(t, before.Monitors, 1)
			require.Equal(t, "pending", before.Monitors[0].Balance.Billing.Status)
			require.Nil(t, before.Monitors[0].Balance.Billing.EffectiveRateMultiplier)
			require.Empty(t, paths, "loading an unsynchronized monitor must not perform external I/O")
			finance.SyncDueBalances(context.Background())
			require.NotNil(t, repo.snapshot)
			require.Equal(t, "ok", repo.snapshot.Billing.Status)
			require.Equal(t, rate, *repo.snapshot.Billing.EffectiveRateMultiplier)
			expectedPaths := []string{"/v1/usage"}
			if !embedded {
				expectedPaths = append(expectedPaths, "/v1/sub2api/billing")
			}
			require.Equal(t, expectedPaths, paths, "reuse complete embedded billing without a redundant request")
			require.Equal(t, now.Add(time.Minute), repo.next)
			now = now.Add(30 * time.Second)
			finance.SyncDueBalances(context.Background())
			require.Equal(t, expectedPaths, paths, "balance cadence stays independent of the 30-second probe")
			now = now.Add(30 * time.Second)
			rate = 0
			finance.SyncDueBalances(context.Background())
			require.Len(t, paths, 2*len(expectedPaths))
			require.Equal(t, float64(0), *repo.snapshot.Billing.EffectiveRateMultiplier, "periodic refresh must preserve an upstream-declared zero multiplier")
			calls := len(paths)
			after, err := center.Overview(context.Background(), "24h")
			require.NoError(t, err)
			require.Equal(t, float64(0), *after.Monitors[0].Balance.Billing.EffectiveRateMultiplier)
			require.False(t, after.Monitors[0].Balance.Billing.Stale)
			require.Len(t, paths, calls, "overview must read the stored snapshot without external I/O")
		})
	}
}
