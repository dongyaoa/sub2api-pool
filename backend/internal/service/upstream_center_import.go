package service

import (
	"context"
	"strings"
	"time"
)

// sourceAccountCredentials is an explicit, one-time credential import. The
// resulting monitor has its own encrypted credential and never acquires the
// account's business bindings or changes its local procurement configuration.
func (s *UpstreamCenterService) sourceAccountCredentials(ctx context.Context, id int64, field string) (provider, endpoint, key string, err error) {
	invalid := func(detail string) (string, string, string, error) {
		return "", "", "", ErrUpstreamInvalid.WithMetadata(map[string]string{"field": field, "detail": detail})
	}
	if id <= 0 || s.accounts == nil {
		return invalid("select a valid API key account")
	}
	a, err := s.accounts.GetByID(ctx, id)
	if err != nil {
		return "", "", "", err
	}
	if a == nil || a.Type != AccountTypeAPIKey || a.ParentAccountID != nil || a.IsSyntheticUITest() {
		return invalid("source must be an API key account with its own credentials")
	}
	if a.Status != StatusActive || !a.Schedulable || (a.AutoPauseOnExpired && a.ExpiresAt != nil && !time.Now().Before(*a.ExpiresAt)) {
		return invalid("source API key account is disabled or expired")
	}
	provider = a.Platform
	switch provider {
	case MonitorProviderOpenAI:
		endpoint = "https://api.openai.com"
	case MonitorProviderAnthropic:
		endpoint = "https://api.anthropic.com"
	case MonitorProviderGemini:
		endpoint = "https://generativelanguage.googleapis.com"
	default:
		return invalid("source account provider is not supported by monitoring")
	}
	if configured := normalizeEndpoint(a.GetCredential("base_url")); configured != "" {
		endpoint = configured
	}
	key = strings.TrimSpace(a.GetCredential("api_key"))
	if key == "" || len(key) > 2000 || strings.ContainsAny(key, "\r\n") {
		return invalid("source account has no usable API key")
	}
	return provider, endpoint, key, nil
}
