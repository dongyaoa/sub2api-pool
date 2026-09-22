package service

import (
	"context"
	"net/http"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type reauthPendingTokenCache struct {
	OpenAITokenCache
	reads int
}

func (c *reauthPendingTokenCache) GetAccessToken(context.Context, string) (string, error) {
	c.reads++
	return "stale-revoked-token", nil
}

func TestOpenAIReauthPendingBlocksTokensAndTransportsAfterAutomationDisabled(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		name := "automation_disabled"
		if enabled {
			name = "automation_enabled"
		}
		t.Run(name, func(t *testing.T) {
			account := reauthTransportAccount()
			account.Extra[OpenAIReauthEnabledKey] = enabled
			account.Extra[OpenAIReauthPendingKey] = true
			cache := &reauthPendingTokenCache{}
			provider := &OpenAITokenProvider{tokenCache: cache}
			token, err := provider.GetAccessToken(context.Background(), account)
			require.EqualError(t, err, "reauth_account_pending")
			require.Empty(t, token)
			require.Zero(t, cache.reads, "pending gate must precede cache lookup even without a worker")

			_, err = (&OpenAIReauthService{}).CheckForUse(context.Background(), account)
			require.EqualError(t, err, "reauth_account_pending")
			proxyURL := account.Proxy.URL()
			require.EqualError(t, validateOpenAIReauthTransportProxy(account, proxyURL), "reauth_account_pending")
			request, err := http.NewRequest(http.MethodGet, "https://chatgpt.com/", nil)
			require.NoError(t, err)
			_, err = (&OpenAIGatewayService{}).doOpenAIUpstream(request, proxyURL, account)
			require.EqualError(t, err, "reauth_account_pending")
			_, err = (&AccountTestService{}).doOpenAIAccountTestUpstream(request, proxyURL, account, false)
			require.EqualError(t, err, "reauth_account_pending")
			pool := newOpenAIWSConnPool(&config.Config{})
			_, err = pool.Acquire(context.Background(), openAIWSAcquireRequest{Account: account, ProxyURL: proxyURL})
			require.EqualError(t, err, "reauth_account_pending")
			_, err = pool.dialConn(context.Background(), openAIWSAcquireRequest{Account: account, ProxyURL: proxyURL})
			require.EqualError(t, err, "reauth_account_pending")
		})
	}
}

func TestOpenAIReauthCheckForUseHonorsFreshPendingAfterAutomationDisabled(t *testing.T) {
	snapshot := reauthTransportAccount()
	fresh := *snapshot
	fresh.Status = StatusActive
	fresh.Schedulable = true
	fresh.Extra = map[string]any{OpenAIReauthEnabledKey: false, OpenAIReauthPendingKey: true}
	svc := &OpenAIReauthService{accounts: &reauthAccountsStub{account: &fresh}}
	_, err := svc.CheckForUse(context.Background(), snapshot)
	require.EqualError(t, err, "reauth_account_pending")
}
