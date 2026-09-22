package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base32"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/mail"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/google/uuid"
)

// Passwords and TOTP seeds are encrypted as one document, outside account JSON.
type openAIReauthSecret struct {
	Email      string `json:"email"`
	Password   string `json:"password"`
	TOTPSecret string `json:"totp_secret"`
}

type openAIReauthCreateContextKey struct{}

type OpenAIReauthService struct {
	repo                   OpenAIReauthRepository
	accounts               AccountRepository
	proxies                ProxyRepository
	admin                  AdminService
	oauth                  *OpenAIOAuthService
	encryptor              SecretEncryptor
	refresh                *OAuthRefreshAPI
	cache                  TokenCacheInvalidator
	workerURL, workerToken string
	keyConfigured          bool
	client                 *http.Client
	cancel                 context.CancelFunc
	wg                     sync.WaitGroup
	importMu               sync.Mutex
}

func NewOpenAIReauthService(repo OpenAIReauthRepository, accounts AccountRepository, proxies ProxyRepository,
	admin AdminService, oauth *OpenAIOAuthService, encryptor SecretEncryptor, refresh *OAuthRefreshAPI,
	cache TokenCacheInvalidator, workerURL, workerToken string, keyConfigured bool) *OpenAIReauthService {
	workerURL = strings.TrimRight(strings.TrimSpace(workerURL), "/")
	u, err := url.Parse(workerURL)
	// Do not send reusable account passwords over an unencrypted remote link.
	if err != nil || u.User != nil || u.RawQuery != "" || u.Fragment != "" ||
		(u.Scheme != "https" && (u.Scheme != "http" || (u.Hostname() != "127.0.0.1" && u.Hostname() != "localhost" && u.Hostname() != "::1"))) {
		workerURL = ""
	}
	return &OpenAIReauthService{repo: repo, accounts: accounts, proxies: proxies, admin: admin, oauth: oauth,
		encryptor: encryptor, refresh: refresh, cache: cache, workerURL: workerURL, workerToken: strings.TrimSpace(workerToken), keyConfigured: keyConfigured,
		client: &http.Client{Timeout: 4 * time.Minute, Transport: &http.Transport{Proxy: nil},
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }},
	}
}

func OpenAIReauthEnabled(a *Account) bool {
	return a != nil && a.Platform == PlatformOpenAI && a.Type == AccountTypeOAuth && a.Extra[OpenAIReauthEnabledKey] == true
}

func OpenAIReauthPending(a *Account) bool { return a != nil && a.Extra[OpenAIReauthPendingKey] == true }

func openAIReauthAccessTokenHash(token string) string {
	if token == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func (s *RateLimitService) tryOpenAIReauth(ctx context.Context, a *Account, status int) bool {
	if s == nil || s.openAIReauth == nil || status != http.StatusUnauthorized {
		return false
	}
	owner, err := resolveCredentialAccount(ctx, s.accountRepo, a)
	if err != nil || owner == nil {
		return false
	}
	if a.openAIReauthObservedTokenHash != "" {
		if !a.IsShadow() {
			fresh, err := s.accountRepo.GetByID(ctx, owner.ID)
			if err != nil || fresh == nil {
				return OpenAIReauthEnabled(owner)
			}
			owner = fresh
		}
		if OpenAIReauthEnabled(owner) && a.openAIReauthObservedTokenHash != openAIReauthAccessTokenHash(owner.GetCredential("access_token")) {
			return true
		}
		// Freeze the matching actual token while Trigger acquires its locks and
		// rechecks authoritative state. Never copy credentials into a shadow.
		snapshot := *owner
		snapshot.Credentials = make(map[string]any, len(owner.Credentials))
		for key, value := range owner.Credentials {
			snapshot.Credentials[key] = value
		}
		owner = &snapshot
	}
	return s.openAIReauth.Trigger(ctx, owner, "upstream_401")
}

func (s *OpenAIReauthService) WorkerConfigured() bool {
	return s != nil && s.workerURL != "" && len(s.workerToken) >= 32 && !strings.ContainsAny(s.workerToken, " \t\r\n")
}
func (s *OpenAIReauthService) EncryptionConfigured() bool {
	return s != nil && s.keyConfigured && s.encryptor != nil
}

// StrictOpenAIProxy rejects every condition that could turn proxy resolution
// into direct access or pool rotation. A provider must supply a static/sticky IP.
func StrictOpenAIProxy(ctx context.Context, a *Account, repo ProxyRepository) (*Proxy, error) {
	if a == nil || a.ProxyID == nil || repo == nil || a.IsShadow() || a.ProxyFallbackOriginID != nil {
		return nil, ErrOpenAIReauthInvalidAccount
	}
	pool, err := ParseAccountProxyPool(a.Extra[AccountProxyPoolExtraKey])
	if err != nil || len(pool) > 1 || (len(pool) == 1 && pool[0].ProxyID != *a.ProxyID) {
		return nil, ErrOpenAIReauthInvalidAccount
	}
	p, err := repo.GetByID(ctx, *a.ProxyID)
	if err != nil || p == nil || !p.IsActive() || p.IsExpired(time.Now()) {
		return nil, ErrOpenAIReauthInvalidAccount
	}
	u, err := url.Parse(p.URL())
	if err != nil || u.Hostname() == "" || (u.Scheme != "http" && u.Scheme != "https" && u.Scheme != "socks5") {
		return nil, ErrOpenAIReauthInvalidAccount
	}
	if u.Scheme == "socks5" && u.User != nil {
		return nil, ErrOpenAIReauthInvalidAccount
	}
	return p, nil
}

// CheckForUse reads authoritative state before a cached token can be returned.
func (s *OpenAIReauthService) CheckForUse(ctx context.Context, a *Account) (*Account, error) {
	if OpenAIReauthPending(a) {
		return nil, errors.New("reauth_account_pending")
	}
	if !OpenAIReauthEnabled(a) {
		return a, nil
	}
	fresh, err := s.accounts.GetByID(ctx, a.ID)
	if err != nil || fresh == nil {
		return nil, errors.New("reauth_account_unavailable")
	}
	if OpenAIReauthPending(fresh) || !fresh.IsActive() || !fresh.Schedulable {
		return nil, errors.New("reauth_account_pending")
	}
	proxy, err := StrictOpenAIProxy(ctx, fresh, s.proxies)
	if err != nil {
		return nil, err
	}
	fresh.Proxy = proxy
	// The caller's routing snapshot must not send this token through an old proxy.
	if a.ProxyID == nil || fresh.ProxyID == nil || *a.ProxyID != *fresh.ProxyID || a.Proxy == nil || fresh.Proxy == nil || a.Proxy.URL() != fresh.Proxy.URL() {
		return nil, errors.New("reauth_proxy_changed")
	}
	return fresh, nil
}

// Trigger returns true when this service owns the failure, even if the durable
// queue is temporarily unavailable. The caller must not permanently disable it.
func (s *OpenAIReauthService) Trigger(ctx context.Context, a *Account, reason string) bool {
	if s == nil || !OpenAIReauthEnabled(a) {
		return false
	}
	// Serialize enqueue with every normal token rotation. Otherwise an old 401
	// arriving mid-refresh can snapshot the old token and leave a stale job.
	if s.refresh == nil || s.refresh.tokenCache == nil {
		return true
	}
	key := OpenAITokenCacheKey(a)
	mu := s.refresh.getLocalLock(key)
	if mu.Lock(ctx) != nil {
		return true
	}
	defer mu.Unlock()
	locked, lockErr := s.refresh.tokenCache.AcquireRefreshLock(ctx, key, 30*time.Second)
	if lockErr != nil || !locked {
		return true
	}
	defer s.refresh.releaseRefreshLock(ctx, key)
	fresh, err := s.accounts.GetByID(ctx, a.ID)
	if err != nil || fresh == nil {
		return true
	}
	if !OpenAIReauthEnabled(fresh) {
		return false
	}
	// A delayed 401 from an old request must not revoke a successful rotation.
	if a.GetCredential("access_token") != fresh.GetCredential("access_token") {
		return true
	}
	_, _ = s.repo.Enqueue(ctx, a.ID, reason)
	if s.cache != nil {
		_ = s.cache.InvalidateToken(ctx, fresh)
	}
	return true
}

func (s *OpenAIReauthService) Start() {
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		timer := time.NewTicker(3 * time.Second)
		defer timer.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-timer.C:
				if !s.WorkerConfigured() || !s.EncryptionConfigured() {
					continue
				}
				job, err := s.repo.Claim(ctx, uuid.NewString(), 6*time.Minute)
				if err == nil && job != nil {
					s.run(ctx, job)
				}
			}
		}
	}()
}

func (s *OpenAIReauthService) Stop() {
	if s != nil && s.cancel != nil {
		s.cancel()
		s.wg.Wait()
	}
	if s != nil && s.client != nil {
		s.client.CloseIdleConnections()
	}
}

func (s *OpenAIReauthService) run(parent context.Context, job *OpenAIReauthJob) {
	ctx, cancel := context.WithTimeout(parent, 4*time.Minute)
	defer cancel()
	code := s.execute(ctx, job)
	// Shutdown is not evidence that credentials or account identity are bad.
	// Leave the lease recoverable by another process or after restart.
	if parent.Err() != nil {
		return
	}
	if ctx.Err() != nil {
		code = "login_timeout"
	}
	if code == "" {
		return
	}
	retry := time.Duration(0)
	switch code {
	case "worker_unavailable", "proxy_unavailable", "login_timeout", "busy", "lock_unavailable", "refresh_failed":
		retry = time.Duration(job.Attempts) * 30 * time.Second
	}
	cleanup, done := context.WithTimeout(context.WithoutCancel(parent), 5*time.Second)
	defer done()
	_, _ = s.repo.Fail(cleanup, job, code, retry, retry == 0)
}

func (s *OpenAIReauthService) execute(ctx context.Context, job *OpenAIReauthJob) string {
	if s.refresh == nil || s.refresh.tokenCache == nil {
		return "lock_unavailable"
	}
	a, err := s.accounts.GetByID(ctx, job.AccountID)
	if err != nil || a == nil {
		return "account_changed"
	}
	key := OpenAITokenCacheKey(a)
	mu := s.refresh.getLocalLock(key)
	if mu.Lock(ctx) != nil {
		return "lock_unavailable"
	}
	defer mu.Unlock()
	ok, err := s.refresh.tokenCache.AcquireRefreshLock(ctx, key, 6*time.Minute)
	if err != nil || !ok {
		return "lock_unavailable"
	}
	defer s.refresh.releaseRefreshLock(ctx, key)
	a, err = s.accounts.GetByID(ctx, job.AccountID)
	if err != nil || a == nil || !OpenAIReauthEnabled(a) || !OpenAIReauthPending(a) || !a.IsActive() || !a.Schedulable {
		return "account_changed"
	}
	cfg, err := s.repo.Get(ctx, a.ID)
	if err != nil || !cfg.Enabled || cfg.Generation != job.Generation {
		return "account_changed"
	}
	if valid, err := s.repo.Validate(ctx, job); err != nil || !valid {
		return "state_changed"
	}
	p, err := StrictOpenAIProxy(ctx, a, s.proxies)
	if err != nil {
		return "proxy_unavailable"
	}
	plain, err := s.encryptor.Decrypt(job.EncryptedSecret)
	if err != nil {
		return "secret_unavailable"
	}
	var secret openAIReauthSecret
	if json.Unmarshal([]byte(plain), &secret) != nil || secret.Email == "" || secret.Password == "" || secret.TOTPSecret == "" {
		return "secret_unavailable"
	}
	var token *OpenAITokenInfo
	if a.GetOpenAIRefreshToken() != "" {
		if ok, err := s.repo.SetStage(ctx, job, "refreshing"); err != nil || !ok {
			return "state_changed"
		}
		token, err = s.oauth.RefreshTokenWithClientID(ctx, a.GetOpenAIRefreshToken(), p.URL(), a.GetCredential("client_id"))
		if err != nil {
			if !openAIReauthRefreshRejected(err) {
				if isNonRetryableRefreshError(err) {
					return "oauth_configuration_error"
				}
				return "refresh_failed"
			}
			token = nil
		}
		if token != nil {
			// Refresh retains the identity of its existing grant. Optional claims
			// can be omitted by the provider; an explicit conflicting claim still
			// fails the identity check below. New browser grants remain strict.
			if token.Email == "" {
				token.Email = a.GetCredential("email")
				if token.Email == "" {
					token.Email = secret.Email
				}
			}
			if token.ChatGPTAccountID == "" {
				token.ChatGPTAccountID = a.GetCredential("chatgpt_account_id")
			}
			if token.ChatGPTUserID == "" {
				token.ChatGPTUserID = a.GetCredential("chatgpt_user_id")
			}
			if token.OrganizationID == "" {
				token.OrganizationID = a.GetCredential("organization_id")
			}
		}
	}
	if token == nil {
		if valid, err := s.repo.Validate(ctx, job); err != nil || !valid {
			return "state_changed"
		}
		auth, err := s.oauth.GenerateAuthURL(ctx, a.ProxyID, openai.DefaultRedirectURI, PlatformOpenAI)
		if err != nil {
			return "oauth_error"
		}
		defer s.oauth.sessionStore.Delete(auth.SessionID)
		session, ok := s.oauth.sessionStore.Get(auth.SessionID)
		if !ok || session.ProxyURL != p.URL() {
			return "proxy_changed"
		}
		u, err := url.Parse(auth.AuthURL)
		if err != nil {
			return "oauth_error"
		}
		q := u.Query()
		q.Set("prompt", "login")
		q.Set("login_hint", secret.Email)
		u.RawQuery = q.Encode()
		if ok, err := s.repo.SetStage(ctx, job, "browser_login"); err != nil || !ok {
			return "state_changed"
		}
		body, _ := json.Marshal(map[string]string{"email": secret.Email, "password": secret.Password, "totp_secret": secret.TOTPSecret,
			"auth_url": u.String(), "redirect_uri": openai.DefaultRedirectURI, "proxy_url": p.URL(), "expected_email": secret.Email, "workspace_id": a.GetCredential("chatgpt_account_id")})
		// #nosec G704 -- The constructor validates the fixed operator-configured worker URL; request data cannot select its destination.
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.workerURL+"/login", bytes.NewReader(body))
		if err != nil {
			return "worker_unavailable"
		}
		req.Header.Set("Authorization", "Bearer "+s.workerToken)
		req.Header.Set("Content-Type", "application/json")
		// #nosec G704 -- The destination is the fixed configured worker, and the client rejects redirects.
		resp, err := s.client.Do(req)
		if err != nil {
			return "worker_unavailable"
		}
		defer func() { _ = resp.Body.Close() }()
		var result struct {
			Code      string `json:"code"`
			State     string `json:"state"`
			ErrorCode string `json:"error_code"`
		}
		if json.NewDecoder(io.LimitReader(resp.Body, 65536)).Decode(&result) != nil {
			return "worker_unavailable"
		}
		if resp.StatusCode != http.StatusOK {
			return reauthWorkerError(result.ErrorCode)
		}
		if valid, err := s.repo.Validate(ctx, job); err != nil || !valid {
			return "state_changed"
		}
		// Proxy edits during a browser attempt cancel the exchange.
		fresh, err := s.accounts.GetByID(ctx, a.ID)
		if err != nil {
			return "account_changed"
		}
		current, err := StrictOpenAIProxy(ctx, fresh, s.proxies)
		if err != nil || fresh.ProxyID == nil || *fresh.ProxyID != *a.ProxyID || current.URL() != p.URL() {
			return "proxy_changed"
		}
		if ok, err := s.repo.SetStage(ctx, job, "exchanging"); err != nil || !ok {
			return "state_changed"
		}
		token, err = s.oauth.ExchangeCode(ctx, &OpenAIExchangeCodeInput{SessionID: auth.SessionID, Code: result.Code, State: result.State})
		if err != nil {
			return "oauth_error"
		}
	}
	if !reauthIdentityMatches(a, secret.Email, token) {
		return "identity_mismatch"
	}
	patch := s.oauth.BuildAccountCredentials(token)
	patch["_token_version"] = time.Now().UnixMilli()
	if ctx.Err() != nil {
		return "login_timeout"
	}
	ok, err = s.repo.Complete(ctx, job, a, patch)
	if err != nil {
		return "persistence_failed"
	}
	if !ok {
		return "state_changed"
	}
	if s.cache != nil {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_ = s.cache.InvalidateToken(cleanup, a)
	}
	return ""
}

func reauthIdentityMatches(a *Account, email string, t *OpenAITokenInfo) bool {
	if t == nil || t.AccessToken == "" || t.ExpiresAt <= time.Now().Unix() || !strings.EqualFold(strings.TrimSpace(t.Email), email) {
		return false
	}
	for key, value := range map[string]string{"chatgpt_account_id": t.ChatGPTAccountID, "chatgpt_user_id": t.ChatGPTUserID, "organization_id": t.OrganizationID} {
		if expected := strings.TrimSpace(a.GetCredential(key)); expected != "" && expected != strings.TrimSpace(value) {
			return false
		}
	}
	return true
}

func reauthWorkerError(code string) string {
	switch code {
	case "busy", "login_timeout", "proxy_unavailable", "unsupported_proxy", "captcha_required", "email_verification_required", "device_verification_required", "account_disabled", "credentials_rejected", "login_flow_unsupported", "oauth_error", "identity_mismatch", "browser_unavailable", "workspace_selection_required", "rate_limited", "untrusted_login_origin", "login_failed", "cancelled":
		return code
	}
	return "worker_unavailable"
}

// Only an expired or revoked account grant warrants another password login.
func openAIReauthRefreshRejected(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	for _, code := range []string{"invalid_grant", "invalid_refresh_token", "refresh_token_expired", "token_expired", "refresh_token_reused", "refresh_token_invalidated", "no refresh token available"} {
		if strings.Contains(msg, code) {
			return true
		}
	}
	return false
}

func parseOpenAIReauthLine(line string) (openAIReauthSecret, string) {
	parts := strings.Split(line, "----")
	if len(parts) != 3 {
		return openAIReauthSecret{}, "invalid_format"
	}
	return validateOpenAIReauthSecret(parts[0], parts[1], parts[2])
}

func validateOpenAIReauthSecret(rawEmail, password, rawSeed string) (openAIReauthSecret, string) {
	email := strings.TrimSpace(rawEmail)
	addr, err := mail.ParseAddress(email)
	if err != nil || addr.Address != email || !strings.Contains(email, "@") || len(email) > 320 {
		return openAIReauthSecret{}, "invalid_email"
	}
	if password == "" || len(password) > 1024 {
		return openAIReauthSecret{}, "invalid_format"
	}
	seed := strings.ToUpper(strings.Join(strings.Fields(rawSeed), ""))
	seed = strings.TrimRight(seed, "=")
	decoded, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(seed)
	if err != nil || len(decoded) < 10 || len(decoded) > 128 {
		return openAIReauthSecret{}, "invalid_totp"
	}
	return openAIReauthSecret{Email: strings.ToLower(email), Password: password, TOTPSecret: seed}, ""
}

type OpenAIReauthImportInput struct {
	Mode     string  `json:"mode"`
	Content  string  `json:"content"`
	ProxyID  *int64  `json:"proxy_id"`
	GroupIDs []int64 `json:"group_ids"`
}
type OpenAIReauthImportResult struct {
	Line      int    `json:"line"`
	AccountID int64  `json:"account_id,omitempty"`
	Email     string `json:"email,omitempty"`
	Status    string `json:"status"`
	ErrorCode string `json:"error_code,omitempty"`
}

func (s *OpenAIReauthService) Import(ctx context.Context, input OpenAIReauthImportInput) ([]OpenAIReauthImportResult, error) {
	if input.Mode != "" && input.Mode != "create" && input.Mode != "bind" {
		return nil, errors.New("invalid_mode")
	}
	if !s.WorkerConfigured() {
		return nil, errors.New("worker_not_configured")
	}
	if !s.EncryptionConfigured() {
		return nil, errors.New("encryption_key_not_configured")
	}
	if len(input.Content) > 1024*1024 || len(strings.Split(input.Content, "\n")) > 500 {
		return nil, errors.New("import_too_large")
	}
	s.importMu.Lock()
	defer s.importMu.Unlock()
	accounts, err := s.accounts.ListByPlatform(ctx, PlatformOpenAI)
	if err != nil {
		return nil, errors.New("import_failed")
	}
	index := make(map[string][]*Account)
	for i := range accounts {
		a := &accounts[i]
		if a.IsShadow() {
			continue
		}
		key := strings.ToLower(openAIReauthAccountEmail(a))
		if key == "" {
			key = strings.ToLower(strings.TrimSpace(a.Name))
		}
		index[key] = append(index[key], a)
	}
	results := make([]OpenAIReauthImportResult, 0)
	seen := make(map[string]bool)
	for i, line := range strings.Split(input.Content, "\n") {
		line = strings.TrimSuffix(line, "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}
		result := OpenAIReauthImportResult{Line: i + 1, Status: "failed"}
		secret, code := parseOpenAIReauthLine(line)
		if code == "" {
			result.Email = secret.Email
			if seen[secret.Email] {
				code = "duplicate_email"
			}
			seen[secret.Email] = true
		}
		if code == "" {
			code = s.importOne(ctx, input, secret, index, &result)
		}
		result.ErrorCode = code
		if code == "" && result.Status == "failed" {
			result.Status = "queued"
		}
		results = append(results, result)
	}
	return results, nil
}

func (s *OpenAIReauthService) importOne(ctx context.Context, input OpenAIReauthImportInput, secret openAIReauthSecret, index map[string][]*Account, result *OpenAIReauthImportResult) string {
	items := index[secret.Email]
	if input.Mode == "create" && len(items) > 0 {
		return "account_exists"
	}
	if input.Mode == "bind" && len(items) == 0 {
		return "account_not_found"
	}
	if len(items) > 1 {
		return "account_conflict"
	}
	var a *Account
	if len(items) == 1 {
		a = items[0]
		if a.Type != AccountTypeOAuth || a.IsOpenAIPersonalAccessToken() || (input.Mode == "" && (!a.IsActive() || !a.Schedulable)) {
			return "account_conflict"
		}
		if input.Mode == "bind" {
			status, err := s.bindCredentials(ctx, a, secret, true)
			if err != nil {
				return err.Error()
			}
			result.AccountID = status.AccountID
			result.Status = "bound"
			return ""
		}
	}
	if a == nil {
		if input.ProxyID == nil {
			return "proxy_required"
		}
		a = &Account{ProxyID: input.ProxyID, Extra: map[string]any{}}
	}
	if _, err := StrictOpenAIProxy(ctx, a, s.proxies); err != nil {
		return "invalid_proxy"
	}
	plain, _ := json.Marshal(secret)
	cipher, err := s.encryptor.Encrypt(string(plain))
	if err != nil {
		return "import_failed"
	}
	created := a.ID == 0
	if created {
		a, err = s.admin.CreateAccount(context.WithValue(ctx, openAIReauthCreateContextKey{}, true), &CreateAccountInput{Name: secret.Email, Platform: PlatformOpenAI, Type: AccountTypeOAuth,
			Credentials: map[string]any{"email": secret.Email}, Extra: map[string]any{OpenAIReauthEnabledKey: true, OpenAIReauthPendingKey: true}, ProxyID: input.ProxyID, Concurrency: 1, Priority: 1, GroupIDs: input.GroupIDs})
		if err != nil {
			return "import_failed"
		}
		index[secret.Email] = []*Account{a}
	}
	result.AccountID = a.ID
	if err = s.repo.Save(ctx, a.ID, cipher, true); err != nil {
		if created {
			cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			defer cancel()
			if s.accounts.Delete(cleanup, a.ID) == nil {
				delete(index, secret.Email)
				result.AccountID = 0
			}
		}
		return "import_failed"
	}
	if s.cache != nil {
		_ = s.cache.InvalidateToken(ctx, a)
	}
	return ""
}

func (s *OpenAIReauthService) List(ctx context.Context) ([]OpenAIReauthStatus, error) {
	statuses, err := s.repo.ListStatuses(ctx, 5000)
	if err == nil && statuses == nil {
		statuses = []OpenAIReauthStatus{}
	}
	return statuses, err
}
func (s *OpenAIReauthService) SetEnabled(ctx context.Context, id int64, enabled bool) error {
	if enabled && !s.WorkerConfigured() {
		return errors.New("worker_not_configured")
	}
	if enabled && !s.EncryptionConfigured() {
		return errors.New("encryption_key_not_configured")
	}
	a, err := s.accounts.GetByID(ctx, id)
	if err != nil || a == nil || a.Platform != PlatformOpenAI || a.Type != AccountTypeOAuth || a.IsShadow() || a.IsOpenAIPersonalAccessToken() {
		return errors.New("invalid_account")
	}
	if enabled {
		if _, err := StrictOpenAIProxy(ctx, a, s.proxies); err != nil {
			return errors.New("invalid_proxy")
		}
	}
	return s.repo.SaveBinding(ctx, a, "", "", enabled)
}
func (s *OpenAIReauthService) Retry(ctx context.Context, id int64) error {
	if !s.WorkerConfigured() {
		return errors.New("worker_not_configured")
	}
	if !s.EncryptionConfigured() {
		return errors.New("encryption_key_not_configured")
	}
	a, err := s.accounts.GetByID(ctx, id)
	if err != nil || a == nil || !a.IsActive() || !a.Schedulable {
		return errors.New("account_disabled")
	}
	if _, err := StrictOpenAIProxy(ctx, a, s.proxies); err != nil {
		return errors.New("invalid_proxy")
	}
	return s.repo.Save(ctx, id, "", true)
}
