package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type reauthCacheAccountRepo struct {
	AccountRepository
	account *Account
	err     error
}

func (r *reauthCacheAccountRepo) GetByID(context.Context, int64) (*Account, error) {
	return r.account, r.err
}

type reauthStaleTokenCache struct {
	OpenAITokenCache
	values []string
	reads  int
	saved  string
	onWait func()
}

func (c *reauthStaleTokenCache) GetAccessToken(context.Context, string) (string, error) {
	c.reads++
	if c.reads > 1 && c.onWait != nil {
		c.onWait()
	}
	index := c.reads - 1
	if index >= len(c.values) {
		index = len(c.values) - 1
	}
	return c.values[index], nil
}

func (c *reauthStaleTokenCache) SetAccessToken(_ context.Context, _ string, value string, _ time.Duration) error {
	c.saved = value
	return nil
}

func (c *reauthStaleTokenCache) AcquireRefreshLock(context.Context, string, time.Duration) (bool, error) {
	return false, nil
}

func reauthCacheFixture() (*OpenAITokenProvider, *reauthCacheAccountRepo, *reauthStaleTokenCache, *Account) {
	account := reauthTransportAccount()
	account.Status = StatusActive
	account.Schedulable = true
	account.Credentials = map[string]any{"access_token": "current-token", "refresh_token": "refresh-token", "expires_at": time.Now().Add(time.Hour).Unix()}
	repo := &reauthCacheAccountRepo{account: account}
	cache := &reauthStaleTokenCache{values: []string{"revoked-cache-token"}}
	reauth := &OpenAIReauthService{accounts: repo, proxies: &reauthProxyStub{proxy: account.Proxy}}
	provider := &OpenAITokenProvider{accountRepo: repo, tokenCache: cache, openAIReauth: reauth,
		refreshPolicy: ProviderRefreshPolicy{OnLockHeld: ProviderLockHeldWaitForCache}}
	return provider, repo, cache, account
}

func TestOpenAIReauthStaleCacheUsesAuthoritativeTokenWithoutSuccessfulInvalidation(t *testing.T) {
	provider, _, cache, account := reauthCacheFixture()
	token, err := provider.GetAccessToken(context.Background(), account)
	require.NoError(t, err)
	require.Equal(t, "current-token", token)
	require.Equal(t, "current-token", cache.saved)
}

func TestOpenAIReauthLockWaitRevalidatesCacheAndPendingState(t *testing.T) {
	for _, unified := range []bool{false, true} {
		for _, outcome := range []string{"rotated", "pending", "repository_failure"} {
			name := "legacy/" + outcome
			if unified {
				name = "unified/" + outcome
			}
			t.Run(name, func(t *testing.T) {
				provider, repo, cache, snapshot := reauthCacheFixture()
				snapshot.Credentials["expires_at"] = time.Now().Add(time.Minute).Unix()
				cache.values = []string{"", "revoked-cache-token"}
				cache.onWait = func() {
					fresh := *snapshot
					fresh.Credentials = map[string]any{"access_token": "newly-rotated-token", "refresh_token": "new-refresh", "expires_at": time.Now().Add(time.Hour).Unix()}
					fresh.Extra = map[string]any{OpenAIReauthEnabledKey: true}
					if outcome == "pending" {
						fresh.Extra[OpenAIReauthPendingKey] = true
					}
					if outcome == "repository_failure" {
						repo.err = errors.New("unavailable")
					}
					repo.account = &fresh
				}
				if unified {
					provider.refreshAPI = NewOAuthRefreshAPI(repo, cache)
					provider.executor = &OpenAITokenRefresher{}
				}
				token, err := provider.GetAccessToken(context.Background(), snapshot)
				if outcome == "rotated" {
					require.NoError(t, err)
					require.Equal(t, "newly-rotated-token", token)
					require.Equal(t, "newly-rotated-token", cache.saved)
				} else {
					require.Error(t, err)
					require.Empty(t, token)
					require.Empty(t, cache.saved)
				}
				require.GreaterOrEqual(t, cache.reads, 2)
			})
		}
	}
}
