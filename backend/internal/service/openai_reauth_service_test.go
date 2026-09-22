package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/stretchr/testify/require"
)

type reauthAccountsStub struct {
	AccountRepository
	account *Account
}

func (r *reauthAccountsStub) GetByID(context.Context, int64) (*Account, error) { return r.account, nil }
func (r *reauthAccountsStub) ListByPlatform(context.Context, string) ([]Account, error) {
	if r.account == nil {
		return []Account{}, nil
	}
	return []Account{*r.account}, nil
}

type reauthProxyStub struct {
	ProxyRepository
	proxy *Proxy
	err   error
}

func (r *reauthProxyStub) GetByID(context.Context, int64) (*Proxy, error) { return r.proxy, r.err }

type reauthCipherStub struct{ plain string }

func (r *reauthCipherStub) Encrypt(value string) (string, error) {
	r.plain = value
	return "ciphertext-only", nil
}
func (r *reauthCipherStub) Decrypt(string) (string, error) { return r.plain, nil }

type reauthLockStub struct {
	GeminiTokenCache
	locked   bool
	fail     bool
	released bool
}

func (r *reauthLockStub) AcquireRefreshLock(context.Context, string, time.Duration) (bool, error) {
	if r.fail {
		return false, errors.New("redis unavailable")
	}
	r.locked = true
	return true, nil
}
func (r *reauthLockStub) ReleaseRefreshLock(context.Context, string) error {
	r.released = true
	return nil
}

type reauthStoreStub struct {
	OpenAIReauthRepository
	config      OpenAIReauthConfig
	valid       bool
	completed   map[string]any
	enqueued    int
	savedCipher string
	failCode    string
	retry       time.Duration
	bound       int
	stages      []string
}

func (r *reauthStoreStub) Get(context.Context, int64) (*OpenAIReauthConfig, error) {
	return &r.config, nil
}
func (r *reauthStoreStub) Validate(context.Context, *OpenAIReauthJob) (bool, error) {
	return r.valid, nil
}
func (r *reauthStoreStub) SetStage(_ context.Context, _ *OpenAIReauthJob, stage string) (bool, error) {
	r.stages = append(r.stages, stage)
	return r.valid, nil
}
func (r *reauthStoreStub) SaveBinding(_ context.Context, a *Account, cipher, email string, enabled bool) error {
	r.bound++
	r.savedCipher = cipher
	r.config.Enabled = enabled
	r.config.AccountID = a.ID
	r.config.Status = "idle"
	r.config.Stage = "idle"
	r.config.Email = email
	return nil
}
func (r *reauthStoreStub) Complete(_ context.Context, _ *OpenAIReauthJob, _ *Account, patch map[string]any) (bool, error) {
	r.completed = patch
	return true, nil
}
func (r *reauthStoreStub) Enqueue(context.Context, int64, string) (bool, error) {
	r.enqueued++
	return true, nil
}
func (r *reauthStoreStub) Save(_ context.Context, _ int64, cipher string, _ bool) error {
	r.savedCipher = cipher
	return nil
}
func (r *reauthStoreStub) Fail(_ context.Context, _ *OpenAIReauthJob, code string, retry time.Duration, _ bool) (bool, error) {
	r.failCode = code
	r.retry = retry
	return true, nil
}

type reauthOAuthStub struct {
	OpenAIOAuthClient
	token      *openai.TokenResponse
	refreshErr error
	proxyURLs  []string
	exchanges  int
	refreshes  int
}

func (r *reauthOAuthStub) RefreshTokenWithClientID(_ context.Context, _, proxyURL, _ string) (*openai.TokenResponse, error) {
	r.refreshes++
	r.proxyURLs = append(r.proxyURLs, proxyURL)
	return r.token, r.refreshErr
}
func (r *reauthOAuthStub) ExchangeCode(_ context.Context, code, verifier, redirect, proxyURL, _ string) (*openai.TokenResponse, error) {
	r.exchanges++
	r.proxyURLs = append(r.proxyURLs, proxyURL)
	if code == "" || verifier == "" || redirect != openai.DefaultRedirectURI {
		return nil, errors.New("invalid exchange")
	}
	return r.token, nil
}

func reauthFixture(t *testing.T) (*OpenAIReauthService, *OpenAIReauthJob, *reauthStoreStub, *reauthOAuthStub, *reauthLockStub) {
	t.Helper()
	p := &Proxy{ID: 2, Protocol: "http", Host: "proxy.example.test", Port: 8080, Status: StatusActive}
	a := &Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true,
		ProxyID: &p.ID, Proxy: p, Concurrency: 7, Priority: 4, Extra: map[string]any{OpenAIReauthEnabledKey: true, OpenAIReauthPendingKey: true, "unrelated": "setting"},
		Credentials: map[string]any{"email": "fixture@example.test", "refresh_token": "old-refresh", "access_token": "old-access", "model_mapping": map[string]any{"a": "b"}}}
	accountRepo := &reauthAccountsStub{account: a}
	proxyRepo := &reauthProxyStub{proxy: p}
	store := &reauthStoreStub{valid: true, config: OpenAIReauthConfig{OpenAIReauthStatus: OpenAIReauthStatus{AccountID: 1, Enabled: true, Generation: 1}}}
	claims, _ := json.Marshal(map[string]any{"email": "fixture@example.test", "exp": time.Now().Add(time.Hour).Unix()})
	token := &openai.TokenResponse{AccessToken: "new-access", RefreshToken: "new-refresh", ExpiresIn: 3600, IDToken: "e30." + base64.RawURLEncoding.EncodeToString(claims) + ".test"}
	oauthClient := &reauthOAuthStub{token: token}
	oauth := NewOpenAIOAuthService(proxyRepo, oauthClient)
	t.Cleanup(oauth.Stop)
	lock := &reauthLockStub{}
	refresh := NewOAuthRefreshAPI(accountRepo, lock)
	secret, _ := json.Marshal(openAIReauthSecret{Email: "fixture@example.test", Password: "fixture-password", TOTPSecret: "JBSWY3DPEHPK3PXP"})
	s := NewOpenAIReauthService(store, accountRepo, proxyRepo, nil, oauth, &reauthCipherStub{plain: string(secret)}, refresh, nil, "http://127.0.0.1:8091", strings.Repeat("t", 32), true)
	job := &OpenAIReauthJob{OpenAIReauthConfig: store.config, LeaseID: "lease", LeaseUntil: time.Now().Add(6 * time.Minute)}
	job.Attempts = 1
	return s, job, store, oauthClient, lock
}

func TestOpenAIReauthRefreshFirstPreservesSettings(t *testing.T) {
	s, job, store, oauth, lock := reauthFixture(t)
	a, _ := s.accounts.GetByID(context.Background(), 1)
	before, _ := json.Marshal(a)
	require.Empty(t, s.execute(context.Background(), job))
	require.Equal(t, 1, oauth.refreshes)
	require.Zero(t, oauth.exchanges)
	require.True(t, lock.released)
	require.Equal(t, []string{"http://proxy.example.test:8080"}, oauth.proxyURLs)
	require.Equal(t, "new-access", store.completed["access_token"])
	require.Equal(t, []string{"refreshing"}, store.stages)
	require.NotContains(t, store.completed, "model_mapping")
	require.NotContains(t, store.completed, "password")
	after, _ := json.Marshal(a)
	require.JSONEq(t, string(before), string(after))
}

func TestOpenAIReauthBrowserFallbackUsesSameProxyAndPKCE(t *testing.T) {
	s, job, store, oauth, _ := reauthFixture(t)
	oauth.refreshErr = errors.New("invalid_grant")
	calls := 0
	worker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		require.Equal(t, "Bearer "+strings.Repeat("t", 32), r.Header.Get("Authorization"))
		var input map[string]string
		require.NoError(t, json.NewDecoder(r.Body).Decode(&input))
		require.Equal(t, "http://proxy.example.test:8080", input["proxy_url"])
		require.Equal(t, "fixture-password", input["password"])
		require.Equal(t, "JBSWY3DPEHPK3PXP", input["totp_secret"])
		u, err := url.Parse(input["auth_url"])
		require.NoError(t, err)
		require.Equal(t, "S256", u.Query().Get("code_challenge_method"))
		require.Equal(t, "login", u.Query().Get("prompt"))
		_ = json.NewEncoder(w).Encode(map[string]string{"code": "fixture-code", "state": u.Query().Get("state")})
	}))
	defer worker.Close()
	s.workerURL = worker.URL
	require.Empty(t, s.execute(context.Background(), job))
	require.Equal(t, 1, calls)
	require.Equal(t, 1, oauth.exchanges)
	require.Equal(t, []string{"http://proxy.example.test:8080", "http://proxy.example.test:8080"}, oauth.proxyURLs)
	require.Equal(t, "new-refresh", store.completed["refresh_token"])
	require.Equal(t, []string{"refreshing", "browser_login", "exchanging"}, store.stages)
}

func TestOpenAIReauthStateMismatchNeverExchangesCode(t *testing.T) {
	s, job, store, oauth, _ := reauthFixture(t)
	oauth.refreshErr = errors.New("invalid_grant")
	worker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"code":"fixture","state":"wrong"}`))
	}))
	defer worker.Close()
	s.workerURL = worker.URL
	require.Equal(t, "oauth_error", s.execute(context.Background(), job))
	require.Zero(t, oauth.exchanges)
	require.Nil(t, store.completed)
}

func TestOpenAIReauthIdentityMismatchNeverPersists(t *testing.T) {
	s, job, store, oauth, _ := reauthFixture(t)
	claims, _ := json.Marshal(map[string]any{"email": "different@example.test", "exp": time.Now().Add(time.Hour).Unix()})
	oauth.token.IDToken = "e30." + base64.RawURLEncoding.EncodeToString(claims) + ".test"
	require.Equal(t, "identity_mismatch", s.execute(context.Background(), job))
	require.Nil(t, store.completed)
}

func TestOpenAIReauthRefusesNetworkAfterStateChanges(t *testing.T) {
	for _, name := range []string{"disabled", "proxy_missing", "proxy_expired", "stale_lease", "redis_down"} {
		t.Run(name, func(t *testing.T) {
			s, job, store, oauth, lock := reauthFixture(t)
			a, _ := s.accounts.GetByID(context.Background(), 1)
			switch name {
			case "disabled":
				a.Schedulable = false
			case "proxy_missing":
				a.ProxyID = nil
			case "proxy_expired":
				past := time.Now().Add(-time.Hour)
				proxies, ok := s.proxies.(*reauthProxyStub)
				require.True(t, ok)
				proxies.proxy.ExpiresAt = &past
			case "stale_lease":
				store.valid = false
			case "redis_down":
				lock.fail = true
			}
			require.NotEmpty(t, s.execute(context.Background(), job))
			require.Zero(t, oauth.refreshes)
			require.Zero(t, oauth.exchanges)
			require.Nil(t, store.completed)
		})
	}
}

func TestOpenAIReauthProxyChangeDuringBrowserCancelsExchange(t *testing.T) {
	s, job, store, oauth, _ := reauthFixture(t)
	oauth.refreshErr = errors.New("invalid_grant")
	worker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var input map[string]string
		_ = json.NewDecoder(r.Body).Decode(&input)
		u, _ := url.Parse(input["auth_url"])
		store.valid = false
		_ = json.NewEncoder(w).Encode(map[string]string{"code": "fixture-code", "state": u.Query().Get("state")})
	}))
	defer worker.Close()
	s.workerURL = worker.URL
	require.Equal(t, "state_changed", s.execute(context.Background(), job))
	require.Zero(t, oauth.exchanges)
	require.Nil(t, store.completed)
}

func TestOpenAIReauthOld401DoesNotQueueNewToken(t *testing.T) {
	s, _, store, _, _ := reauthFixture(t)
	a, _ := s.accounts.GetByID(context.Background(), 1)
	old := *a
	old.Credentials = map[string]any{"access_token": "older-access"}
	require.True(t, s.Trigger(context.Background(), &old, "upstream_401"))
	require.Zero(t, store.enqueued)
	require.True(t, s.Trigger(context.Background(), a, "upstream_401"))
	require.Equal(t, 1, store.enqueued)
}

func TestOpenAIReauthImportKeepsPasswordAndUsesEncryptedStore(t *testing.T) {
	s, _, store, _, _ := reauthFixture(t)
	a, _ := s.accounts.GetByID(context.Background(), 1)
	before, _ := json.Marshal(a)
	results, err := s.Import(context.Background(), OpenAIReauthImportInput{Content: "fixture@example.test---- p\\@ss word ----JBSWY3DPEHPK3PXP\r\n"})
	require.NoError(t, err)
	require.Len(t, results, 1)
	require.Empty(t, results[0].ErrorCode)
	require.Equal(t, "queued", results[0].Status)
	require.Equal(t, "ciphertext-only", store.savedCipher)
	var secret openAIReauthSecret
	cipher, ok := s.encryptor.(*reauthCipherStub)
	require.True(t, ok)
	require.NoError(t, json.Unmarshal([]byte(cipher.plain), &secret))
	require.Equal(t, ` p\@ss word `, secret.Password)
	after, _ := json.Marshal(a)
	require.JSONEq(t, string(before), string(after))
	encoded, _ := json.Marshal(results)
	require.NotContains(t, string(encoded), secret.Password)
	require.NotContains(t, string(encoded), secret.TOTPSecret)
}

func TestOpenAIReauthValidationAndChallengeFailure(t *testing.T) {
	for _, line := range []string{"bad-format", "not-email----password----JBSWY3DPEHPK3PXP", "fixture@example.test--------JBSWY3DPEHPK3PXP", "fixture@example.test----password----123456"} {
		_, code := parseOpenAIReauthLine(line)
		require.NotEmpty(t, code)
	}
	s, job, store, oauth, _ := reauthFixture(t)
	oauth.refreshErr = errors.New("invalid_grant")
	worker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(422)
		_, _ = w.Write([]byte(`{"error_code":"captcha_required"}`))
	}))
	defer worker.Close()
	s.workerURL = worker.URL
	s.run(context.Background(), job)
	require.Equal(t, "captcha_required", store.failCode)
	require.Zero(t, store.retry)
	require.Nil(t, store.completed)
	require.Equal(t, "worker_unavailable", reauthWorkerError("raw response with fixture-password"))
}

func TestOpenAIReauthRejectsUnencryptedRemoteWorker(t *testing.T) {
	s := NewOpenAIReauthService(nil, nil, nil, nil, nil, nil, nil, nil, "http://worker.example.test", strings.Repeat("t", 32), true)
	require.False(t, s.WorkerConfigured())
}

func TestOpenAIReauthRefreshWithoutOptionalIDTokenKeepsGrantIdentity(t *testing.T) {
	s, job, store, oauth, _ := reauthFixture(t)
	a, _ := s.accounts.GetByID(context.Background(), 1)
	a.Credentials["chatgpt_account_id"] = "workspace-original"
	a.Credentials["chatgpt_user_id"] = "user-original"
	oauth.token.IDToken = ""
	require.Empty(t, s.execute(context.Background(), job))
	require.Zero(t, oauth.exchanges)
	require.Equal(t, "workspace-original", store.completed["chatgpt_account_id"])
	require.Equal(t, "user-original", store.completed["chatgpt_user_id"])
}

func TestOpenAIReauthRefreshWithoutEmailUsesImportedIdentity(t *testing.T) {
	s, job, store, oauth, _ := reauthFixture(t)
	a, _ := s.accounts.GetByID(context.Background(), 1)
	a.Name = "fixture@example.test"
	delete(a.Credentials, "email")
	oauth.token.IDToken = ""
	require.Empty(t, s.execute(context.Background(), job))
	require.Equal(t, 1, oauth.refreshes)
	require.Zero(t, oauth.exchanges)
	require.Equal(t, "fixture@example.test", store.completed["email"])
}

func TestOpenAIReauthRefreshWithoutEmailRejectsConflictingStoredIdentity(t *testing.T) {
	s, job, store, oauth, _ := reauthFixture(t)
	a, _ := s.accounts.GetByID(context.Background(), 1)
	a.Credentials["email"] = "different@example.test"
	oauth.token.IDToken = ""
	require.Equal(t, "identity_mismatch", s.execute(context.Background(), job))
	require.Nil(t, store.completed)
}

func TestOpenAIReauthTransientRefreshFailureNeverSubmitsPassword(t *testing.T) {
	s, job, store, oauth, _ := reauthFixture(t)
	oauth.refreshErr = errors.New("status 502 upstream unavailable")
	s.run(context.Background(), job)
	require.Equal(t, "refresh_failed", store.failCode)
	require.Positive(t, store.retry)
	require.Zero(t, oauth.exchanges)
	for _, message := range []string{"invalid_client", "invalid_scope", "status 502", "context deadline exceeded"} {
		require.False(t, openAIReauthRefreshRejected(errors.New(message)))
	}
}

func TestOpenAIReauthShutdownLeavesJobRecoverable(t *testing.T) {
	s, job, store, _, _ := reauthFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	s.run(ctx, job)
	require.Empty(t, store.failCode)
}

func TestOpenAIReauthShadow401QueuesOwnerWithoutRuntimeBlock(t *testing.T) {
	s, _, store, _, _ := reauthFixture(t)
	id := int64(1)
	shadow := &Account{ID: 9, ParentAccountID: &id, Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true}
	limits := &RateLimitService{accountRepo: s.accounts, openAIReauth: s}
	gateway := &OpenAIGatewayService{rateLimitService: limits}
	require.True(t, gateway.handleOpenAIAccountUpstreamError(context.Background(), shadow, 401, nil, []byte(`{"error":{"code":"token_revoked"}}`)))
	require.Equal(t, 1, store.enqueued)
	require.False(t, gateway.isOpenAIAccountRuntimeBlocked(shadow))
}

func TestOpenAIReauthOldShadow401UsesActualRequestToken(t *testing.T) {
	s, _, store, _, _ := reauthFixture(t)
	owner, _ := s.accounts.GetByID(context.Background(), 1)
	delete(owner.Extra, OpenAIReauthPendingKey)
	shadow := &Account{ID: 9, ParentAccountID: &owner.ID, Platform: PlatformOpenAI, Type: AccountTypeOAuth,
		Status: StatusActive, Schedulable: true, ProxyID: owner.ProxyID, Proxy: owner.Proxy}
	selected := attachSelectionProfitGate(context.Background(), &AccountSelectionResult{Account: shadow}).Account
	require.NotSame(t, shadow, selected)
	limits := &RateLimitService{accountRepo: s.accounts, openAIReauth: s}
	gateway := &OpenAIGatewayService{accountRepo: s.accounts, rateLimitService: limits}
	token, _, err := gateway.GetAccessToken(context.Background(), selected)
	require.NoError(t, err)
	require.Equal(t, "old-access", token)
	require.Empty(t, shadow.openAIReauthObservedTokenHash, "shared source must not acquire request state")
	require.NotEmpty(t, selected.openAIReauthObservedTokenHash)
	owner.Credentials["access_token"] = "newly-rotated-access"
	require.True(t, gateway.handleOpenAIAccountUpstreamError(context.Background(), selected, 401, nil, []byte(`{"error":{"code":"token_revoked"}}`)))
	require.Zero(t, store.enqueued, "old shadow request must not revoke the new parent grant")
	require.False(t, gateway.isOpenAIAccountRuntimeBlocked(selected))
	encoded, err := json.Marshal(selected)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), selected.openAIReauthObservedTokenHash)
}

func TestOpenAIReauthNewToken401UsesProviderResultInsteadOfStaleSelection(t *testing.T) {
	s, _, store, _, _ := reauthFixture(t)
	owner, _ := s.accounts.GetByID(context.Background(), 1)
	delete(owner.Extra, OpenAIReauthPendingKey)
	selected := attachSelectionProfitGate(context.Background(), &AccountSelectionResult{Account: owner}).Account
	owner.Credentials = map[string]any{"access_token": "newly-rotated-access", "refresh_token": "new-refresh", "expires_at": time.Now().Add(time.Hour).Unix()}
	provider := NewOpenAITokenProvider(s.accounts, nil, s.oauth)
	provider.openAIReauth = s
	limits := &RateLimitService{accountRepo: s.accounts, openAIReauth: s}
	gateway := &OpenAIGatewayService{accountRepo: s.accounts, rateLimitService: limits, openAITokenProvider: provider}
	token, _, err := gateway.GetAccessToken(context.Background(), selected)
	require.NoError(t, err)
	require.Equal(t, "newly-rotated-access", token)
	require.Equal(t, "old-access", selected.GetCredential("access_token"))
	require.True(t, gateway.handleOpenAIAccountUpstreamError(context.Background(), selected, 401, nil, []byte(`{"error":{"code":"token_revoked"}}`)))
	require.Equal(t, 1, store.enqueued, "a genuine failure of the returned new token must trigger recovery")
	require.False(t, gateway.isOpenAIAccountRuntimeBlocked(selected))
}
