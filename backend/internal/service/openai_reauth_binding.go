package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/mail"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
)

type OpenAIReauthCredentialsInput struct {
	Email      string `json:"email"`
	Password   string `json:"password"`
	TOTPSecret string `json:"totp_secret"`
	Enabled    *bool  `json:"enabled"`
}

// BindCredentials stores recovery credentials without interrupting current
// authorization, including on accounts manually disabled by an administrator.
func (s *OpenAIReauthService) BindCredentials(ctx context.Context, id int64, input OpenAIReauthCredentialsInput) (*OpenAIReauthStatus, error) {
	if input.Enabled == nil {
		return nil, errors.New("invalid_request")
	}
	secret, code := validateOpenAIReauthSecret(input.Email, input.Password, input.TOTPSecret)
	if code != "" {
		return nil, errors.New(code)
	}
	a, err := s.accounts.GetByID(ctx, id)
	if err != nil || a == nil {
		return nil, errors.New("invalid_account")
	}
	return s.bindCredentials(ctx, a, secret, *input.Enabled)
}

func (s *OpenAIReauthService) bindCredentials(ctx context.Context, a *Account, secret openAIReauthSecret, enabled bool) (*OpenAIReauthStatus, error) {
	if !s.EncryptionConfigured() {
		return nil, errors.New("encryption_key_not_configured")
	}
	if enabled && !s.WorkerConfigured() {
		return nil, errors.New("worker_not_configured")
	}
	if a.Platform != PlatformOpenAI || a.Type != AccountTypeOAuth || a.IsShadow() || a.IsOpenAIPersonalAccessToken() {
		return nil, errors.New("invalid_account")
	}
	email := openAIReauthAccountEmail(a)
	if email == "" && strings.TrimSpace(a.GetCredential("chatgpt_account_id")) == "" && strings.TrimSpace(a.GetCredential("chatgpt_user_id")) == "" {
		return nil, errors.New("identity_unknown")
	}
	if email != "" && !strings.EqualFold(email, secret.Email) {
		return nil, errors.New("identity_mismatch")
	}
	if enabled {
		if _, err := StrictOpenAIProxy(ctx, a, s.proxies); err != nil {
			return nil, errors.New("invalid_proxy")
		}
	}
	plain, _ := json.Marshal(secret)
	cipher, err := s.encryptor.Encrypt(string(plain))
	if err != nil {
		return nil, errors.New("configuration_failed")
	}
	if err := s.repo.SaveBinding(ctx, a, cipher, secret.Email, enabled); err != nil {
		if errors.Is(err, ErrOpenAIReauthInvalidAccount) {
			return nil, errors.New("state_changed")
		}
		return nil, errors.New("configuration_failed")
	}
	cfg, err := s.repo.Get(ctx, a.ID)
	if err != nil || cfg == nil {
		return nil, errors.New("configuration_failed")
	}
	status := cfg.OpenAIReauthStatus
	status.Email = secret.Email
	return &status, nil
}

// Read only identity already present on the selected account. An expired token
// is still useful here as a consistency hint; authorization validates new
// token identity independently before persisting it.
func openAIReauthAccountEmail(a *Account) string {
	if email := strings.TrimSpace(a.GetCredential("email")); email != "" {
		return email
	}
	if claims, err := openai.DecodeIDToken(a.GetCredential("id_token")); err == nil && strings.TrimSpace(claims.Email) != "" {
		return strings.TrimSpace(claims.Email)
	}
	name := strings.TrimSpace(a.Name)
	if addr, err := mail.ParseAddress(name); err == nil && addr.Address == name && strings.Contains(name, "@") {
		return name
	}
	return ""
}
