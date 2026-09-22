package service

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func autoReauthSchedulableAccount() *Account {
	proxyID := int64(7)
	return &Account{ID: 9, Platform: PlatformOpenAI, Type: AccountTypeOAuth,
		Status: StatusActive, Schedulable: true, ProxyID: &proxyID,
		Extra: map[string]any{OpenAIReauthEnabledKey: true},
		Proxy: &Proxy{ID: proxyID, Status: StatusActive, Protocol: "http", Host: "proxy.example", Port: 8080}}
}

func TestOpenAIReauthPendingBlocksAccountAndShadowAfterDisable(t *testing.T) {
	a := autoReauthSchedulableAccount()
	require.True(t, a.IsSchedulable())
	require.True(t, a.IsCredentialUsableForShadow())
	a.Extra[OpenAIReauthPendingKey] = true
	for _, enabled := range []bool{true, false} {
		a.Extra[OpenAIReauthEnabledKey] = enabled
		require.False(t, a.IsSchedulable())
		require.False(t, a.IsCredentialUsableForShadow())
	}
}

func TestOpenAIReauthStrictProxyFailuresStopScheduling(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*Account)
	}{
		{"missing", func(a *Account) { a.Proxy = nil }},
		{"direct", func(a *Account) { a.ProxyID = nil }},
		{"expired", func(a *Account) { past := time.Now().Add(-time.Hour); a.Proxy.ExpiresAt = &past }},
		{"disabled", func(a *Account) { a.Proxy.Status = StatusDisabled }},
		{"changed", func(a *Account) { a.Proxy.ID++ }},
		{"fallback", func(a *Account) { origin := int64(8); a.ProxyFallbackOriginID = &origin }},
		{"unsupported_socks_auth", func(a *Account) { a.Proxy.Protocol = "socks5"; a.Proxy.Username = "user" }},
		{"rotating_pool", func(a *Account) { a.ProxyPool = []AccountProxyPoolEntry{{ProxyID: 7}, {ProxyID: 8}} }},
		{"malformed_pool", func(a *Account) { a.Extra[AccountProxyPoolExtraKey] = "invalid" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := autoReauthSchedulableAccount()
			tc.mutate(a)
			require.False(t, a.IsSchedulable())
			require.False(t, a.IsCredentialUsableForShadow())
		})
	}
}

func TestOpenAIReauthProxySelectionNeverClearsOrRotatesBinding(t *testing.T) {
	a := autoReauthSchedulableAccount()
	a.Proxy.Status = StatusExpired
	a.ProxyPool = []AccountProxyPoolEntry{{ProxyID: 7, Concurrency: 1, Proxy: a.Proxy}}
	SelectAccountProxy(a)
	require.NotNil(t, a.ProxyID)
	require.EqualValues(t, 7, *a.ProxyID)
	require.NotNil(t, a.Proxy)
	require.False(t, selectNextAccountProxy(a, map[int64]struct{}{7: {}}))
	a.Proxy.Status = StatusActive
	require.False(t, selectNextAccountProxy(a, map[int64]struct{}{7: {}}))
}
