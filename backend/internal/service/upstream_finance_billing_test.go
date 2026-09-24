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

func remoteBillingFixture(rate float64, at time.Time) string {
	return fmt.Sprintf(`{"object":"sub2api.key_billing","schema_version":1,"billing_scope":"token","group_rate_multiplier":%g,"resolved_rate_multiplier":%g,"effective_rate_multiplier":%g,"peak_rate_enabled":false,"observed_at":%q}`, rate, rate, rate, at.Format(time.RFC3339Nano))
}

func TestUpstreamFinanceRemoteBillingZeroUnknownAndChanges(t *testing.T) {
	now := time.Date(2026, 9, 23, 8, 0, 0, 0, time.UTC)
	for _, rate := range []float64{.75, 0, .15} {
		snapshot, err := parseUpstreamRemoteBilling([]byte(remoteBillingFixture(rate, now)), "sub2api_billing")
		require.NoError(t, err)
		require.NotNil(t, snapshot.EffectiveRateMultiplier)
		require.Equal(t, rate, *snapshot.EffectiveRateMultiplier)
		require.Nil(t, snapshot.GroupID)
		require.Nil(t, snapshot.GroupName)
		require.Equal(t, "sub2api_billing", snapshot.Source)
	}
	for _, body := range []string{
		`{}`, `{"group_rate_multiplier":1}`, `{"cost":2,"actual_cost":1}`,
		strings.Replace(remoteBillingFixture(.5, now), `"resolved_rate_multiplier":0.5,`, "", 1),
		strings.Replace(remoteBillingFixture(.5, now), `"effective_rate_multiplier":0.5`, `"effective_rate_multiplier":0.7`, 1),
		remoteBillingFixture(-1, now),
	} {
		_, err := parseUpstreamRemoteBilling([]byte(body), "sub2api_billing")
		require.Error(t, err, body)
	}
	pending := pendingUpstreamRemoteBilling()
	require.Nil(t, pending.EffectiveRateMultiplier)
	require.True(t, pending.Stale)
}

func TestUpstreamFinanceRemoteBillingPersonalAndPeak(t *testing.T) {
	body := `{"object":"sub2api.key_billing","schema_version":1,"billing_scope":"token","group_id":7,"group_name":"上游专属","group_rate_multiplier":0.8,"user_rate_multiplier":0.5,"resolved_rate_multiplier":0.5,"effective_rate_multiplier":0.75,"peak_rate_enabled":true,"peak_start":"08:00","peak_end":"12:00","peak_rate_multiplier":1.5,"applied_peak_multiplier":1.5,"timezone":"UTC","observed_at":"2026-09-23T09:00:00Z"}`
	snapshot, err := parseUpstreamRemoteBilling([]byte(body), "sub2api_billing")
	require.NoError(t, err)
	require.Equal(t, int64(7), *snapshot.GroupID)
	require.Equal(t, "上游专属", *snapshot.GroupName)
	require.Equal(t, .8, *snapshot.GroupRateMultiplier)
	require.Equal(t, .5, *snapshot.ResolvedRateMultiplier)
	require.Equal(t, .75, *snapshot.EffectiveRateMultiplier)
	usage := parseUpstreamBillingFromUsage([]byte(`{"balance":20,"billing":` + body + `}`))
	require.NotNil(t, usage)
	require.Equal(t, "sub2api_usage", usage.Source)
	require.Nil(t, parseUpstreamBillingFromUsage([]byte(`{"usage":{"today":{"cost":20,"actual_cost":10}}}`)), "consumption ratios cannot identify a current rate")
}

func TestUpstreamFinanceRemoteBillingFreshness(t *testing.T) {
	now := time.Now().UTC()
	last := now.Add(-time.Minute)
	snapshot := &UpstreamRemoteBillingSnapshot{Status: "ok", SyncedAt: &last, ObservedAt: &last, EffectiveRateMultiplier: financeFloat(.4)}
	markUpstreamRemoteBillingStale(snapshot, now)
	require.False(t, snapshot.Stale)
	snapshot.Status = "error"
	snapshot.LastAttemptAt = &now
	markUpstreamRemoteBillingStale(snapshot, now)
	require.True(t, snapshot.Stale)
	require.Equal(t, .4, *snapshot.EffectiveRateMultiplier)
	require.Equal(t, last, *snapshot.SyncedAt)
	snapshot.Status = "ok"
	markUpstreamRemoteBillingStale(snapshot, now.Add(3*time.Minute))
	require.True(t, snapshot.Stale)
}

func TestUpstreamFinanceRemoteBillingEndpointAndFailure(t *testing.T) {
	now := time.Now().UTC()
	svc := NewUpstreamFinanceService(nil, financeTestCipher{}, nil, nil, nil)
	svc.now = func() time.Time { return now }
	target := &UpstreamFinanceTarget{ID: 1, Endpoint: "https://8.8.8.8/prefix/v1", APIKeyEncrypted: "private-test-key"}
	calls := 0
	svc.client.Transport = financeRoundTrip(func(req *http.Request) (*http.Response, error) {
		calls++
		require.Equal(t, "https://8.8.8.8/prefix/v1/sub2api/billing", req.URL.String())
		require.Equal(t, "Bearer private-test-key", req.Header.Get("Authorization"))
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(remoteBillingFixture(0, now))), Header: http.Header{}}, nil
	})
	got := svc.fetchRemoteBilling(context.Background(), target)
	require.Equal(t, "ok", got.Status)
	require.NotNil(t, got.EffectiveRateMultiplier)
	require.Zero(t, *got.EffectiveRateMultiplier)
	require.False(t, got.Stale)
	svc.client.Transport = financeRoundTrip(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 404, Body: io.NopCloser(strings.NewReader("private-test-key")), Header: http.Header{}}, nil
	})
	got = svc.fetchRemoteBilling(context.Background(), target)
	require.Equal(t, "unsupported", got.Status)
	require.Nil(t, got.EffectiveRateMultiplier)
	require.Equal(t, "billing_endpoint_unsupported", got.Error)
	require.Nil(t, got.SyncedAt)
	require.Equal(t, now, *got.LastAttemptAt)
	target.Endpoint = "https://127.0.0.1"
	got = svc.fetchRemoteBilling(context.Background(), target)
	require.Equal(t, "endpoint_unavailable", got.Error)
	require.Equal(t, 1, calls)
}
