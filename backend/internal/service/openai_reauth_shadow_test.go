package service

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func reauthShadowFixture() (*Account, *Account, *reauthCacheAccountRepo) {
	owner := reauthTransportAccount()
	owner.Status = StatusActive
	owner.Schedulable = true
	owner.Credentials = map[string]any{"access_token": "parent-access-token"}
	shadowProxy := *owner.Proxy
	shadowID := *owner.ProxyID
	shadow := &Account{ID: 43, Platform: PlatformOpenAI, Type: AccountTypeOAuth,
		ParentAccountID: &owner.ID, ProxyID: &shadowID, Proxy: &shadowProxy,
		Extra: map[string]any{}, Credentials: map[string]any{"model_mapping": map[string]any{"a": "b"}}}
	return owner, shadow, &reauthCacheAccountRepo{account: owner}
}

func TestOpenAIReauthShadowCannotBorrowTokensThroughDifferentRoute(t *testing.T) {
	for _, scenario := range []string{"different_proxy", "endpoint_changed", "direct", "fallback", "pending", "owner_unavailable"} {
		t.Run(scenario, func(t *testing.T) {
			owner, shadow, repo := reauthShadowFixture()
			switch scenario {
			case "different_proxy":
				id := int64(999)
				owner.ProxyID = &id
				owner.Proxy.ID = id
			case "endpoint_changed":
				owner.Proxy.Port++
			case "direct":
				shadow.ProxyID = nil
				shadow.Proxy = nil
			case "fallback":
				id := int64(17)
				shadow.ProxyFallbackOriginID = &id
			case "pending":
				owner.Extra[OpenAIReauthEnabledKey] = false
				owner.Extra[OpenAIReauthPendingKey] = true
			case "owner_unavailable":
				repo.err = errors.New("unavailable")
			}
			gateway := &OpenAIGatewayService{accountRepo: repo}
			token, _, err := gateway.GetAccessToken(context.Background(), shadow)
			require.Error(t, err)
			require.Empty(t, token)
			request, err := http.NewRequest(http.MethodGet, "https://chatgpt.com/", nil)
			require.NoError(t, err)
			_, err = gateway.doOpenAIUpstream(request, openAIAccountProxyURL(shadow), shadow)
			require.Error(t, err, "guard must reject before dispatching to missing upstream")
			_, err = (&AccountTestService{accountRepo: repo}).doOpenAIAccountTestUpstream(request, openAIAccountProxyURL(shadow), shadow, false)
			require.Error(t, err)
		})
	}
}

func TestOpenAIReauthShadowPreservesMatchingRouteWithoutCopyingCredentials(t *testing.T) {
	owner, shadow, repo := reauthShadowFixture()
	require.NoError(t, validateOpenAIReauthCredentialOwnerProxy(context.Background(), repo, shadow, openAIAccountProxyURL(shadow)))
	token, kind, err := (&OpenAIGatewayService{accountRepo: repo}).GetAccessToken(context.Background(), shadow)
	require.NoError(t, err)
	require.Equal(t, owner.GetCredential("access_token"), token)
	require.Equal(t, "oauth", kind)
	require.NotContains(t, shadow.Credentials, "access_token")
	require.NotContains(t, shadow.Extra, OpenAIReauthEnabledKey)
	// Ordinary shadows retain their pre-existing routing behavior.
	owner.Extra[OpenAIReauthEnabledKey] = false
	shadow.ProxyID = nil
	shadow.Proxy = nil
	require.NoError(t, validateOpenAIReauthCredentialOwnerProxy(context.Background(), repo, shadow, ""))
}

func TestOpenAIReauthShadowWebsocketCannotReusePreviousProxyConnection(t *testing.T) {
	_, shadow, _ := reauthShadowFixture()
	old := newOpenAIWSConn("previous-proxy", shadow.ID, nil, nil)
	old.handshakeCompatibility = normalizeOpenAIWSHandshakeCompatibility(shadow, nil)
	require.NotEmpty(t, old.handshakeCompatibility.reauthProxy)
	shadow.Proxy.Port++
	current := normalizeOpenAIWSHandshakeCompatibility(shadow, nil)
	require.False(t, old.matchesHandshakeCompatibility(current))
}
