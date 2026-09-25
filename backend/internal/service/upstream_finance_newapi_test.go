package service

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func newAPIFixtures() map[string]string {
	return map[string]string{
		"/api/usage/token/":     `{"code":true,"data":{"object":"token_usage","total_available":1500000,"total_used":250000,"unlimited_quota":false}}`,
		"/api/status":           `{"success":true,"data":{"quota_per_unit":500000,"quota_display_type":"CNY","usd_exchange_rate":7.3}}`,
		"/api/user/self":        `{"success":true,"data":{"id":42,"quota":10000000,"group":"vip"}}`,
		"/api/token/search":     `{"success":true,"data":{"total":1,"items":[{"id":13,"user_id":42,"group":"premium","key":"never-store-this-key","used_quota":250000,"remain_quota":1500000,"unlimited_quota":false}]}}`,
		"/api/token/13":         `{"success":true,"data":{"id":13,"user_id":42,"group":"premium","used_quota":250000,"remain_quota":1500000,"unlimited_quota":false}}`,
		"/api/user/self/groups": `{"success":true,"data":{"premium":{"ratio":0.35,"desc":"VIP price"},"vip":{"ratio":0},"auto":{"ratio":"自动"}}}`,
	}
}

func newAPIFinanceFixture(t *testing.T, responses map[string]string, authorized bool) (*UpstreamFinanceService, *UpstreamFinanceTarget, *[]string) {
	t.Helper()
	svc := NewUpstreamFinanceService(nil, financeTestCipher{}, nil, nil, nil)
	target := &UpstreamFinanceTarget{ID: 7, Endpoint: "https://8.8.8.8/prefix/v1/responses", Provider: "openai", WalletRef: "default", APIKeyEncrypted: "sk-inference-secret"}
	if authorized {
		target.NewAPIUserID, target.NewAPIAccessTokenEncrypted = 42, "console-secret"
	}
	paths := []string{}
	svc.client.Transport = financeRoundTrip(func(req *http.Request) (*http.Response, error) {
		require.Equal(t, http.MethodGet, req.Method)
		require.Equal(t, "8.8.8.8", req.URL.Host)
		require.True(t, strings.HasPrefix(req.URL.Path, "/prefix/"))
		path := strings.TrimPrefix(req.URL.Path, "/prefix")
		paths = append(paths, path)
		switch path {
		case "/api/status":
			require.Empty(t, req.Header.Get("Authorization"))
			require.Empty(t, req.Header.Get("New-Api-User"))
		case "/api/user/self", "/api/token/search", "/api/token/13", "/api/user/self/groups":
			require.Equal(t, "Bearer console-secret", req.Header.Get("Authorization"))
			require.Equal(t, "42", req.Header.Get("New-Api-User"))
		default:
			require.Equal(t, "Bearer sk-inference-secret", req.Header.Get("Authorization"))
			require.Empty(t, req.Header.Get("New-Api-User"))
		}
		if path == "/api/token/search" {
			require.Equal(t, "sk-inference-secret", req.URL.Query().Get("token"))
		}
		body, exists := responses[path]
		status := http.StatusOK
		if !exists {
			status = http.StatusNotFound
		}
		return &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
	})
	return svc, target, &paths
}

func TestUpstreamFinanceNewAPIAutoDetectionKeyQuota(t *testing.T) {
	svc, target, paths := newAPIFinanceFixture(t, newAPIFixtures(), false)
	got := svc.fetchBalance(context.Background(), target)
	require.Equal(t, []string{"/v1/usage", "/api/usage/token/", "/api/status"}, *paths)
	require.Equal(t, "key_quota", got.Kind)
	require.Equal(t, "ok", got.Status)
	require.Equal(t, "USD", got.Currency, "display exchange must not change system USD")
	require.Equal(t, 3.0, *got.QuotaRemaining)
	require.Equal(t, .5, *got.TotalUsed)
	require.Nil(t, got.TodayUsed)
	require.Nil(t, got.Balance, "key quota cannot masquerade as shared wallet")
	require.Nil(t, got.Billing.EffectiveRateMultiplier)
	require.Equal(t, "newapi_account_auth_required", got.Billing.Error)
}

func TestUpstreamFinanceNewAPIAccountWalletAndEffectiveRatio(t *testing.T) {
	svc, target, paths := newAPIFinanceFixture(t, newAPIFixtures(), true)
	got := svc.fetchBalance(context.Background(), target)
	require.Len(t, *paths, 4)
	require.NotContains(t, *paths, "/api/usage/token/", "authorized polling avoids the strict key-usage rate limit")
	require.Equal(t, "wallet", got.Kind)
	require.Equal(t, 20.0, *got.Balance)
	require.Equal(t, 3.0, *got.QuotaRemaining)
	require.Equal(t, .5, *got.TotalUsed, "cumulative usage is scoped to the key, not entire user")
	require.Nil(t, got.TodayUsed)
	require.Equal(t, "premium", *got.Billing.GroupName)
	require.Equal(t, .35, *got.Billing.EffectiveRateMultiplier)
	require.Equal(t, "newapi_account", got.Billing.Source)
	require.Equal(t, "ok", got.Billing.Status)
	require.False(t, got.Billing.Stale)
	require.Nil(t, got.Billing.GroupRateMultiplier, "user-effective group rate must not be called public base")
	encoded, err := json.Marshal(got)
	require.NoError(t, err)
	for _, secret := range []string{"console-secret", "sk-inference-secret", "never-store-this-key"} {
		require.NotContains(t, string(encoded), secret)
	}
}

func TestUpstreamFinanceNewAPIUnitsUnlimitedAndMalformed(t *testing.T) {
	for _, tc := range []struct {
		name, path, body, currency string
		unlimited                  bool
	}{
		{"unlimited", "/api/usage/token/", `{"success":true,"data":{"object":"token_usage","total_used":0,"unlimited_quota":true}}`, "USD", true},
		{"no conversion", "/api/status", `{"success":false,"data":{"quota_per_unit":500000}}`, "QUOTA", false},
		{"zero divisor", "/api/status", `{"success":true,"data":{"quota_per_unit":0}}`, "QUOTA", false},
		{"tiny divisor", "/api/status", `{"success":true,"data":{"quota_per_unit":1e-310}}`, "QUOTA", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fixtures := newAPIFixtures()
			fixtures[tc.path] = tc.body
			svc, target, _ := newAPIFinanceFixture(t, fixtures, false)
			got := svc.fetchBalance(context.Background(), target)
			require.Equal(t, "ok", got.Status)
			require.Equal(t, tc.currency, got.Currency)
			require.Equal(t, tc.unlimited, got.UnlimitedQuota)
			if tc.unlimited {
				require.Nil(t, got.QuotaRemaining)
			}
			if tc.currency == "QUOTA" {
				require.Equal(t, 1500000.0, *got.QuotaRemaining)
				require.Equal(t, "newapi_quota_unit_unknown", got.Error)
			}
		})
	}
	for _, body := range []string{`{}`, `{"code":false,"data":{"object":"token_usage","total_used":0,"total_available":0,"unlimited_quota":false}}`, `{"code":true,"data":{"object":"token_usage","total_used":0,"total_available":0}}`, `{"code":true,"data":{"object":"token_usage","total_used":-1,"total_available":0,"unlimited_quota":false}}`} {
		fixtures := newAPIFixtures()
		fixtures["/api/usage/token/"] = body
		svc, target, _ := newAPIFinanceFixture(t, fixtures, false)
		got := svc.fetchBalance(context.Background(), target)
		require.Equal(t, "unsupported", got.Status)
		require.Nil(t, got.QuotaRemaining)
	}
}

func TestUpstreamFinanceNewAPIAccountIdentityAndGroupFailures(t *testing.T) {
	for _, tc := range []struct {
		name, path, body, code string
		wallet                 bool
	}{
		{"invalid auth", "/api/user/self", `{"success":false,"message":"console-secret"}`, "newapi_account_auth_failed", false},
		{"wrong user", "/api/user/self", `{"success":true,"data":{"id":9,"quota":10000000,"group":"vip"}}`, "newapi_account_identity_mismatch", false},
		{"missing token", "/api/token/search", `{"success":true,"data":{"total":0,"items":[]}}`, "newapi_token_not_found", false},
		{"ambiguous token", "/api/token/search", `{"success":true,"data":{"total":2,"items":[]}}`, "newapi_token_ambiguous", false},
		{"wrong token owner", "/api/token/search", `{"success":true,"data":{"total":1,"items":[{"id":13,"user_id":9,"group":"premium"}]}}`, "newapi_account_identity_mismatch", false},
		{"missing rate", "/api/user/self/groups", `{"success":true,"data":{"other":{"ratio":1}}}`, "newapi_group_rate_unavailable", true},
		{"negative rate", "/api/user/self/groups", `{"success":true,"data":{"premium":{"ratio":-1}}}`, "newapi_group_rate_unavailable", true},
		{"dynamic group", "/api/token/search", `{"success":true,"data":{"total":1,"items":[{"id":13,"user_id":42,"group":"auto"}]}}`, "newapi_auto_group", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fixtures := newAPIFixtures()
			if tc.path == "/api/token/search" && tc.code == "newapi_auto_group" {
				tc.body = strings.Replace(fixtures[tc.path], `"group":"premium"`, `"group":"auto"`, 1)
			}
			fixtures[tc.path] = tc.body
			svc, target, _ := newAPIFinanceFixture(t, fixtures, true)
			got := svc.fetchBalance(context.Background(), target)
			require.Equal(t, tc.code, got.Billing.Error)
			require.Nil(t, got.Billing.EffectiveRateMultiplier)
			if tc.wallet {
				require.NotNil(t, got.Balance)
			} else {
				require.Nil(t, got.Balance)
			}
			if tc.code == "newapi_auto_group" {
				require.Equal(t, "ok", got.Billing.Status)
			}
		})
	}
}

func TestUpstreamFinanceNewAPIInheritedZeroAndDisabledKey(t *testing.T) {
	fixtures := newAPIFixtures()
	fixtures["/api/token/search"] = strings.Replace(fixtures["/api/token/search"], `"group":"premium"`, `"group":""`, 1)
	delete(fixtures, "/api/usage/token/") // revoked/expired token still readable with account authorization
	svc, target, _ := newAPIFinanceFixture(t, fixtures, true)
	got := svc.fetchBalance(context.Background(), target)
	require.Equal(t, "ok", got.Status)
	require.Equal(t, "vip", *got.Billing.GroupName)
	require.Equal(t, 0.0, *got.Billing.EffectiveRateMultiplier)
	require.Equal(t, 3.0, *got.QuotaRemaining)
	require.Equal(t, .5, *got.TotalUsed)
}

func TestUpstreamFinanceNewAPISub2APIPriorityAndSSRF(t *testing.T) {
	fixtures := newAPIFixtures()
	fixtures["/v1/usage"] = `{"balance":2}`
	svc, target, paths := newAPIFinanceFixture(t, fixtures, false)
	got := svc.fetchBalance(context.Background(), target)
	require.Equal(t, "wallet", got.Kind)
	require.Equal(t, []string{"/v1/usage"}, *paths)
	target.NewAPIUserID, target.NewAPIAccessTokenEncrypted = 42, "console-secret"
	target.Endpoint = "https://127.0.0.1"
	got = svc.fetchBalance(context.Background(), target)
	require.Equal(t, "endpoint_unavailable", got.Error)
	require.Len(t, *paths, 1)
}

func TestUpstreamFinanceNewAPIWalletIdentity(t *testing.T) {
	base := UpstreamFinanceTarget{ID: 1, Provider: "openai", Endpoint: "https://8.8.8.8", APIKeyEncrypted: "key", WalletRef: "default"}
	original := upstreamBalanceIdentity(&base)
	base.NewAPIUserID, base.NewAPIAccessTokenEncrypted = 42, "encrypted-console"
	authorized := upstreamBalanceIdentity(&base)
	require.NotEqual(t, original, authorized)
	base.NewAPIUserID = 43
	require.NotEqual(t, authorized, upstreamBalanceIdentity(&base))
	base.NewAPIUserID, base.NewAPIAccessTokenEncrypted = 0, ""
	require.Equal(t, original, upstreamBalanceIdentity(&base))

	targets := []*UpstreamTarget{}
	for _, userID := range []int64{42, 42, 43} {
		targets = append(targets, &UpstreamTarget{ID: int64(len(targets) + 1), NewAPIUserID: userID, Endpoint: "https://8.8.8.8/v1", WalletRef: "default", Balance: &UpstreamBalanceSnapshot{Kind: "wallet", Balance: financeFloat(20), Currency: "USD", Status: "ok"}})
	}
	require.Len(t, upstreamSupplierWallets(targets), 2, "shared key wallets are deduplicated, separate users remain separate")
	targets[1].Endpoint = "https://1.1.1.1"
	require.Len(t, upstreamSupplierWallets(targets), 3)
}
