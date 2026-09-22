package service

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

func reauthTransportAccount() *Account {
	id := int64(17)
	return &Account{ID: 42, Platform: PlatformOpenAI, Type: AccountTypeOAuth, ProxyID: &id,
		Proxy: &Proxy{ID: id, Protocol: "http", Host: "127.0.0.1", Port: 3128, Status: StatusActive},
		Extra: map[string]any{OpenAIReauthEnabledKey: true}}
}

func TestOpenAIReauthTransportRejectsDirectAndAlternateRoutes(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*Account, *string)
	}{
		{"direct", func(_ *Account, proxyURL *string) { *proxyURL = "" }},
		{"other_proxy", func(_ *Account, proxyURL *string) { *proxyURL = "http://127.0.0.1:9999" }},
		{"missing_relation", func(a *Account, _ *string) { a.Proxy = nil }},
		{"mismatched_relation", func(a *Account, _ *string) { a.Proxy.ID++ }},
		{"inactive", func(a *Account, _ *string) { a.Proxy.Status = StatusDisabled }},
		{"expired", func(a *Account, _ *string) { past := time.Now().Add(-time.Second); a.Proxy.ExpiresAt = &past }},
		{"fallback", func(a *Account, _ *string) { id := int64(999); a.ProxyFallbackOriginID = &id }},
		{"shadow", func(a *Account, _ *string) { id := int64(999); a.ParentAccountID = &id }},
		{"pool_rotation", func(a *Account, _ *string) {
			a.Extra[AccountProxyPoolExtraKey] = []AccountProxyPoolEntry{{ProxyID: 17, Concurrency: 1}, {ProxyID: 18, Concurrency: 1}}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			account := reauthTransportAccount()
			proxyURL := account.Proxy.URL()
			tc.mutate(account, &proxyURL)
			require.ErrorIs(t, validateOpenAIReauthTransportProxy(account, proxyURL), ErrOpenAIReauthInvalidAccount)
			// No upstream dependencies exist: any accidental dispatch would panic.
			request, err := http.NewRequest(http.MethodGet, "https://chatgpt.com/", nil)
			require.NoError(t, err)
			_, err = (&OpenAIGatewayService{}).doOpenAIUpstream(request, proxyURL, account)
			require.ErrorIs(t, err, ErrOpenAIReauthInvalidAccount)
			_, err = (&AccountTestService{}).doOpenAIAccountTestUpstream(request, proxyURL, account, false)
			require.ErrorIs(t, err, ErrOpenAIReauthInvalidAccount)
			pool := newOpenAIWSConnPool(&config.Config{})
			_, err = pool.Acquire(context.Background(), openAIWSAcquireRequest{Account: account, ProxyURL: proxyURL})
			require.ErrorIs(t, err, ErrOpenAIReauthInvalidAccount)
			_, err = pool.dialConn(context.Background(), openAIWSAcquireRequest{Account: account, ProxyURL: proxyURL})
			require.ErrorIs(t, err, ErrOpenAIReauthInvalidAccount)
		})
	}
	account := reauthTransportAccount()
	require.NoError(t, validateOpenAIReauthTransportProxy(account, account.Proxy.URL()))
	account.Extra[OpenAIReauthEnabledKey] = false
	require.NoError(t, validateOpenAIReauthTransportProxy(account, ""))
}

func TestOpenAIReauthWebsocketCompatibilityIncludesPinnedProxy(t *testing.T) {
	account := reauthTransportAccount()
	first := normalizeOpenAIWSHandshakeCompatibility(account, nil)
	account.Proxy.Port++
	second := normalizeOpenAIWSHandshakeCompatibility(account, nil)
	require.NotEqual(t, first, second, "must not reuse a socket from the previous proxy")
	require.Len(t, second.reauthProxy, 64)
}

func TestOpenAIReauthRefreshDoesNotFallBackToDirectWhenProxyLookupFails(t *testing.T) {
	repo := &contentModerationTestProxyRepo{getByIDErr: errors.New("proxy repository offline")}
	service := &OpenAIOAuthService{proxyRepo: repo}
	_, err := service.RefreshAccountToken(context.Background(), reauthTransportAccount())
	require.ErrorIs(t, err, ErrOpenAIReauthInvalidAccount)
	require.Equal(t, int64(1), repo.getCalls.Load())
}

func TestOpenAIReauthWebsocketCompatibilityIncludesActualAuthorization(t *testing.T) {
	for _, shadow := range []bool{false, true} {
		account := reauthTransportAccount()
		if shadow {
			parentID := int64(9)
			account.ParentAccountID = &parentID
			account.Extra = nil
		}
		old := normalizeOpenAIWSHandshakeCompatibility(account, http.Header{"Authorization": {"Bearer old-fixture-token"}})
		fresh := normalizeOpenAIWSHandshakeCompatibility(account, http.Header{"Authorization": {"Bearer new-fixture-token"}})
		require.NotEqual(t, old, fresh, "rotated authorization must not reuse a socket with an old handshake")
		require.Len(t, fresh.reauthToken, 64)
	}
}
