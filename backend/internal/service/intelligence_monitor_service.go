package service

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/google/uuid"
)

type IntelligenceMonitorService struct {
	repo           IntelligenceMonitorRepository
	encryptor      SecretEncryptor
	upstreams      UpstreamCenterRepository
	groups         GroupRepository
	keys           *APIKeyService
	finance        *UpstreamFinanceService
	accounts       AccountRepository
	oauthForward   intelligenceOAuthForwarder
	oauthSlots     intelligenceOAuthSlots
	cfg            *config.Config
	externalClient *http.Client
	localClient    *http.Client
	localEndpoint  string
	ctx            context.Context
	cancel         context.CancelFunc
	mu             sync.Mutex
	started        bool
	stopped        bool
	wg             sync.WaitGroup
	slots          chan struct{}
	wake           chan struct{}
}

func NewIntelligenceMonitorService(repo IntelligenceMonitorRepository, encryptor SecretEncryptor, upstreamRepo UpstreamCenterRepository, groupRepo GroupRepository, apiKeys *APIKeyService, finance *UpstreamFinanceService, cfg *config.Config) *IntelligenceMonitorService {
	ctx, cancel := context.WithCancel(context.Background())
	port := 8080
	if cfg != nil && cfg.Server.Port > 0 {
		port = cfg.Server.Port
	}
	localEndpoint := "http://127.0.0.1:" + strconv.Itoa(port)
	requestTimeout := time.Duration(IntelligenceMonitorMaxTimeoutSeconds) * time.Second
	localTransport := &http.Transport{Proxy: nil, DialContext: (&net.Dialer{Timeout: 5 * time.Second}).DialContext, ResponseHeaderTimeout: requestTimeout, MaxIdleConns: 4, IdleConnTimeout: 90 * time.Second}
	return &IntelligenceMonitorService{repo: repo, encryptor: encryptor, upstreams: upstreamRepo, groups: groupRepo, keys: apiKeys, finance: finance, cfg: cfg, externalClient: newSSRFSafeHTTPClientWithHeaderTimeout(requestTimeout, requestTimeout), localClient: &http.Client{Timeout: requestTimeout, Transport: localTransport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, localEndpoint: localEndpoint, ctx: ctx, cancel: cancel, slots: make(chan struct{}, 2), wake: make(chan struct{}, 1)}
}

func (s *IntelligenceMonitorService) ListPlans(ctx context.Context) ([]*IntelligenceMonitorPlan, error) {
	plans, err := s.repo.ListPlans(ctx)
	if err != nil {
		return nil, err
	}
	data, err := s.loadPlanListData(ctx, plans)
	if err != nil {
		return nil, err
	}
	for _, p := range plans {
		s.decoratePlan(p)
		p.LatestRun, p.RateSnapshot, p.SourceName = nil, nil, ""
		runs := data.Runs[p.ID]
		if len(runs) > 0 {
			p.LatestRun = runs[0]
			// An active run remains visible even if its creation timestamp is
			// older than completed records (for example after a clock change).
			for _, run := range runs {
				if run.Status == "pending" || run.Status == "running" {
					p.LatestRun = run
					break
				}
			}
			p.RateSnapshot = p.LatestRun.RateSnapshot
			p.SourceName = p.LatestRun.SourceName
		}
		p.RecentRuns = make([]*IntelligenceMonitorRun, 0, IntelligenceMonitorRetainedRuns)
		for _, run := range runs {
			if (run.Status == "succeeded" || run.Status == "failed") && len(p.RecentRuns) < IntelligenceMonitorRetainedRuns {
				p.RecentRuns = append(p.RecentRuns, run)
			}
		}
		currentName, hasSource := data.SourceNames[p.ID]
		if p.SourceType == "openai_oauth" && hasSource {
			p.Name, p.SourceName = currentName, currentName
		}
		if p.SourceName == "" {
			switch p.SourceType {
			case "upstream", "local_group":
				p.SourceName = currentName
			default:
				p.SourceName = p.Name
			}
		}
	}
	return plans, nil
}
func (s *IntelligenceMonitorService) decoratePlan(p *IntelligenceMonitorPlan) {
	p.OAuth = p.SourceType == "openai_oauth"
	p.Model = IntelligenceMonitorModel
	p.ReasoningEffort = IntelligenceMonitorReasoning
	p.Prompt = IntelligenceMonitorPrompt
	if p.SourceType == "external" {
		plain, err := s.encryptor.Decrypt(p.APIKeyEncrypted)
		if err == nil && len(plain) > 8 {
			p.APIKeyMasked = plain[:4] + "••••" + plain[len(plain)-4:]
		} else {
			p.APIKeyMasked = "***"
		}
	}
}

func (s *IntelligenceMonitorService) SavePlan(ctx context.Context, id, actorID int64, in IntelligenceMonitorInput) (*IntelligenceMonitorPlan, error) {
	if actorID <= 0 {
		return nil, ErrIntelligenceInvalid
	}
	p := &IntelligenceMonitorPlan{SourceType: "external", APIMode: MonitorAPIModeResponses, IntervalSeconds: IntelligenceMonitorDefaultIntervalSeconds, TimeoutSeconds: IntelligenceMonitorDefaultTimeoutSeconds, CreatedBy: actorID}
	var old *IntelligenceMonitorPlan
	if id > 0 {
		var err error
		p, err = s.repo.GetPlan(ctx, id)
		if err != nil {
			return nil, err
		}
		copy := *p
		old = &copy
	}
	if id > 0 && intelligenceEnabledOnly(in) {
		if p.SourceType == "openai_oauth" {
			if *in.Enabled {
				account, err := s.intelligenceOAuthAccount(ctx, p.AccountID)
				if err != nil {
					return nil, err
				}
				p.Name, p.SourceName = account.Name, account.Name
			} else if p.AccountID != nil && s.accounts != nil {
				// Pausing must remain possible after an account is disabled,
				// deleted or temporarily unavailable.
				if account, e := s.accounts.GetByID(ctx, *p.AccountID); e == nil && account != nil {
					p.Name, p.SourceName = account.Name, account.Name
				}
			}
		}
		p.Enabled = *in.Enabled
		p.AllowWhileBusy = true
		if err := s.repo.SavePlan(ctx, p); err != nil {
			return nil, err
		}
		s.decoratePlan(p)
		s.notify()
		return p, nil
	}
	applyUpstreamString(&p.Name, in.Name)
	applyUpstreamString(&p.SourceType, in.SourceType)
	applyUpstreamString(&p.Endpoint, in.Endpoint)
	applyUpstreamString(&p.APIMode, in.APIMode)
	applyUpstreamString(&p.SupplierNote, in.SupplierNote)
	applyUpstreamString(&p.GroupNote, in.GroupNote)
	applyUpstreamString(&p.RateNote, in.RateNote)
	applyUpstreamString(&p.Notes, in.Notes)
	if in.Enabled != nil {
		p.Enabled = *in.Enabled
	}
	if in.IntervalSeconds != nil {
		p.IntervalSeconds = *in.IntervalSeconds
	}
	if in.TimeoutSeconds != nil {
		p.TimeoutSeconds = *in.TimeoutSeconds
	}
	if len(in.UpstreamTargetID) > 0 {
		if json.Unmarshal(in.UpstreamTargetID, &p.UpstreamTargetID) != nil {
			return nil, ErrIntelligenceInvalid
		}
	}
	if len(in.GroupID) > 0 {
		if json.Unmarshal(in.GroupID, &p.GroupID) != nil {
			return nil, ErrIntelligenceInvalid
		}
	}
	if len(in.AccountID) > 0 {
		if json.Unmarshal(in.AccountID, &p.AccountID) != nil {
			return nil, ErrIntelligenceInvalid
		}
	}
	if p.SourceType == "openai_oauth" {
		account, err := s.intelligenceOAuthAccount(ctx, p.AccountID)
		if err != nil {
			return nil, err
		}
		// The account is the name source; caller-supplied names are ignored.
		p.Name, p.SourceName = account.Name, account.Name
		p.APIMode = MonitorAPIModeResponses
	}
	if p.IntervalSeconds < 30 || p.IntervalSeconds > 86400 {
		return nil, ErrIntelligenceInvalid.WithMetadata(map[string]string{"field": "interval_seconds", "detail": "choose an integer interval between 30 and 86400 seconds"})
	}
	if p.TimeoutSeconds < IntelligenceMonitorMinTimeoutSeconds || p.TimeoutSeconds > IntelligenceMonitorMaxTimeoutSeconds {
		return nil, ErrIntelligenceInvalid.WithMetadata(map[string]string{"field": "timeout_seconds", "detail": "choose an integer timeout between 180 and 900 seconds"})
	}
	if p.Name == "" || utf8.RuneCountInString(p.Name) > 100 || len(p.Endpoint) > 500 || len(p.SupplierNote) > 500 || len(p.GroupNote) > 500 || len(p.RateNote) > 500 || len(p.Notes) > 4000 {
		return nil, ErrIntelligenceInvalid
	}
	if p.APIMode != MonitorAPIModeResponses && p.APIMode != MonitorAPIModeChatCompletions {
		return nil, ErrIntelligenceInvalid
	}
	var createdKey *APIKey
	switch p.SourceType {
	case "external":
		p.AccountID = nil
		p.UpstreamTargetID = nil
		p.GroupID = nil
		p.LocalAPIKeyID = nil
		p.LocalKeyOwnerID = nil
		plain := ""
		if in.APIKey != nil {
			plain = strings.TrimSpace(*in.APIKey)
		}
		if plain == "" && old != nil && old.SourceType == "external" {
			var err error
			plain, err = s.encryptor.Decrypt(old.APIKeyEncrypted)
			if err != nil {
				return nil, ErrChannelMonitorAPIKeyDecryptFailed
			}
		}
		if plain == "" || len(plain) > 2000 || strings.ContainsAny(plain, "\r\n") {
			return nil, ErrChannelMonitorMissingAPIKey
		}
		p.Endpoint = normalizeEndpoint(p.Endpoint)
		u, err := url.Parse(p.Endpoint)
		if err != nil || u.User != nil {
			return nil, ErrChannelMonitorInvalidEndpoint
		}
		if old == nil || old.SourceType != "external" || old.Endpoint != p.Endpoint {
			if err = validateEndpoint(p.Endpoint); err != nil {
				return nil, err
			}
		}
		p.APIKeyEncrypted, err = s.encryptor.Encrypt(plain)
		if err != nil {
			return nil, err
		}
	case "upstream":
		p.AccountID = nil
		if p.UpstreamTargetID == nil || *p.UpstreamTargetID <= 0 {
			return nil, ErrIntelligenceInvalid
		}
		target, err := s.upstreams.GetTarget(ctx, *p.UpstreamTargetID)
		if err != nil {
			return nil, err
		}
		if target.Provider != MonitorProviderOpenAI {
			return nil, ErrIntelligenceInvalid.WithMetadata(map[string]string{"detail": "the fixed model requires an OpenAI-compatible upstream target"})
		}
		p.Endpoint = ""
		p.APIKeyEncrypted = ""
		p.GroupID = nil
		p.LocalAPIKeyID = nil
		p.LocalKeyOwnerID = nil
		p.SourceName = target.Name
	case "local_group":
		p.AccountID = nil
		if p.GroupID == nil || *p.GroupID <= 0 || s.keys == nil {
			return nil, ErrIntelligenceInvalid
		}
		group, err := s.groups.GetByID(ctx, *p.GroupID)
		if err != nil {
			return nil, err
		}
		if group.Platform != PlatformOpenAI && group.Platform != PlatformComposite {
			return nil, ErrIntelligenceInvalid.WithMetadata(map[string]string{"detail": "choose an OpenAI or composite group supporting the fixed model"})
		}
		if group.Status != StatusActive {
			return nil, ErrIntelligenceInvalid.WithMetadata(map[string]string{"detail": "the selected group is disabled"})
		}
		if old == nil || old.SourceType != "local_group" || !sameUpstreamSupplier(old.GroupID, p.GroupID) {
			createdKey, err = s.keys.Create(ctx, actorID, CreateAPIKeyRequest{Name: "智商监控 · " + p.Name, GroupID: p.GroupID, IPWhitelist: []string{"127.0.0.1/32", "::1/128"}})
			if err != nil {
				return nil, err
			}
			p.LocalAPIKeyID = &createdKey.ID
			p.LocalKeyOwnerID = &createdKey.UserID
		}
		p.UpstreamTargetID = nil
		p.Endpoint = ""
		p.APIKeyEncrypted = ""
		p.SourceName = group.Name
	case "openai_oauth":
		p.Endpoint, p.APIKeyEncrypted = "", ""
		p.UpstreamTargetID, p.GroupID, p.LocalAPIKeyID, p.LocalKeyOwnerID = nil, nil, nil, nil
	default:
		return nil, ErrIntelligenceInvalid
	}
	if err := s.repo.SavePlan(ctx, p); err != nil {
		if createdKey != nil {
			s.cleanupKey(createdKey.ID, createdKey.UserID)
		}
		return nil, err
	}
	if old != nil && old.LocalAPIKeyID != nil && !sameUpstreamSupplier(old.LocalAPIKeyID, p.LocalAPIKeyID) && old.LocalKeyOwnerID != nil {
		s.cleanupKey(*old.LocalAPIKeyID, *old.LocalKeyOwnerID)
	}
	s.decoratePlan(p)
	s.notify()
	return p, nil
}

func (s *IntelligenceMonitorService) cleanupKey(id, owner int64) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := s.keys.Delete(ctx, id, owner); err != nil {
		slog.Warn("intelligence monitor dedicated key cleanup failed", "key_id", id, "error", err)
		key, e := s.keys.GetByID(ctx, id)
		if e == nil {
			s.keys.InvalidateAuthCacheByKey(ctx, key.Key)
		}
	}
}
func intelligenceEnabledOnly(in IntelligenceMonitorInput) bool {
	return in.Enabled != nil && in.Name == nil && in.SourceType == nil && in.Endpoint == nil && in.APIKey == nil && len(in.UpstreamTargetID) == 0 && len(in.GroupID) == 0 && len(in.AccountID) == 0 && in.SupplierNote == nil && in.GroupNote == nil && in.RateNote == nil && in.Notes == nil && in.APIMode == nil && in.IntervalSeconds == nil && in.TimeoutSeconds == nil
}
func (s *IntelligenceMonitorService) DeletePlan(ctx context.Context, id int64) error {
	plan, err := s.repo.GetPlan(ctx, id)
	if err != nil {
		return err
	}
	if err = s.repo.ArchivePlan(ctx, id); err != nil {
		return err
	}
	if plan.LocalAPIKeyID != nil && plan.LocalKeyOwnerID != nil {
		s.cleanupKey(*plan.LocalAPIKeyID, *plan.LocalKeyOwnerID)
	}
	return nil
}

func (s *IntelligenceMonitorService) Enqueue(ctx context.Context, id int64) (*IntelligenceMonitorRun, error) {
	return s.enqueue(ctx, id, false)
}
func (s *IntelligenceMonitorService) enqueue(ctx context.Context, id int64, scheduled bool) (*IntelligenceMonitorRun, error) {
	p, err := s.repo.GetPlan(ctx, id)
	if err != nil {
		return nil, err
	}
	run := &IntelligenceMonitorRun{PlanID: id, PlanName: p.Name, Trigger: "manual", Model: IntelligenceMonitorModel, ReasoningEffort: IntelligenceMonitorReasoning, Prompt: IntelligenceMonitorPrompt, SourceType: p.SourceType, SourceName: p.Name, SourceEndpoint: p.Endpoint, APIMode: p.APIMode, TimeoutSeconds: p.TimeoutSeconds, NotesSnapshot: map[string]string{"supplier_note": p.SupplierNote, "group_note": p.GroupNote, "rate_note": p.RateNote, "notes": p.Notes}, SourceSnapshot: map[string]any{"created_by": p.CreatedBy}, PlanUpdatedAt: p.UpdatedAt}
	if scheduled {
		run.Trigger = "scheduled"
	}
	// Snapshot API-key sources and OAuth account identity at queue time. OAuth
	// credentials are resolved/refreshed only by the gateway during execution.
	// Source failures are persisted and rendered as failed by the worker, too.
	switch p.SourceType {
	case "openai_oauth":
		run.OAuth = true
		run.APIMode = MonitorAPIModeResponses
		run.SourceEndpoint = ""
		run.SourceSnapshot["account_id"] = p.AccountID
		run.SourceSnapshot["auth_type"] = "oauth"
		run.SourceSnapshot["oauth"] = true
		if account, e := s.intelligenceOAuthAccount(ctx, p.AccountID); e != nil {
			run.SourceSnapshot["resolution_error"] = "selected OpenAI OAuth account is unavailable"
		} else {
			run.PlanName, run.SourceName = account.Name, account.Name
			run.SourceSnapshot["account_name"] = account.Name
			run.SourceSnapshot["account_type"] = account.Type
			run.SourceSnapshot["platform"] = account.Platform
		}
	case "external":
		run.RequestKeyEncrypted = p.APIKeyEncrypted
	case "upstream":
		if p.UpstreamTargetID != nil {
			run.SourceSnapshot["upstream_target_id"] = *p.UpstreamTargetID
			target, e := s.upstreams.GetTarget(ctx, *p.UpstreamTargetID)
			if e != nil {
				run.SourceSnapshot["resolution_error"] = "upstream target is unavailable"
			} else {
				run.SourceName = target.Name
				run.SourceEndpoint = target.Endpoint
				run.RequestKeyEncrypted = target.APIKeyEncrypted
				run.SourceSnapshot["supplier_id"] = target.SupplierID
			}
		}
	case "local_group":
		run.SourceEndpoint = s.localEndpoint
		run.SourceSnapshot["group_id"] = p.GroupID
		run.SourceSnapshot["local_api_key_id"] = p.LocalAPIKeyID
		if p.GroupID != nil {
			if group, e := s.groups.GetByID(ctx, *p.GroupID); e == nil {
				run.SourceName = group.Name
			}
		}
		if p.LocalAPIKeyID == nil {
			run.SourceSnapshot["resolution_error"] = "monitoring API key is missing"
		} else {
			key, e := s.keys.GetByID(ctx, *p.LocalAPIKeyID)
			if e != nil || key.GroupID == nil || !sameUpstreamSupplier(key.GroupID, p.GroupID) || !key.IsActive() {
				run.SourceSnapshot["resolution_error"] = "monitoring API key is unavailable or its group changed"
			} else {
				run.RequestKeyEncrypted, e = s.encryptor.Encrypt(key.Key)
				if e != nil {
					return nil, e
				}
			}
		}
	}
	if err = s.repo.Enqueue(ctx, run, scheduled); err != nil {
		return nil, err
	}
	s.notify()
	return run, nil
}

func (s *IntelligenceMonitorService) ListRuns(ctx context.Context, q IntelligenceMonitorRunQuery) (*IntelligenceMonitorRunPage, error) {
	if q.Page < 1 {
		q.Page = 1
	}
	if q.PageSize < 1 {
		q.PageSize = 20
	}
	if q.PageSize > 100 {
		q.PageSize = 100
	}
	return s.repo.ListRuns(ctx, q)
}
func (s *IntelligenceMonitorService) GetRun(ctx context.Context, id int64) (*IntelligenceMonitorRun, error) {
	return s.repo.GetRun(ctx, id)
}
func (s *IntelligenceMonitorService) notify() {
	select {
	case s.wake <- struct{}{}:
	default:
	}
}
func (s *IntelligenceMonitorService) Start() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.started || s.stopped {
		return
	}
	s.started = true
	s.wg.Add(1)
	go s.loop()
}
func (s *IntelligenceMonitorService) Stop() {
	s.mu.Lock()
	s.stopped = true
	s.cancel()
	s.mu.Unlock()
	s.wg.Wait()
}
func (s *IntelligenceMonitorService) loop() {
	defer s.wg.Done()
	cleanupCtx, cleanupCancel := context.WithTimeout(s.ctx, 30*time.Second)
	if err := s.repo.PruneRuns(cleanupCtx); err != nil && s.ctx.Err() == nil {
		slog.Warn("intelligence monitoring startup retention cleanup failed", "error", err)
	}
	cleanupCancel()
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()
	for {
		if s.ctx.Err() != nil {
			return
		}
		s.tick()
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
		case <-s.wake:
		}
	}
}
func (s *IntelligenceMonitorService) tick() {
	ctx, cancel := context.WithTimeout(s.ctx, 15*time.Second)
	defer cancel()
	if err := s.repo.ExpireRuns(ctx); err != nil {
		slog.Warn("intelligence monitoring expiry failed", "error", err)
		return
	}
	ids, err := s.repo.DuePlanIDs(ctx, 10)
	if err != nil {
		slog.Warn("intelligence monitoring schedule failed", "error", err)
		return
	}
	for _, id := range ids {
		if _, err = s.enqueue(ctx, id, true); err != nil && !errors.Is(err, ErrIntelligenceBusy) && !errors.Is(err, ErrIntelligenceNotFound) {
			slog.Warn("intelligence monitoring enqueue failed", "plan_id", id, "error", err)
		}
	}
	for len(s.slots) < cap(s.slots) {
		run, err := s.repo.ClaimNext(ctx, uuid.NewString())
		if err != nil {
			slog.Warn("intelligence monitoring claim failed", "error", err)
			return
		}
		if run == nil {
			return
		}
		s.slots <- struct{}{}
		s.wg.Add(1)
		go func(run *IntelligenceMonitorRun) {
			defer s.wg.Done()
			defer func() { <-s.slots; s.notify() }()
			s.execute(run)
		}(run)
	}
}

func (s *IntelligenceMonitorService) execute(run *IntelligenceMonitorRun) {
	ctx, cancel := context.WithTimeout(s.ctx, time.Duration(run.TimeoutSeconds+45)*time.Second)
	defer cancel()
	run.Status = "failed"
	if run.SourceType == "openai_oauth" {
		requestCtx, requestCancel := context.WithTimeout(ctx, time.Duration(run.TimeoutSeconds)*time.Second)
		run.HTTPStatus, run.RawText, run.Error = s.generateOpenAIOAuth(requestCtx, run)
		requestCancel()
		s.finishIntelligenceRun(run)
		return
	}
	key, err := s.encryptor.Decrypt(run.RequestKeyEncrypted)
	if msg, _ := run.SourceSnapshot["resolution_error"].(string); msg != "" {
		run.Error = msg
	} else if err != nil || key == "" {
		run.Error = "monitoring credential is unavailable"
	} else if sourceError := s.validateRunSource(ctx, run, key); sourceError != "" {
		run.Error = sourceError
	} else {
		switch run.SourceType {
		case "upstream":
			id := intelligenceSnapshotID(run.SourceSnapshot["upstream_target_id"])
			if id > 0 && s.finance != nil {
				run.RateSnapshot, err = s.finance.SyncRemoteBilling(ctx, id)
				if err != nil {
					run.RateSnapshot, _ = s.finance.LatestRemoteBilling(ctx, id)
					if run.RateSnapshot != nil {
						copy := *run.RateSnapshot
						copy.Stale = true
						copy.Status = "error"
						copy.Error = "live multiplier synchronization unavailable"
						run.RateSnapshot = &copy
					}
				}
			}
		case "local_group":
			run.RateSnapshot = s.fetchLocalBilling(ctx, run, key)
		}
		requestCtx, requestCancel := context.WithTimeout(ctx, time.Duration(run.TimeoutSeconds)*time.Second)
		if sourceError := s.validateRunSource(requestCtx, run, key); sourceError != "" {
			run.Error = sourceError
		} else {
			run.HTTPStatus, run.RawText, run.Error = s.generate(requestCtx, run, key)
		}
		requestCancel()
	}
	s.finishIntelligenceRun(run)
}

func (s *IntelligenceMonitorService) finishIntelligenceRun(run *IntelligenceMonitorRun) {
	if run.Error == "" {
		run.HTML = extractIntelligenceHTML(run.RawText)
		if run.HTML == "" {
			run.Error = "model response did not contain an HTML document"
		} else {
			run.Status = "succeeded"
		}
	}
	saveCtx, saveCancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer saveCancel()
	if err := s.repo.CompleteRun(saveCtx, run); err != nil {
		slog.Error("intelligence monitoring result persistence failed", "run_id", run.ID, "error", err)
	}
}
func (s *IntelligenceMonitorService) validateRunSource(ctx context.Context, run *IntelligenceMonitorRun, key string) string {
	switch run.SourceType {
	case "upstream":
		id := intelligenceSnapshotID(run.SourceSnapshot["upstream_target_id"])
		target, err := s.upstreams.GetTarget(ctx, id)
		if err != nil {
			return "upstream target is unavailable"
		}
		currentKey, err := s.encryptor.Decrypt(target.APIKeyEncrypted)
		if err != nil || currentKey != key || target.Endpoint != run.SourceEndpoint {
			return "upstream credentials changed after this run was queued; create a new run"
		}
	case "local_group":
		id := intelligenceSnapshotID(run.SourceSnapshot["local_api_key_id"])
		groupID := intelligenceSnapshotID(run.SourceSnapshot["group_id"])
		apiKey, err := s.keys.GetByID(ctx, id)
		if err != nil || !apiKey.IsActive() || apiKey.GroupID == nil || *apiKey.GroupID != groupID || apiKey.Key != key {
			return "monitoring API key or its assigned group changed after this run was queued"
		}
	}
	return ""
}
func intelligenceSnapshotID(value any) int64 {
	switch v := value.(type) {
	case float64:
		return int64(v)
	case int64:
		return v
	case *int64:
		if v != nil {
			return *v
		}
	}
	return 0
}
