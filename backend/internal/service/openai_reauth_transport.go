package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/url"
	"time"
)

func openAIAccountProxyURL(account *Account) string {
	if account != nil && account.ProxyID != nil && account.Proxy != nil {
		return account.Proxy.URL()
	}
	return ""
}

// A shadow borrows credentials, not permission to change their egress route.
// Compare its actual routing snapshot against the freshly resolved owner.
func validateOpenAIReauthShadowRoute(shadow, owner *Account, proxyURL string) error {
	if !OpenAIReauthEnabled(owner) && !OpenAIReauthPending(owner) {
		return nil
	}
	if err := validateOpenAIReauthTransportProxy(owner, proxyURL); err != nil {
		return err
	}
	if shadow == nil || shadow.ProxyID == nil || owner.ProxyID == nil || *shadow.ProxyID != *owner.ProxyID ||
		shadow.ProxyFallbackOriginID != nil || shadow.Proxy == nil || shadow.Proxy.ID != *shadow.ProxyID ||
		openAIAccountProxyURL(shadow) != proxyURL || !owner.IsActive() || !owner.Schedulable {
		return ErrOpenAIReauthInvalidAccount
	}
	return nil
}

// Final HTTP/WS dispatch guard also covers shadows, whose persisted extra does
// not inherit the owner's authorization flags. Never copy secrets to a shadow.
func validateOpenAIReauthCredentialOwnerProxy(ctx context.Context, repo AccountRepository, account *Account, proxyURL string) error {
	if account == nil || !account.IsShadow() || OpenAIReauthEnabled(account) {
		return validateOpenAIReauthTransportProxy(account, proxyURL)
	}
	if OpenAIReauthPending(account) {
		return errors.New("reauth_account_pending")
	}
	if repo == nil {
		return errors.New("reauth_account_unavailable")
	}
	owner, err := resolveCredentialAccount(ctx, repo, account)
	if err != nil || owner == nil {
		return errors.New("reauth_account_unavailable")
	}
	return validateOpenAIReauthShadowRoute(account, owner, proxyURL)
}

// validateOpenAIReauthTransportProxy is the final guard for an already-resolved
// account snapshot. A missing relation, a pool/fallback route, or an alternate
// transport proxy must fail before any token can leave through another IP.
func validateOpenAIReauthTransportProxy(account *Account, proxyURL string) error {
	if OpenAIReauthPending(account) {
		return errors.New("reauth_account_pending")
	}
	if !OpenAIReauthEnabled(account) {
		return nil
	}
	if account.ProxyID == nil || account.Proxy == nil || account.Proxy.ID != *account.ProxyID ||
		account.IsShadow() || account.ProxyFallbackOriginID != nil || !account.Proxy.IsActive() ||
		account.Proxy.IsExpired(time.Now()) || proxyURL == "" || proxyURL != account.Proxy.URL() {
		return ErrOpenAIReauthInvalidAccount
	}
	pool, err := ParseAccountProxyPool(account.Extra[AccountProxyPoolExtraKey])
	if err != nil || len(pool) > 1 || (len(pool) == 1 && pool[0].ProxyID != *account.ProxyID) {
		return ErrOpenAIReauthInvalidAccount
	}
	u, err := url.Parse(proxyURL)
	if err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https" && u.Scheme != "socks5") ||
		(u.Scheme == "socks5" && u.User != nil) {
		return ErrOpenAIReauthInvalidAccount
	}
	return nil
}

// Reauth accounts and credential shadows must not reuse a websocket established
// under a different proxy. Shadows do not inherit the owner's durable flags, so
// include their route even when their owner has not enabled auto reauthorization.
func openAIReauthProxyCompatibility(account *Account) string {
	if account == nil || (!OpenAIReauthEnabled(account) && !account.IsShadow()) || account.Proxy == nil {
		return ""
	}
	sum := sha256.Sum256([]byte(account.Proxy.URL()))
	return hex.EncodeToString(sum[:])
}
