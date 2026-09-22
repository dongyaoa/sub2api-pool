package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/url"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/stretchr/testify/require"
)

type manualReauthStoreStub struct {
	OpenAIReauthRepository
	called     bool
	applied    bool
	err        error
	patch      map[string]any
	generation int64
}

func (r *manualReauthStoreStub) CompleteManual(_ context.Context, _ *Account, generation int64, patch map[string]any) (bool, error) {
	r.called = true
	r.patch = patch
	r.generation = generation
	return r.applied, r.err
}

func manualReauthSession(t *testing.T, s *OpenAIReauthService) OpenAIManualReauthSession {
	t.Helper()
	a, err := s.accounts.GetByID(context.Background(), 1)
	require.NoError(t, err)
	auth, err := s.oauth.GenerateAuthURL(context.Background(), a.ProxyID, openai.DefaultRedirectURI, PlatformOpenAI)
	require.NoError(t, err)
	u, err := url.Parse(auth.AuthURL)
	require.NoError(t, err)
	return OpenAIManualReauthSession{SessionID: auth.SessionID, Code: "fixture-manual-code", State: u.Query().Get("state")}
}

func TestOpenAIManualReauthUsesVerifiedSessionAndPreservesManualControls(t *testing.T) {
	s, _, store, oauth, lock := reauthFixture(t)
	manual := &manualReauthStoreStub{OpenAIReauthRepository: store, applied: true}
	s.repo = manual
	a, err := s.accounts.GetByID(context.Background(), 1)
	require.NoError(t, err)
	a.Status = StatusDisabled
	a.Schedulable = false
	a.Extra[OpenAIReauthEnabledKey] = false
	until := time.Now().Add(time.Hour)
	a.RateLimitResetAt = &until
	before, _ := json.Marshal(a)
	input := manualReauthSession(t, s)
	updated, err := s.CompleteManualOAuth(context.Background(), 1, input)
	require.NoError(t, err)
	require.Equal(t, a, updated)
	require.True(t, manual.called)
	require.Equal(t, 1, oauth.exchanges)
	require.Zero(t, oauth.refreshes)
	require.True(t, lock.released)
	require.Equal(t, []string{"http://proxy.example.test:8080"}, oauth.proxyURLs)
	require.Equal(t, int64(1), manual.generation)
	require.Equal(t, "new-access", manual.patch["access_token"])
	require.NotContains(t, manual.patch, "model_mapping")
	require.NotContains(t, manual.patch, "schedulable")
	after, _ := json.Marshal(a)
	require.JSONEq(t, string(before), string(after))
	_, ok := s.oauth.sessionStore.Get(input.SessionID)
	require.False(t, ok, "successful exchange consumes the session")
}

func TestOpenAIManualReauthRejectsUnverifiedOrDifferentProxySession(t *testing.T) {
	for _, scenario := range []string{"missing_session", "wrong_state", "wrong_proxy", "not_pending"} {
		t.Run(scenario, func(t *testing.T) {
			s, _, store, oauth, _ := reauthFixture(t)
			manual := &manualReauthStoreStub{OpenAIReauthRepository: store, applied: true}
			s.repo = manual
			input := manualReauthSession(t, s)
			switch scenario {
			case "missing_session":
				input.SessionID = "unknown-session"
			case "wrong_state":
				input.State = "wrong-state"
			case "wrong_proxy":
				session, _ := s.oauth.sessionStore.Get(input.SessionID)
				session.ProxyURL = "http://different.example.test:8080"
			case "not_pending":
				a, _ := s.accounts.GetByID(context.Background(), 1)
				delete(a.Extra, OpenAIReauthPendingKey)
			}
			_, err := s.CompleteManualOAuth(context.Background(), 1, input)
			require.Error(t, err)
			require.False(t, manual.called)
			require.Zero(t, oauth.exchanges)
		})
	}
}

func TestOpenAIManualReauthRejectsChangedIdentity(t *testing.T) {
	s, _, store, oauth, _ := reauthFixture(t)
	manual := &manualReauthStoreStub{OpenAIReauthRepository: store, applied: true}
	s.repo = manual
	input := manualReauthSession(t, s)
	claims, _ := json.Marshal(map[string]any{"email": "another@example.test", "exp": time.Now().Add(time.Hour).Unix()})
	oauth.token.IDToken = "e30." + base64.RawURLEncoding.EncodeToString(claims) + ".test"
	_, err := s.CompleteManualOAuth(context.Background(), 1, input)
	require.EqualError(t, err, "identity_mismatch")
	require.False(t, manual.called)
}

func TestOpenAIManualReauthKeepsPendingOnCASFailureAndSanitizesPersistenceErrors(t *testing.T) {
	for _, persistenceErr := range []error{nil, errors.New("SQL response containing a credential")} {
		t.Run("cas", func(t *testing.T) {
			s, _, store, _, _ := reauthFixture(t)
			manual := &manualReauthStoreStub{OpenAIReauthRepository: store, err: persistenceErr}
			s.repo = manual
			_, err := s.CompleteManualOAuth(context.Background(), 1, manualReauthSession(t, s))
			if persistenceErr == nil {
				require.EqualError(t, err, "state_changed")
			} else {
				require.EqualError(t, err, "manual_reauth_persistence_failed")
			}
			a, _ := s.accounts.GetByID(context.Background(), 1)
			require.True(t, OpenAIReauthPending(a))
		})
	}
}
