package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
	"github.com/google/uuid"
)

type UpstreamCenterService struct {
	repo             UpstreamCenterRepository
	encryptor        SecretEncryptor
	accounts         AccountRepository
	finance          *UpstreamFinanceService
	storageKeys      *APIKeyService
	storageCleanupMu sync.Mutex
	ctx              context.Context
	cancel           context.CancelFunc
	startOnce        sync.Once
	stopOnce         sync.Once
	wg               sync.WaitGroup
	lifecycleMu      sync.Mutex
	stopped          bool
	manualChecks     sync.WaitGroup
	slots            chan struct{}
	modelsClient     *http.Client
	probeClient      *http.Client
	checkModel       func(context.Context, string, string, string, string, *CheckOptions) *CheckResult
}

func NewUpstreamCenterService(repo UpstreamCenterRepository, encryptor SecretEncryptor, accountRepo AccountRepository, financeSvc *UpstreamFinanceService) *UpstreamCenterService {
	ctx, cancel := context.WithCancel(context.Background())
	return &UpstreamCenterService{repo: repo, encryptor: encryptor, accounts: accountRepo, finance: financeSvc, ctx: ctx, cancel: cancel, slots: make(chan struct{}, 5), modelsClient: newSSRFSafeHTTPClient(15 * time.Second), probeClient: newUpstreamProbeHTTPClient(), checkModel: runCheckForModel}
}

const upstreamProbeMaxTimeout = 45 * time.Second

func newUpstreamProbeHTTPClient() *http.Client {
	// The target context sets the actual 5-45s budget. The shared upstream
	// transport must not impose the legacy channel monitor's 30s header cap.
	return newSSRFSafeHTTPClientWithHeaderTimeout(upstreamProbeMaxTimeout, upstreamProbeMaxTimeout)
}

func (s *UpstreamCenterService) Overview(ctx context.Context, window string) (*UpstreamOverview, error) {
	duration := 24 * time.Hour
	switch window {
	case "", "24h":
	case "7d":
		duration = 7 * 24 * time.Hour
	case "30d":
		duration = 30 * 24 * time.Hour
	default:
		return nil, ErrUpstreamInvalid
	}
	suppliers, err := s.repo.ListSuppliers(ctx)
	if err != nil {
		return nil, err
	}
	targets, err := s.repo.ListTargets(ctx)
	if err != nil {
		return nil, err
	}
	if err = s.repo.PopulateStatistics(ctx, targets, time.Now().Add(-duration)); err != nil {
		return nil, err
	}
	from, to := timezone.Today(), timezone.Today().AddDate(0, 0, 1)
	out := &UpstreamOverview{Suppliers: suppliers, Monitors: []*UpstreamTarget{}}
	byID := map[int64]*UpstreamSupplier{}
	for _, v := range suppliers {
		v.Targets = []*UpstreamTarget{}
		v.Wallets = []*UpstreamBalanceSnapshot{}
		byID[v.ID] = v
		if s.finance != nil {
			v.Finance, err = s.finance.Summary(ctx, &v.ID, nil, from, to)
			if err != nil {
				return nil, err
			}
		}
	}
	for _, t := range targets {
		s.maskTarget(t)
		if s.finance != nil {
			t.Finance, err = s.finance.Summary(ctx, t.SupplierID, &t.ID, from, to)
			if err != nil {
				return nil, err
			}
			t.Balance, err = s.finance.LatestBalance(ctx, t.ID)
			if err != nil {
				return nil, err
			}
		}
		if t.SupplierID == nil {
			out.Monitors = append(out.Monitors, t)
			continue
		}
		supplier := byID[*t.SupplierID]
		if supplier == nil {
			continue
		}
		supplier.Targets = append(supplier.Targets, t)
	}
	for _, supplier := range suppliers {
		supplier.Wallets = upstreamSupplierWallets(supplier.Targets)
	}
	if s.finance != nil {
		out.Summary, err = s.finance.Summary(ctx, nil, nil, from, to)
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (s *UpstreamCenterService) SaveSupplier(ctx context.Context, id int64, name, website, notes *string) (*UpstreamSupplier, error) {
	v := &UpstreamSupplier{Targets: []*UpstreamTarget{}, Wallets: []*UpstreamBalanceSnapshot{}}
	if id > 0 {
		var err error
		v, err = s.repo.GetSupplier(ctx, id)
		if err != nil {
			return nil, err
		}
	}
	if name != nil {
		v.Name = strings.TrimSpace(*name)
	}
	if website != nil {
		v.Website = strings.TrimSpace(*website)
	}
	if notes != nil {
		v.Notes = strings.TrimSpace(*notes)
	}
	if v.Name == "" || utf8.RuneCountInString(v.Name) > 100 || len(v.Notes) > 4000 || len(v.Website) > 500 {
		return nil, ErrUpstreamInvalid
	}
	if v.Website != "" {
		u, err := url.Parse(v.Website)
		if err != nil || u.Host == "" || (u.Scheme != "https" && u.Scheme != "http") || u.User != nil {
			return nil, ErrUpstreamInvalid
		}
	}
	if err := s.repo.SaveSupplier(ctx, v); err != nil {
		return nil, err
	}
	return v, nil
}
func (s *UpstreamCenterService) ArchiveSupplier(ctx context.Context, id int64) error {
	return s.repo.ArchiveSupplier(ctx, id)
}
func (s *UpstreamCenterService) ArchiveTarget(ctx context.Context, id int64) error {
	return s.repo.ArchiveTarget(ctx, id)
}

func (s *UpstreamCenterService) SaveTarget(ctx context.Context, id int64, in UpstreamTargetInput) (*UpstreamTarget, error) {
	t := &UpstreamTarget{Provider: MonitorProviderOpenAI, APIMode: MonitorAPIModeChatCompletions, Enabled: true, IntervalSeconds: 30, TimeoutSeconds: 45, DegradedThresholdMs: 6000, WalletRef: "default", Models: []string{"gpt-5.6-sol"}, AccountIDs: []int64{}}
	var oldSupplier *int64
	oldProvider, oldEndpoint, oldEncrypted := "", "", ""
	var oldNewAPIUserID int64
	oldNewAPITokenEncrypted := ""
	if id > 0 {
		var err error
		t, err = s.repo.GetTarget(ctx, id)
		if err != nil {
			return nil, err
		}
		oldSupplier = t.SupplierID
		oldProvider = t.Provider
		oldEndpoint = normalizeEndpoint(t.Endpoint)
		oldEncrypted = t.APIKeyEncrypted
		oldNewAPIUserID = t.NewAPIUserID
		oldNewAPITokenEncrypted = t.NewAPIAccessTokenEncrypted
	}
	if len(in.SupplierID) > 0 {
		if err := json.Unmarshal(in.SupplierID, &t.SupplierID); err != nil {
			return nil, ErrUpstreamInvalid
		}
	}
	if t.SupplierID != nil && *t.SupplierID <= 0 {
		return nil, ErrUpstreamInvalid
	}
	applyUpstreamString(&t.Name, in.Name)
	applyUpstreamString(&t.Provider, in.Provider)
	applyUpstreamString(&t.APIMode, in.APIMode)
	applyUpstreamString(&t.Endpoint, in.Endpoint)
	applyUpstreamString(&t.WalletRef, in.WalletRef)
	applyUpstreamString(&t.Notes, in.Notes)
	if in.Enabled != nil {
		t.Enabled = *in.Enabled
	}
	if in.IntervalSeconds != nil {
		t.IntervalSeconds = *in.IntervalSeconds
	}
	if in.TimeoutSeconds != nil {
		t.TimeoutSeconds = *in.TimeoutSeconds
	}
	if in.DegradedThresholdMs != nil {
		t.DegradedThresholdMs = *in.DegradedThresholdMs
	}
	if in.Models != nil {
		t.Models = normalizeModels(*in.Models)
	}
	if in.AccountIDs != nil {
		t.AccountIDs = append([]int64{}, (*in.AccountIDs)...)
	}
	sort.Slice(t.AccountIDs, func(i, j int) bool { return t.AccountIDs[i] < t.AccountIDs[j] })
	for i, aid := range t.AccountIDs {
		if aid <= 0 || (i > 0 && aid == t.AccountIDs[i-1]) {
			return nil, ErrUpstreamInvalid
		}
	}
	if t.SupplierID == nil && len(t.AccountIDs) > 0 {
		return nil, ErrUpstreamInvalid.WithMetadata(map[string]string{"field": "account_ids", "detail": "independent monitors cannot bind business accounts"})
	}
	plain := ""
	if in.APIKey != nil {
		plain = strings.TrimSpace(*in.APIKey)
	}
	if in.SourceAccountID != nil {
		if t.SupplierID != nil || len(t.AccountIDs) > 0 || plain != "" {
			return nil, ErrUpstreamInvalid.WithMetadata(map[string]string{"field": "source_account_id", "detail": "use a source account only for an independent monitor without an explicit API key"})
		}
		provider, endpoint, key, err := s.sourceAccountCredentials(ctx, *in.SourceAccountID, "source_account_id")
		if err != nil {
			return nil, err
		}
		if (in.Endpoint != nil && strings.TrimSpace(*in.Endpoint) != "" && normalizeEndpoint(*in.Endpoint) != endpoint) ||
			(in.Provider != nil && strings.TrimSpace(*in.Provider) != "" && strings.TrimSpace(*in.Provider) != provider) {
			return nil, ErrUpstreamInvalid.WithMetadata(map[string]string{"field": "source_account_id", "detail": "source account credentials must use that account's endpoint and provider"})
		}
		if id > 0 && in.APIMode == nil && oldProvider != provider {
			t.APIMode = MonitorAPIModeChatCompletions
		}
		t.Provider, t.Endpoint, plain = provider, endpoint, key
	}
	// Never carry the saved key to a different credential recipient. An explicit
	// key, validated source import or exact linked-account credentials must supply
	// the replacement; clearing a pending import must not revive the old key.
	credentialRecipientChanged := id > 0 && (normalizeEndpoint(t.Endpoint) != oldEndpoint || t.Provider != oldProvider)
	if credentialRecipientChanged && plain == "" && len(t.AccountIDs) == 0 {
		return nil, ErrUpstreamInvalid.WithMetadata(map[string]string{"field": "api_key", "detail": "enter an API key or import an account before changing the endpoint or provider"})
	}
	if plain == "" && oldEncrypted != "" && !credentialRecipientChanged {
		var err error
		plain, err = s.encryptor.Decrypt(oldEncrypted)
		if err != nil {
			return nil, ErrChannelMonitorAPIKeyDecryptFailed
		}
	}
	t.BindingCredentials = map[int64]UpstreamBindingCredential{}
	for _, aid := range t.AccountIDs {
		if s.accounts == nil {
			return nil, ErrUpstreamInvalid
		}
		a, err := s.accounts.GetByID(ctx, aid)
		if err != nil {
			return nil, err
		}
		if a == nil || a.Type != AccountTypeAPIKey {
			return nil, ErrUpstreamInvalid.WithMetadata(map[string]string{"field": "account_ids", "detail": "only API key accounts may be linked"})
		}
		accountKey := strings.TrimSpace(a.GetCredential("api_key"))
		accountEndpoint := normalizeEndpoint(a.GetCredential("base_url"))
		if accountEndpoint == "" {
			switch a.Platform {
			case PlatformOpenAI:
				accountEndpoint = "https://api.openai.com"
			case PlatformGemini:
				accountEndpoint = "https://generativelanguage.googleapis.com"
			default:
				accountEndpoint = "https://api.anthropic.com"
			}
		}
		if t.Endpoint == "" {
			t.Endpoint = accountEndpoint
		}
		if plain == "" {
			plain = accountKey
		}
		if plain != accountKey || normalizeEndpoint(t.Endpoint) != accountEndpoint || t.Provider != a.Platform {
			return nil, ErrUpstreamInvalid.WithMetadata(map[string]string{"field": "account_ids", "detail": "all linked accounts must use exactly this endpoint, provider and API key"})
		}
		t.BindingCredentials[aid] = UpstreamBindingCredential{APIKey: a.GetCredential("api_key"), BaseURL: a.GetCredential("base_url")}
	}
	if err := validateUpstreamTargetConfig(t, plain, id == 0 || normalizeEndpoint(t.Endpoint) != oldEndpoint); err != nil {
		return nil, err
	}
	t.Endpoint = normalizeEndpoint(t.Endpoint)
	if err := s.applyNewAPICredentials(t, in, oldNewAPIUserID, oldNewAPITokenEncrypted,
		id > 0 && (t.Endpoint != oldEndpoint || t.Provider != oldProvider)); err != nil {
		return nil, err
	}
	if t.SupplierID != nil {
		if _, err := s.repo.GetSupplier(ctx, *t.SupplierID); err != nil {
			return nil, err
		}
	}
	oldPlain := ""
	if oldEncrypted != "" {
		oldPlain, _ = s.encryptor.Decrypt(oldEncrypted)
	}
	if plain == oldPlain && oldEncrypted != "" {
		t.APIKeyEncrypted = oldEncrypted
	} else {
		encrypted, err := s.encryptor.Encrypt(plain)
		if err != nil {
			return nil, fmt.Errorf("encrypt upstream credentials: %w", err)
		}
		t.APIKeyEncrypted = encrypted
	}
	fingerprint := sha256.Sum256([]byte(t.Endpoint + "\x00" + plain))
	t.APIKeyFingerprint = hex.EncodeToString(fingerprint[:])
	t.ResetBindings = id > 0 && (!sameUpstreamSupplier(oldSupplier, t.SupplierID) || oldEndpoint != t.Endpoint || oldPlain != plain)
	if err := s.repo.SaveTarget(ctx, t); err != nil {
		return nil, err
	}
	s.maskTarget(t)
	t.Statistics = []*UpstreamModelStatistics{}
	for _, m := range t.Models {
		t.Statistics = append(t.Statistics, &UpstreamModelStatistics{Model: m, Status: "unknown", Timeline: []*UpstreamHistoryRecord{}})
	}
	return t, nil
}

func (s *UpstreamCenterService) applyNewAPICredentials(t *UpstreamTarget, in UpstreamTargetInput, oldUserID int64, oldEncrypted string, recipientChanged bool) error {
	invalid := func(detail string) error {
		return ErrUpstreamInvalid.WithMetadata(map[string]string{"field": "newapi_access_token", "detail": detail})
	}
	if in.NewAPIUserID != nil {
		if *in.NewAPIUserID < 0 {
			return ErrUpstreamInvalid.WithMetadata(map[string]string{"field": "newapi_user_id", "detail": "New API user ID must be positive, or zero to clear the console credentials"})
		}
		t.NewAPIUserID = *in.NewAPIUserID
		if t.NewAPIUserID == 0 {
			t.NewAPIAccessTokenEncrypted = ""
			return nil
		}
	}
	plain := ""
	if in.NewAPIAccessToken != nil {
		// Validate line breaks before trimming: tokens are used in an HTTP header.
		if len(*in.NewAPIAccessToken) > 4096 || strings.ContainsAny(*in.NewAPIAccessToken, "\r\n") {
			return invalid("New API access token must not exceed 4096 bytes or contain line breaks")
		}
		plain = strings.TrimSpace(*in.NewAPIAccessToken)
	}
	if plain == "" {
		if oldEncrypted != "" && (recipientChanged || t.NewAPIUserID != oldUserID) {
			return invalid("enter a new New API access token or clear the console credentials before changing the endpoint, provider or user ID")
		}
		t.NewAPIAccessTokenEncrypted = oldEncrypted
	} else {
		if t.NewAPIUserID <= 0 {
			return invalid("a positive New API user ID is required with the access token")
		}
		// Preserve stable ciphertext for unchanged credentials so cosmetic edits
		// do not invalidate snapshots or schedule a redundant balance refresh.
		previous := ""
		if oldEncrypted != "" {
			previous, _ = s.encryptor.Decrypt(oldEncrypted)
		}
		if oldEncrypted != "" && previous == plain {
			t.NewAPIAccessTokenEncrypted = oldEncrypted
		} else {
			encrypted, err := s.encryptor.Encrypt(plain)
			if err != nil {
				return fmt.Errorf("encrypt upstream console credentials: %w", err)
			}
			t.NewAPIAccessTokenEncrypted = encrypted
		}
	}
	if (t.NewAPIUserID > 0) != (t.NewAPIAccessTokenEncrypted != "") {
		return invalid("provide both a New API user ID and access token, or clear the console credentials")
	}
	return nil
}

func sameUpstreamSupplier(a, b *int64) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}
func applyUpstreamString(dst *string, src *string) {
	if src != nil {
		*dst = strings.TrimSpace(*src)
	}
}
func validateUpstreamTargetConfig(t *UpstreamTarget, key string, validateNetwork bool) error {
	t.APIMode = defaultAPIMode(t.APIMode)
	if t.Name == "" || utf8.RuneCountInString(t.Name) > 100 || len(t.Endpoint) > 500 || len(t.Notes) > 4000 || len(t.WalletRef) > 100 || len(t.AccountIDs) > 100 {
		return ErrUpstreamInvalid
	}
	if t.WalletRef == "" {
		t.WalletRef = "default"
	}
	if t.Provider != MonitorProviderOpenAI && t.Provider != MonitorProviderAnthropic && t.Provider != MonitorProviderGemini {
		return ErrChannelMonitorInvalidProvider
	}
	if err := validateAPIMode(t.Provider, t.APIMode); err != nil {
		return err
	}
	if t.IntervalSeconds < 30 || t.IntervalSeconds > 3600 || t.TimeoutSeconds < 5 || t.TimeoutSeconds > 45 || t.DegradedThresholdMs < 100 || t.DegradedThresholdMs > 45000 {
		return ErrUpstreamInvalid
	}
	if len(t.Models) < 1 || len(t.Models) > 8 {
		return ErrUpstreamInvalid.WithMetadata(map[string]string{"field": "models", "detail": "choose between one and eight models"})
	}
	for _, m := range t.Models {
		if len(m) > 200 || strings.ContainsAny(m, "?#\r\n\t ") || strings.Contains(m, "..") {
			return ErrUpstreamInvalid
		}
	}
	if key == "" || len(key) > 2000 || strings.ContainsAny(key, "\r\n") {
		return ErrChannelMonitorMissingAPIKey
	}
	if u, err := url.Parse(t.Endpoint); err != nil || u.User != nil {
		return ErrChannelMonitorInvalidEndpoint
	}
	// An existing target must remain pausable during an upstream DNS outage.
	// Unchanged endpoints were validated on creation and every actual HTTP dial
	// still enforces the SSRF policy.
	if validateNetwork {
		return validateEndpoint(t.Endpoint)
	}
	return nil
}
func (s *UpstreamCenterService) maskTarget(t *UpstreamTarget) {
	t.APIKeyMasked = "***"
	t.NewAPIAccessTokenConfigured = t.NewAPIUserID > 0 && t.NewAPIAccessTokenEncrypted != ""
	plain, err := s.encryptor.Decrypt(t.APIKeyEncrypted)
	if err == nil && len(plain) > 8 {
		t.APIKeyMasked = plain[:4] + "••••" + plain[len(plain)-4:]
	}
	// These fields are never serialized, but release credential copies as soon
	// as the persistence boundary no longer needs them.
	t.BindingCredentials = nil
}

func (s *UpstreamCenterService) History(ctx context.Context, q UpstreamHistoryQuery) (*UpstreamHistoryPage, error) {
	if _, err := s.repo.GetTarget(ctx, q.TargetID); err != nil {
		return nil, err
	}
	if q.Page < 1 {
		q.Page = 1
	}
	if q.PageSize < 1 {
		q.PageSize = 50
	}
	if q.PageSize > 200 {
		q.PageSize = 200
	}
	if q.From != nil && q.To != nil && !q.From.Before(*q.To) {
		return nil, ErrUpstreamInvalid
	}
	return s.repo.History(ctx, q)
}

func (s *UpstreamCenterService) RunCheck(ctx context.Context, id int64) ([]*UpstreamHistoryRecord, error) {
	s.lifecycleMu.Lock()
	if s.stopped {
		s.lifecycleMu.Unlock()
		return nil, context.Canceled
	}
	s.manualChecks.Add(1)
	s.lifecycleMu.Unlock()
	defer s.manualChecks.Done()
	runCtx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(s.ctx, cancel)
	defer stop()
	defer cancel()
	return s.runCheck(runCtx, id, true)
}
func (s *UpstreamCenterService) runCheck(ctx context.Context, id int64, manual bool) ([]*UpstreamHistoryRecord, error) {
	select {
	case s.slots <- struct{}{}:
		defer func() { <-s.slots }()
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	token := uuid.NewString()
	claimed, err := s.repo.ClaimCheck(ctx, id, token, manual)
	if err != nil {
		return nil, err
	}
	if !claimed {
		if _, err = s.repo.GetTarget(ctx, id); err != nil {
			return nil, err
		}
		return nil, ErrUpstreamBusy
	}
	defer func() {
		releaseCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if e := s.repo.ReleaseCheck(releaseCtx, id, token); e != nil {
			slog.Warn("upstream center lease release failed", "target_id", id, "error", e)
		}
	}()
	t, err := s.repo.GetTarget(ctx, id)
	if err != nil {
		return nil, err
	}
	key, err := s.encryptor.Decrypt(t.APIKeyEncrypted)
	entries := make([]*UpstreamHistoryRecord, 0, len(t.Models))
	for _, model := range t.Models {
		var result *CheckResult
		if err != nil || key == "" {
			result = &CheckResult{Model: model, Status: MonitorStatusError, Message: "API key cannot be decrypted; edit this target and enter the key again", CheckedAt: time.Now()}
		} else {
			probeCtx, cancel := context.WithTimeout(ctx, time.Duration(t.TimeoutSeconds)*time.Second)
			result = s.checkModel(probeCtx, t.Provider, t.Endpoint, key, model, &CheckOptions{APIMode: t.APIMode, HTTPClient: s.probeClient})
			cancel()
			if result.Status == MonitorStatusOperational || result.Status == MonitorStatusDegraded {
				result.Status = MonitorStatusOperational
				result.Message = ""
				if result.LatencyMs != nil && *result.LatencyMs >= t.DegradedThresholdMs {
					result.Status = MonitorStatusDegraded
					result.Message = "response exceeded the configured latency threshold"
				}
			}
		}
		h := &UpstreamHistoryRecord{TargetID: id, Model: model, Status: result.Status, LatencyMs: result.LatencyMs, PingLatencyMs: result.PingLatencyMs, HTTPStatus: result.HTTPStatus, Message: upstreamCheckMessage(result, key), CheckedAt: result.CheckedAt, CostSource: "unknown"}
		if s.finance != nil && result.Usage != nil {
			cost, costErr := s.finance.EstimateMonitorCost(ctx, id, model, *result.Usage)
			if costErr == nil && cost != nil {
				h.Cost = cost
				h.CostSource = "estimated"
			}
		}
		entries = append(entries, h)
		if ctx.Err() != nil {
			break
		}
	}
	// A disconnected admin client must not discard a paid probe's audit record.
	saveCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	saved, saveErr := s.repo.CompleteCheck(saveCtx, id, token, entries)
	if saveErr != nil {
		return nil, saveErr
	}
	if !saved {
		return nil, ErrUpstreamBusy
	}
	return entries, nil
}

func upstreamCheckMessage(r *CheckResult, key string) string {
	msg := strings.TrimSpace(r.Message)
	if key != "" {
		msg = strings.ReplaceAll(msg, key, "[redacted]")
	}
	msg = truncateMessage(sanitizeErrorMessage(msg))
	if msg != "" {
		return msg
	}
	if r.HTTPStatus != nil && (*r.HTTPStatus < 200 || *r.HTTPStatus >= 300) {
		return fmt.Sprintf("upstream returned HTTP %d", *r.HTTPStatus)
	}
	if r.Status == MonitorStatusFailed {
		return "upstream response did not pass the content check"
	}
	return ""
}

func (s *UpstreamCenterService) Models(ctx context.Context, in UpstreamModelsInput) ([]string, error) {
	provider, endpoint, key := strings.TrimSpace(in.Provider), normalizeEndpoint(in.Endpoint), strings.TrimSpace(in.APIKey)
	if in.AccountID != nil {
		if key != "" {
			return nil, ErrUpstreamInvalid.WithMetadata(map[string]string{"field": "account_id", "detail": "choose either an account or an explicit API key"})
		}
		accountProvider, accountEndpoint, accountKey, err := s.sourceAccountCredentials(ctx, *in.AccountID, "account_id")
		if err != nil {
			return nil, err
		}
		if endpoint == "" {
			endpoint = accountEndpoint
		} else if endpoint != accountEndpoint {
			return nil, ErrUpstreamInvalid.WithMetadata(map[string]string{"field": "endpoint", "detail": "account credentials can only be used with that account's endpoint"})
		}
		if provider == "" {
			provider = accountProvider
		} else if provider != accountProvider {
			return nil, ErrUpstreamInvalid.WithMetadata(map[string]string{"field": "provider", "detail": "account credentials can only be used with that account's provider"})
		}
		key = accountKey
	}
	if in.TargetID != nil && in.AccountID == nil {
		t, err := s.repo.GetTarget(ctx, *in.TargetID)
		if err != nil {
			return nil, err
		}
		if provider == "" {
			provider = t.Provider
		}
		if endpoint == "" {
			endpoint = t.Endpoint
		}
		if key == "" {
			if endpoint != normalizeEndpoint(t.Endpoint) || provider != t.Provider {
				return nil, ErrUpstreamInvalid.WithMetadata(map[string]string{"field": "api_key", "detail": "enter a key before fetching models from a changed endpoint or provider"})
			}
			key, err = s.encryptor.Decrypt(t.APIKeyEncrypted)
			if err != nil {
				return nil, ErrChannelMonitorAPIKeyDecryptFailed
			}
		}
	}
	if key == "" || len(key) > 2000 || strings.ContainsAny(key, "\r\n") {
		return nil, ErrChannelMonitorMissingAPIKey
	}
	if err := validateEndpoint(endpoint); err != nil {
		return nil, err
	}
	u, err := url.Parse(endpoint)
	if err != nil || u.User != nil {
		return nil, ErrChannelMonitorInvalidEndpoint
	}
	path := "/v1/models"
	if provider == MonitorProviderGemini {
		path = "/v1beta/models"
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, joinURL(endpoint, path), nil)
	if err != nil {
		return nil, ErrUpstreamInvalid
	}
	switch provider {
	case MonitorProviderOpenAI:
		req.Header.Set("Authorization", "Bearer "+key)
	case MonitorProviderAnthropic:
		req.Header.Set("x-api-key", key)
		req.Header.Set("anthropic-version", monitorAnthropicAPIVersion)
	case MonitorProviderGemini:
		req.Header.Set("x-goog-api-key", key)
	default:
		return nil, ErrChannelMonitorInvalidProvider
	}
	req.Header.Set("Accept", "application/json")
	resp, err := s.modelsClient.Do(req)
	if err != nil {
		return nil, ErrUpstreamInvalid.WithMetadata(map[string]string{"field": "models", "detail": "model discovery failed; models can be entered manually"})
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, ErrUpstreamInvalid.WithMetadata(map[string]string{"field": "models", "detail": fmt.Sprintf("model discovery returned HTTP %d; models can be entered manually", resp.StatusCode)})
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1024*1024+1))
	if err != nil || len(body) > 1024*1024 {
		return nil, ErrUpstreamInvalid
	}
	var payload struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if json.Unmarshal(body, &payload) != nil {
		return nil, ErrUpstreamInvalid
	}
	models := []string{}
	for _, m := range payload.Data {
		if len(m.ID) <= 200 {
			models = append(models, m.ID)
		}
	}
	for _, m := range payload.Models {
		if len(m.Name) <= 207 {
			models = append(models, strings.TrimPrefix(m.Name, "models/"))
		}
	}
	models = normalizeModels(models)
	sort.Strings(models)
	if len(models) > 500 {
		models = models[:500]
	}
	return models, nil
}

// Start owns an independent scheduler. Claims are conditional in PostgreSQL,
// so multiple application instances cannot probe the same target together.
func (s *UpstreamCenterService) Start() {
	s.startOnce.Do(func() {
		s.lifecycleMu.Lock()
		defer s.lifecycleMu.Unlock()
		if s.stopped {
			return
		}
		s.wg.Add(1)
		go s.scheduleLoop()
		if _, ok := s.repo.(UpstreamStorageRepository); ok {
			s.wg.Add(1)
			go s.storageLoop()
		}
		if s.finance != nil {
			s.wg.Add(1)
			go s.balanceLoop()
		}
	})
}
func (s *UpstreamCenterService) Stop() {
	s.stopOnce.Do(func() {
		s.lifecycleMu.Lock()
		s.stopped = true
		s.cancel()
		s.lifecycleMu.Unlock()
		s.wg.Wait()
		s.manualChecks.Wait()
	})
}
func (s *UpstreamCenterService) scheduleLoop() {
	defer s.wg.Done()
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()
	s.dispatchDue()
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			s.dispatchDue()
		}
	}
}

// A dedicated single worker keeps balance I/O from delaying probe scheduling.
// It never overlaps itself and shares the service shutdown cancellation.
func (s *UpstreamCenterService) balanceLoop() {
	defer s.wg.Done()
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	for {
		if s.ctx.Err() != nil {
			return
		}
		s.finance.SyncDueBalances(s.ctx)
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
func (s *UpstreamCenterService) dispatchDue() {
	free := cap(s.slots) - len(s.slots)
	if free <= 0 {
		return
	}
	ctx, cancel := context.WithTimeout(s.ctx, 5*time.Second)
	ids, err := s.repo.DueTargetIDs(ctx, free)
	cancel()
	if err != nil {
		if s.ctx.Err() == nil {
			slog.Warn("upstream center scheduler query failed", "error", err)
		}
		return
	}
	for _, id := range ids {
		s.wg.Add(1)
		go func(targetID int64) {
			defer s.wg.Done()
			ctx, cancel := context.WithTimeout(s.ctx, 7*time.Minute)
			defer cancel()
			_, err := s.runCheck(ctx, targetID, false)
			if err != nil && s.ctx.Err() == nil && err != ErrUpstreamBusy {
				slog.Warn("upstream center probe failed", "target_id", targetID, "error", err)
			}
		}(id)
	}
}
