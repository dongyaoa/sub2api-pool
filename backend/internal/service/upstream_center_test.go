//go:build unit

package service

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type upstreamTestEncryptor struct{}

func (upstreamTestEncryptor) Encrypt(value string) (string, error) { return "encrypted:" + value, nil }
func (upstreamTestEncryptor) Decrypt(value string) (string, error) {
	if !strings.HasPrefix(value, "encrypted:") {
		return "", errors.New("bad ciphertext")
	}
	return strings.TrimPrefix(value, "encrypted:"), nil
}

type upstreamTestRepo struct {
	UpstreamCenterRepository
	target               *UpstreamTarget
	saved                *UpstreamTarget
	records              []*UpstreamHistoryRecord
	manual               bool
	completeContextError error
}

func (r *upstreamTestRepo) GetTarget(_ context.Context, id int64) (*UpstreamTarget, error) {
	if r.target == nil || r.target.ID != id {
		return nil, ErrUpstreamNotFound
	}
	copy := *r.target
	return &copy, nil
}
func (r *upstreamTestRepo) GetSupplier(_ context.Context, id int64) (*UpstreamSupplier, error) {
	return &UpstreamSupplier{ID: id, Name: "Supplier"}, nil
}
func (r *upstreamTestRepo) SaveTarget(_ context.Context, target *UpstreamTarget) error {
	copy := *target
	r.saved = &copy
	return nil
}
func (r *upstreamTestRepo) ClaimCheck(_ context.Context, _ int64, _ string, manual bool) (bool, error) {
	r.manual = manual
	return true, nil
}
func (r *upstreamTestRepo) CompleteCheck(ctx context.Context, _ int64, _ string, records []*UpstreamHistoryRecord) (bool, error) {
	r.completeContextError = ctx.Err()
	r.records = records
	return true, nil
}
func (r *upstreamTestRepo) ReleaseCheck(context.Context, int64, string) error { return nil }

type upstreamTestAccounts struct {
	AccountRepository
	account *Account
}

func (a upstreamTestAccounts) GetByID(context.Context, int64) (*Account, error) {
	return a.account, nil
}

func validUpstreamTestTarget() *UpstreamTarget {
	return &UpstreamTarget{ID: 7, Name: "Claude", Provider: "anthropic", APIMode: "chat_completions", Endpoint: "https://8.8.8.8", APIKeyEncrypted: "encrypted:secret-testing-key", Models: []string{"claude-test"}, Enabled: true, IntervalSeconds: 300, TimeoutSeconds: 45, DegradedThresholdMs: 6000, WalletRef: "default", AccountIDs: []int64{}}
}

func TestUpstreamCreateDefaultsAndExplicitConfiguration(t *testing.T) {
	for _, supplier := range []json.RawMessage{nil, json.RawMessage("2")} {
		repo := &upstreamTestRepo{}
		svc := NewUpstreamCenterService(repo, upstreamTestEncryptor{}, nil, nil)
		name, endpoint, key := "New monitor", "https://8.8.8.8", "private-test-key"
		input := UpstreamTargetInput{SupplierID: supplier, Name: &name, Endpoint: &endpoint, APIKey: &key}
		created, err := svc.SaveTarget(context.Background(), 0, input)
		require.NoError(t, err)
		require.Equal(t, []string{"gpt-5.6-sol"}, created.Models)
		require.Equal(t, 30, created.IntervalSeconds)
		require.Equal(t, created.Models, repo.saved.Models)
		require.Equal(t, created.IntervalSeconds, repo.saved.IntervalSeconds)
		models, interval := []string{"custom-model"}, 120
		input.Models, input.IntervalSeconds = &models, &interval
		created, err = svc.SaveTarget(context.Background(), 0, input)
		require.NoError(t, err)
		require.Equal(t, models, created.Models)
		require.Equal(t, interval, created.IntervalSeconds)
		models = []string{}
		_, err = svc.SaveTarget(context.Background(), 0, input)
		require.ErrorIs(t, err, ErrUpstreamInvalid, "an explicitly empty model list must not silently select the default")
	}
}

func TestUpstreamThirtySecondIntervalBounds(t *testing.T) {
	for _, seconds := range []int{29, 30, 31, 3600, 3601} {
		target := validUpstreamTestTarget()
		target.IntervalSeconds = seconds
		err := validateUpstreamTargetConfig(target, "private-test-key", false)
		if seconds < 30 || seconds > 3600 {
			require.ErrorIs(t, err, ErrUpstreamInvalid)
		} else {
			require.NoError(t, err)
		}
	}
}

func TestUpstreamBlankKeyUpdatePreservesCiphertextAndRedactsJSON(t *testing.T) {
	repo := &upstreamTestRepo{target: validUpstreamTestTarget()}
	svc := NewUpstreamCenterService(repo, upstreamTestEncryptor{}, nil, nil)
	blank, name := "", "Renamed"
	result, err := svc.SaveTarget(context.Background(), 7, UpstreamTargetInput{Name: &name, APIKey: &blank})
	require.NoError(t, err)
	require.Equal(t, repo.target.APIKeyEncrypted, repo.saved.APIKeyEncrypted)
	require.False(t, repo.saved.ResetBindings)
	require.Equal(t, []string{"claude-test"}, result.Models, "editing an existing monitor must preserve its chosen models")
	require.Equal(t, 300, result.IntervalSeconds, "new creation defaults must not rewrite an existing schedule")
	encoded, err := json.Marshal(result)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "secret-testing-key")
	require.NotContains(t, string(encoded), "api_key_encrypted")
	require.NotContains(t, string(encoded), "api_key_fingerprint")
}

func TestUpstreamRejectsBindingDifferentKey(t *testing.T) {
	repo := &upstreamTestRepo{target: validUpstreamTestTarget()}
	sid := int64(2)
	repo.target.SupplierID = &sid
	accounts := upstreamTestAccounts{account: &Account{ID: 9, Type: AccountTypeAPIKey, Credentials: map[string]any{"base_url": "https://8.8.8.8", "api_key": "different-key"}}}
	svc := NewUpstreamCenterService(repo, upstreamTestEncryptor{}, accounts, nil)
	ids := []int64{9}
	_, err := svc.SaveTarget(context.Background(), 7, UpstreamTargetInput{AccountIDs: &ids})
	require.ErrorIs(t, err, ErrUpstreamInvalid)
	require.Nil(t, repo.saved)
}

func TestUpstreamPrivateEndpointRejected(t *testing.T) {
	target := validUpstreamTestTarget()
	target.Endpoint = "https://127.0.0.1"
	require.ErrorIs(t, validateUpstreamTargetConfig(target, "key", true), ErrChannelMonitorEndpointPrivate)
	target.Endpoint = "https://user:password@8.8.8.8"
	require.ErrorIs(t, validateUpstreamTargetConfig(target, "key", true), ErrChannelMonitorInvalidEndpoint)
}

func TestUpstreamCanPauseExistingTargetDuringDNSOutage(t *testing.T) {
	repo := &upstreamTestRepo{target: validUpstreamTestTarget()}
	repo.target.Endpoint = "https://unresolvable.invalid"
	svc := NewUpstreamCenterService(repo, upstreamTestEncryptor{}, nil, nil)
	enabled := false
	result, err := svc.SaveTarget(context.Background(), 7, UpstreamTargetInput{Enabled: &enabled})
	require.NoError(t, err)
	require.False(t, result.Enabled)
	privateEndpoint := "https://127.0.0.1"
	replacementKey := "replacement-key"
	_, err = svc.SaveTarget(context.Background(), 7, UpstreamTargetInput{Endpoint: &privateEndpoint, APIKey: &replacementKey})
	require.ErrorIs(t, err, ErrChannelMonitorEndpointPrivate)
}

func TestUpstreamManualPausedCheckPersistsAfterCallerDisconnect(t *testing.T) {
	target := validUpstreamTestTarget()
	target.Enabled = false
	repo := &upstreamTestRepo{target: target}
	svc := NewUpstreamCenterService(repo, upstreamTestEncryptor{}, nil, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	latency := 7000
	status := 200
	svc.checkModel = func(context.Context, string, string, string, string, *CheckOptions) *CheckResult {
		cancel()
		return &CheckResult{Model: "claude-test", Status: MonitorStatusOperational, LatencyMs: &latency, HTTPStatus: &status, CheckedAt: time.Now()}
	}
	records, err := svc.RunCheck(ctx, 7)
	require.NoError(t, err)
	require.True(t, repo.manual)
	require.False(t, repo.target.Enabled)
	require.Nil(t, repo.completeContextError)
	require.Len(t, records, 1)
	require.Equal(t, MonitorStatusDegraded, records[0].Status)
	require.Nil(t, records[0].Cost)
	require.Equal(t, "unknown", records[0].CostSource)
}

func TestUpstreamProbeErrorPreservesSafeDetailsAndRedactsKey(t *testing.T) {
	status := 401
	require.Equal(t, "upstream HTTP 401: API key [redacted] was rejected", upstreamCheckMessage(&CheckResult{HTTPStatus: &status, Status: MonitorStatusError, Message: "upstream HTTP 401: API key secret-value was rejected"}, "secret-value"))
	require.NotContains(t, upstreamCheckMessage(&CheckResult{Status: MonitorStatusError, Message: "echo secret-value"}, "secret-value"), "secret-value")
	require.Equal(t, "challenge mismatch: expected 42, got 41", upstreamCheckMessage(&CheckResult{Status: MonitorStatusFailed, Message: "challenge mismatch: expected 42, got 41"}, "key"))
	require.Equal(t, "upstream returned HTTP 401", upstreamCheckMessage(&CheckResult{HTTPStatus: &status, Status: MonitorStatusError, Message: " \n\t"}, "key"))
	require.Equal(t, "upstream response did not pass the content check", upstreamCheckMessage(&CheckResult{Status: MonitorStatusFailed}, "key"))
}

func TestUpstreamProbeErrorSanitizesOtherCredentialShapesAndTruncatesDetails(t *testing.T) {
	status := 429
	message := "upstream HTTP 429: quota exhausted; Authorization: Bearer sk-other-token-value-123456; https://upstream.example?api_key=query-secret; key=actual-secret " + strings.Repeat("details ", 500)
	safe := upstreamCheckMessage(&CheckResult{HTTPStatus: &status, Status: MonitorStatusError, Message: message}, "actual-secret")
	require.Contains(t, safe, "quota exhausted")
	require.NotContains(t, safe, "actual-secret")
	require.NotContains(t, safe, "other-token-value-123456")
	require.NotContains(t, safe, "query-secret")
	require.LessOrEqual(t, len(safe), monitorMessageMaxBytes)
}

func TestUpstreamProbeHistoryPersistsSpecificSafeFailure(t *testing.T) {
	repo := &upstreamTestRepo{target: validUpstreamTestTarget()}
	svc := NewUpstreamCenterService(repo, upstreamTestEncryptor{}, nil, nil)
	svc.checkModel = func(_ context.Context, _ string, _ string, key string, model string, _ *CheckOptions) *CheckResult {
		status := 429
		return &CheckResult{Model: model, Status: MonitorStatusError, HTTPStatus: &status, Message: "upstream HTTP 429: daily group quota exhausted for " + key, CheckedAt: time.Now()}
	}
	records, err := svc.RunCheck(context.Background(), 7)
	require.NoError(t, err)
	require.Len(t, records, 1)
	require.Equal(t, "upstream HTTP 429: daily group quota exhausted for [redacted]", records[0].Message)
	require.Equal(t, records[0].Message, repo.records[0].Message)
}

func TestUpstreamProbeUsageParsing(t *testing.T) {
	cases := []struct {
		name, provider, body string
		want                 *UsageTokens
	}{
		{"chat cache", "openai", `{"usage":{"prompt_tokens":100,"completion_tokens":7,"prompt_tokens_details":{"cached_tokens":20}}}`, &UsageTokens{InputTokens: 80, OutputTokens: 7, CacheReadTokens: 20}},
		{"responses", "openai", `{"usage":{"input_tokens":42,"output_tokens":9}}`, &UsageTokens{InputTokens: 42, OutputTokens: 9}},
		{"anthropic", "anthropic", `{"usage":{"input_tokens":10,"output_tokens":9,"cache_read_input_tokens":50,"cache_creation_input_tokens":20,"cache_creation":{"ephemeral_5m_input_tokens":5,"ephemeral_1h_input_tokens":15}}}`, &UsageTokens{InputTokens: 10, OutputTokens: 9, CacheReadTokens: 50, CacheCreationTokens: 20, CacheCreation5mTokens: 5, CacheCreation1hTokens: 15}},
		{"gemini thinking", "gemini", `{"usageMetadata":{"promptTokenCount":20,"candidatesTokenCount":4,"cachedContentTokenCount":5,"thoughtsTokenCount":3}}`, &UsageTokens{InputTokens: 15, OutputTokens: 7, CacheReadTokens: 5}},
		{"missing", "openai", `{}`, nil},
		{"partial", "openai", `{"usage":{"prompt_tokens":10}}`, nil},
		{"negative", "openai", `{"usage":{"prompt_tokens":10,"completion_tokens":-1}}`, nil},
		{"invalid cache", "openai", `{"usage":{"prompt_tokens":10,"completion_tokens":1,"prompt_tokens_details":{"cached_tokens":11}}}`, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) { require.Equal(t, tc.want, parseUpstreamMonitorUsage(tc.provider, tc.body)) })
	}
}

func TestUpstreamHTTPClientNeverForwardsCredentialsOnRedirect(t *testing.T) {
	received := false
	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { received = true; w.WriteHeader(http.StatusOK) }))
	defer destination.Close()
	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, destination.URL, http.StatusTemporaryRedirect)
	}))
	defer source.Close()
	client := newSSRFSafeHTTPClient(time.Second)
	// Test the redirect policy with loopback transports; production retains the
	// SSRF-aware dialer and refuses either httptest address.
	client.Transport = http.DefaultTransport
	req, err := http.NewRequest(http.MethodGet, source.URL, nil)
	require.NoError(t, err)
	req.Header.Set("x-api-key", "private-test-key")
	response, err := client.Do(req)
	require.NoError(t, err)
	defer func() { _ = response.Body.Close() }()
	require.Equal(t, http.StatusTemporaryRedirect, response.StatusCode)
	require.False(t, received)
}

func TestUpstreamSharedWalletKeepsNewestAmountWithoutUnrelatedFailure(t *testing.T) {
	old, newer, attempt := time.Now().Add(-time.Hour), time.Now().Add(-time.Minute), time.Now()
	oldAmount, newAmount := 20.0, 19.0
	result := mergeUpstreamWalletBalances(&UpstreamBalanceSnapshot{Status: "ok", SyncedAt: &newer, LastAttemptAt: &newer, Balance: &newAmount}, &UpstreamBalanceSnapshot{Status: "error", Error: "HTTP 503", SyncedAt: &old, LastAttemptAt: &attempt, Balance: &oldAmount})
	require.Equal(t, 19.0, *result.Balance)
	require.Equal(t, newer, *result.SyncedAt)
	require.Equal(t, newer, *result.LastAttemptAt)
	require.Equal(t, "ok", result.Status)
	require.Empty(t, result.Error)
}

func TestUpstreamStopCancelsAndWaitsForManualProbe(t *testing.T) {
	repo := &upstreamTestRepo{target: validUpstreamTestTarget()}
	svc := NewUpstreamCenterService(repo, upstreamTestEncryptor{}, nil, nil)
	entered := make(chan struct{})
	finished := make(chan struct{})
	svc.checkModel = func(ctx context.Context, _ string, _ string, _ string, model string, _ *CheckOptions) *CheckResult {
		close(entered)
		<-ctx.Done()
		return &CheckResult{Model: model, Status: MonitorStatusError, CheckedAt: time.Now()}
	}
	go func() { defer close(finished); _, _ = svc.RunCheck(context.Background(), 7) }()
	<-entered
	svc.Stop()
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("manual probe outlived Stop")
	}
	require.Len(t, repo.records, 1)
	_, err := svc.RunCheck(context.Background(), 7)
	require.ErrorIs(t, err, context.Canceled)
}

type upstreamModelsTransport func(*http.Request) (*http.Response, error)

func (f upstreamModelsTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestUpstreamModelDiscoveryImportsAccountReadOnly(t *testing.T) {
	account := &Account{ID: 9, Type: AccountTypeAPIKey, Platform: PlatformOpenAI, Status: StatusActive, Schedulable: true, Credentials: map[string]any{"base_url": "https://8.8.8.8/relay/v1", "api_key": "private-key"}}
	svc := NewUpstreamCenterService(nil, upstreamTestEncryptor{}, upstreamTestAccounts{account: account}, nil)
	svc.modelsClient = &http.Client{Transport: upstreamModelsTransport(func(r *http.Request) (*http.Response, error) {
		require.Equal(t, "https://8.8.8.8/relay/v1/models", r.URL.String())
		require.Equal(t, "Bearer private-key", r.Header.Get("Authorization"))
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"data":[{"id":"model-b"},{"id":"model-a"},{"id":"model-a"}]}`)), Header: make(http.Header)}, nil
	})}
	id := int64(9)
	models, err := svc.Models(context.Background(), UpstreamModelsInput{AccountID: &id})
	require.NoError(t, err)
	require.Equal(t, []string{"model-a", "model-b"}, models)
	require.Nil(t, account.Extra)
	_, err = svc.Models(context.Background(), UpstreamModelsInput{AccountID: &id, Endpoint: "https://1.1.1.1"})
	require.ErrorIs(t, err, ErrUpstreamInvalid)
}

func TestUpstreamProbeTransportAllowsConfigured45SecondDeadline(t *testing.T) {
	legacyTransport := newMonitorHTTPTransport(monitorResponseHeaderTimeout)
	upstreamTransport := newMonitorHTTPTransport(upstreamProbeMaxTimeout)
	require.Equal(t, 30*time.Second, legacyTransport.ResponseHeaderTimeout)
	require.Equal(t, 45*time.Second, upstreamTransport.ResponseHeaderTimeout)
	require.NotNil(t, upstreamTransport.DialContext)
	client := newUpstreamProbeHTTPClient()
	require.Equal(t, 45*time.Second, client.Timeout)
	require.ErrorIs(t, client.CheckRedirect(nil, nil), http.ErrUseLastResponse)

	repo := &upstreamTestRepo{target: validUpstreamTestTarget()}
	svc := NewUpstreamCenterService(repo, upstreamTestEncryptor{}, nil, nil)
	svc.checkModel = func(ctx context.Context, _, _, _, model string, opts *CheckOptions) *CheckResult {
		require.Same(t, svc.probeClient, opts.HTTPClient)
		deadline, ok := ctx.Deadline()
		require.True(t, ok)
		require.InDelta(t, 45, time.Until(deadline).Seconds(), 1)
		return &CheckResult{Model: model, Status: MonitorStatusOperational, CheckedAt: time.Now()}
	}
	_, err := svc.RunCheck(context.Background(), 7)
	require.NoError(t, err)
}

func TestUpstreamProbeClientOverrideAndTargetTimeout(t *testing.T) {
	// Scale deadlines down to exercise the real net/http header timeout without
	// introducing a 30-second test. The legacy call must still use its own client.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(100 * time.Millisecond):
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"choices":[{"message":{"content":"ready"}}]}`)
		case <-r.Context().Done():
		}
	}))
	defer server.Close()
	legacyClient := monitorHTTPClient
	monitorHTTPClient = &http.Client{Timeout: time.Second, Transport: &http.Transport{ResponseHeaderTimeout: 20 * time.Millisecond}}
	t.Cleanup(func() { monitorHTTPClient.CloseIdleConnections(); monitorHTTPClient = legacyClient })
	upstreamClient := &http.Client{Timeout: time.Second, Transport: &http.Transport{ResponseHeaderTimeout: 500 * time.Millisecond}}
	defer upstreamClient.CloseIdleConnections()

	_, _, _, err := callProvider(context.Background(), MonitorProviderOpenAI, server.URL, "key", "model", "test", nil)
	require.Error(t, err)
	text, _, status, err := callProvider(context.Background(), MonitorProviderOpenAI, server.URL, "key", "model", "test", &CheckOptions{HTTPClient: upstreamClient})
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, status)
	require.Equal(t, "ready", text)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, _, _, err = callProvider(ctx, MonitorProviderOpenAI, server.URL, "key", "model", "test", &CheckOptions{HTTPClient: upstreamClient})
	require.ErrorIs(t, err, context.DeadlineExceeded)
}
