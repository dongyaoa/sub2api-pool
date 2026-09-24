package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestUpstreamFinanceParseBalances(t *testing.T) {
	tests := []struct {
		name, body, kind, currency   string
		balance, quota, today, total *float64
	}{
		{"wallet", `{"mode":"unrestricted","balance":0,"remaining":0,"unit":"USD","usage":{"today":{"actual_cost":1.25,"cost":999},"total":{"actual_cost":9}}}`, "wallet", "USD", financeFloat(0), nil, financeFloat(1.25), financeFloat(9)},
		{"key quota", `{"mode":"quota_limited","remaining":6,"quota":{"remaining":6,"unit":"USD"},"usage":{"today":{"actual_cost":"0.2"}}}`, "key_quota", "USD", nil, financeFloat(6), financeFloat(.2), nil},
		{"subscription", `{"mode":"unrestricted","remaining":30,"subscription":{"daily_usage_usd":2}}`, "subscription", "USD", nil, financeFloat(30), nil, nil},
		{"rate limited without total", `{"mode":"quota_limited","rate_limits":[{"window":"1d","remaining":4}]}`, "key_quota", "USD", nil, nil, nil, nil},
		{"envelope", `{"code":0,"data":{"mode":"unrestricted","balance":"5.5","unit":"CNY"}}`, "wallet", "CNY", financeFloat(5.5), nil, nil, nil},
		{"ambiguous remaining", `{"mode":"unrestricted","remaining":50,"usage":{"total":{"actual_cost":8}}}`, "unknown", "USD", nil, nil, nil, financeFloat(8)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseUpstreamUsage([]byte(tt.body))
			require.NoError(t, err)
			require.Equal(t, tt.kind, got.Kind)
			require.Equal(t, tt.currency, got.Currency)
			require.Equal(t, tt.balance, got.Balance)
			require.Equal(t, tt.quota, got.QuotaRemaining)
			require.Equal(t, tt.today, got.TodayUsed)
			require.Equal(t, tt.total, got.TotalUsed)
		})
	}
}

func TestUpstreamFinanceRejectsFalseZeroAndErrors(t *testing.T) {
	for _, body := range []string{`{}`, `{"message":"unauthorized"}`, `{"error":{"message":"secret"}}`, `{"balance":"NaN"}`, `{"balance":1e50}`, `{"balance":1} {}`, `{"code":401,"data":{"balance":10}}`, `{"code":401,"balance":10}`, `{"mode":"unrestricted","remaining":5}`, `{"isValid":false,"balance":10}`, `{"success":false,"data":{"balance":10}}`, `{"isValid":false,"data":{"balance":10}}`, `{"error":{"message":"unauthorized"},"data":{"balance":10}}`, `{"data":{"success":false,"balance":10}}`} {
		_, err := parseUpstreamUsage([]byte(body))
		require.Error(t, err, body)
	}
	got, err := parseUpstreamUsage([]byte(`{"balance":2,"usage":{"today":{"actual_cost":"bad"},"total":{"cost":99}}}`))
	require.NoError(t, err)
	require.Nil(t, got.TodayUsed)
	require.Nil(t, got.TotalUsed)
	require.Equal(t, "sub2api_default", got.CurrencySource)
	got, err = parseUpstreamUsage([]byte(`{"success":true,"error":null,"data":{"balance":10}}`))
	require.NoError(t, err)
	require.Equal(t, 10.0, *got.Balance)
}

func TestUpstreamFinanceEndpointNormalization(t *testing.T) {
	for _, endpoint := range []string{"https://example.com", "https://example.com/v1/", "https://example.com/v1/responses", "https://example.com/v1/chat/completions", "https://example.com/v1beta"} {
		got, err := upstreamUsageURL(endpoint)
		require.NoError(t, err)
		require.Equal(t, "https://example.com/v1/usage", got)
	}
	got, err := upstreamUsageURL("https://example.com/prefix/v1/messages")
	require.NoError(t, err)
	require.Equal(t, "https://example.com/prefix/v1/usage", got)
	for _, endpoint := range []string{"http://example.com", "https://user:secret@example.com", "https://example.com?key=secret", "https://example.com#fragment"} {
		_, err := upstreamUsageURL(endpoint)
		require.Error(t, err)
	}
}

type financeTestCipher struct{}

func (financeTestCipher) Encrypt(s string) (string, error) { return s, nil }
func (financeTestCipher) Decrypt(s string) (string, error) {
	if s == "broken" {
		return "", errors.New("internal secret")
	}
	return s, nil
}

type financeRoundTrip func(*http.Request) (*http.Response, error)

func (f financeRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestUpstreamFinanceNetworkRedactionAndRedirect(t *testing.T) {
	svc := NewUpstreamFinanceService(nil, financeTestCipher{}, nil, nil, nil)
	calls := 0
	svc.client.Transport = financeRoundTrip(func(req *http.Request) (*http.Response, error) {
		calls++
		require.Equal(t, "Bearer test-secret", req.Header.Get("Authorization"))
		return &http.Response{StatusCode: 302, Header: http.Header{"Location": []string{"https://example.org/collect"}}, Body: io.NopCloser(strings.NewReader("test-secret")), Request: req}, nil
	})
	got := svc.fetchBalance(context.Background(), &UpstreamFinanceTarget{ID: 1, Endpoint: "https://8.8.8.8/v1", APIKeyEncrypted: "test-secret"})
	require.Equal(t, 1, calls)
	require.Equal(t, "upstream_http_302", got.Error)
	require.Nil(t, got.Balance)
	got = svc.fetchBalance(context.Background(), &UpstreamFinanceTarget{ID: 1, Endpoint: "https://127.0.0.1", APIKeyEncrypted: "test-secret"})
	require.Equal(t, 1, calls)
	require.Equal(t, "endpoint_unavailable", got.Error)
	svc.client.Transport = financeRoundTrip(func(req *http.Request) (*http.Response, error) {
		return nil, errors.New("test-secret https://private.local")
	})
	got = svc.fetchBalance(context.Background(), &UpstreamFinanceTarget{ID: 1, Endpoint: "https://8.8.8.8", APIKeyEncrypted: "test-secret"})
	require.Equal(t, "upstream_request_failed", got.Error)
}

type financeTestRepo struct {
	UpstreamFinanceRepository
	ids []int64
}

func (r *financeTestRepo) ActiveAccountIDs(context.Context, int64) ([]int64, error) {
	return r.ids, nil
}

type financeAccounts struct {
	AccountRepository
	accounts map[int64]*Account
}

func (r *financeAccounts) GetByID(_ context.Context, id int64) (*Account, error) {
	return r.accounts[id], nil
}

func TestUpstreamFinanceMonitorCostUsesProcurementRate(t *testing.T) {
	billing := NewBillingService(nil, nil)
	repo := &financeTestRepo{ids: []int64{1}}
	accounts := &financeAccounts{accounts: map[int64]*Account{1: {ID: 1, RateMultiplier: financeFloat(.25)}}}
	svc := NewUpstreamFinanceService(repo, financeTestCipher{}, billing, nil, accounts)
	tokens := UsageTokens{InputTokens: 100, OutputTokens: 10, CacheReadTokens: 50}
	base := tryModelFilePricing(billing, "claude-3-5-sonnet", tokens, "", time.Now())
	require.NotNil(t, base)
	cost, err := svc.EstimateMonitorCost(context.Background(), 1, "claude-3-5-sonnet", tokens)
	require.NoError(t, err)
	require.NotNil(t, cost)
	require.InDelta(t, *base*.25, *cost, 1e-12)
	cost, err = svc.EstimateMonitorCost(context.Background(), 1, "made-up-claude-model", tokens)
	require.NoError(t, err)
	require.Nil(t, cost)
	repo.ids = []int64{1, 2}
	accounts.accounts[2] = &Account{ID: 2, RateMultiplier: financeFloat(.5)}
	cost, err = svc.EstimateMonitorCost(context.Background(), 1, "claude-3-5-sonnet", tokens)
	require.NoError(t, err)
	require.Nil(t, cost, "conflicting procurement rates must not pick the first")
}

func TestUpstreamFinanceQueryBounds(t *testing.T) {
	now := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)
	q, err := normalizeUpstreamFinanceQuery(UpstreamFinanceQuery{}, now)
	require.NoError(t, err)
	require.Equal(t, 50, q.PageSize)
	require.True(t, q.From.Before(q.To))
	_, err = normalizeUpstreamFinanceQuery(UpstreamFinanceQuery{From: now, To: now}, now)
	require.Error(t, err)
	_, err = normalizeUpstreamFinanceQuery(UpstreamFinanceQuery{From: now.AddDate(-2, 0, 0), To: now}, now)
	require.Error(t, err)
}

func financeFloat(v float64) *float64 { return &v }
