package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
)

// OpenAIManualReauthSession carries an unconsumed PKCE session. Arbitrary
// imported tokens are deliberately not accepted as proof of recovery.
type OpenAIManualReauthSession struct {
	SessionID string `json:"session_id" binding:"required"`
	Code      string `json:"code" binding:"required"`
	State     string `json:"state" binding:"required"`
}

func (s *OpenAIReauthService) CompleteManualOAuth(ctx context.Context, accountID int64, input OpenAIManualReauthSession) (*Account, error) {
	ctx, cancel := context.WithTimeout(ctx, 4*time.Minute)
	defer cancel()
	if s == nil || s.oauth == nil || s.repo == nil || s.accounts == nil || s.refresh == nil || s.refresh.tokenCache == nil {
		return nil, errors.New("manual_reauth_unavailable")
	}
	a, err := s.accounts.GetByID(ctx, accountID)
	if err != nil || a == nil || a.Platform != PlatformOpenAI || a.Type != AccountTypeOAuth || !OpenAIReauthPending(a) {
		return nil, errors.New("manual_reauth_not_pending")
	}
	// Share both locks with automatic reauthorization and token refresh. This
	// avoids consuming a refresh token while an old job is completing.
	key := OpenAITokenCacheKey(a)
	mu := s.refresh.getLocalLock(key)
	if err = mu.Lock(ctx); err != nil {
		return nil, errors.New("lock_unavailable")
	}
	defer mu.Unlock()
	locked, err := s.refresh.tokenCache.AcquireRefreshLock(ctx, key, 6*time.Minute)
	if err != nil || !locked {
		return nil, errors.New("lock_unavailable")
	}
	defer s.refresh.releaseRefreshLock(ctx, key)
	a, err = s.accounts.GetByID(ctx, accountID)
	if err != nil || a == nil || !OpenAIReauthPending(a) {
		return nil, errors.New("state_changed")
	}
	cfg, err := s.repo.Get(ctx, accountID)
	if err != nil || cfg == nil {
		return nil, errors.New("manual_reauth_unavailable")
	}
	p, err := StrictOpenAIProxy(ctx, a, s.proxies)
	if err != nil {
		return nil, errors.New("proxy_unavailable")
	}
	proxySnapshot := *p
	session, ok := s.oauth.sessionStore.Get(input.SessionID)
	if !ok || session.ProxyURL != p.URL() || session.RedirectURI != openai.DefaultRedirectURI {
		return nil, errors.New("manual_reauth_session_mismatch")
	}
	token, err := s.oauth.ExchangeCode(ctx, &OpenAIExchangeCodeInput{SessionID: input.SessionID, Code: input.Code, State: input.State})
	if err != nil {
		return nil, errors.New("manual_reauth_exchange_failed")
	}
	if ctx.Err() != nil {
		return nil, errors.New("manual_reauth_exchange_failed")
	}
	email := strings.TrimSpace(a.GetCredential("email"))
	if email == "" {
		email = strings.TrimSpace(a.Name)
	}
	if !reauthIdentityMatches(a, email, token) {
		return nil, errors.New("identity_mismatch")
	}
	patch := s.oauth.BuildAccountCredentials(token)
	patch["_token_version"] = time.Now().UnixMilli()
	// Use the exact proxy snapshot checked before the exchange in the final CAS.
	a.Proxy = &proxySnapshot
	applied, err := s.repo.CompleteManual(ctx, a, cfg.Generation, patch)
	if err != nil {
		return nil, errors.New("manual_reauth_persistence_failed")
	}
	if !applied {
		return nil, errors.New("state_changed")
	}
	if s.cache != nil {
		_ = s.cache.InvalidateToken(ctx, a)
	}
	updated, err := s.accounts.GetByID(ctx, accountID)
	if err != nil || updated == nil {
		return nil, errors.New("manual_reauth_state_unavailable")
	}
	return updated, nil
}
