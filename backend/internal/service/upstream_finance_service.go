package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
	"github.com/google/uuid"
	"golang.org/x/sync/errgroup"
)

const (
	upstreamBalanceTimeout  = 12 * time.Second
	upstreamBalanceMaxBody  = 2 * 1024 * 1024
	upstreamBalanceInterval = time.Minute
)

var (
	ErrUpstreamFinanceTargetNotFound  = infraerrors.NotFound("UPSTREAM_TARGET_NOT_FOUND", "Upstream target not found")
	ErrUpstreamFinanceIdentityChanged = infraerrors.Conflict("UPSTREAM_TARGET_CHANGED", "Upstream credentials changed during synchronization; retry")
	ErrUpstreamBalanceBusy            = infraerrors.Conflict("UPSTREAM_BALANCE_BUSY", "Balance synchronization is already running")
)

type UpstreamFinanceService struct {
	repo        UpstreamFinanceRepository
	encryptor   SecretEncryptor
	billing     *BillingService
	channels    *ChannelService
	accounts    AccountRepository
	client      *http.Client
	now         func() time.Time
	newAPICache newAPIFinanceCache
}

func NewUpstreamFinanceService(repo UpstreamFinanceRepository, encryptor SecretEncryptor, billing *BillingService, channels *ChannelService, accounts AccountRepository) *UpstreamFinanceService {
	client := newSSRFSafeHTTPClient(upstreamBalanceTimeout)
	// Redirects can change the credential recipient and are not part of this API.
	client.CheckRedirect = func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }
	return &UpstreamFinanceService{repo: repo, encryptor: encryptor, billing: billing, channels: channels, accounts: accounts, client: client, now: time.Now}
}

func normalizeUpstreamFinanceQuery(q UpstreamFinanceQuery, now time.Time) (UpstreamFinanceQuery, error) {
	if q.From.IsZero() {
		q.From = timezone.StartOfDay(now)
	}
	if q.To.IsZero() {
		q.To = now
	}
	if !q.To.After(q.From) || q.To.Sub(q.From) > 366*24*time.Hour {
		return q, infraerrors.BadRequest("INVALID_UPSTREAM_FINANCE_RANGE", "Time range must be increasing and at most 366 days")
	}
	if (q.SupplierID != nil && *q.SupplierID <= 0) || (q.TargetID != nil && *q.TargetID <= 0) {
		return q, infraerrors.BadRequest("INVALID_UPSTREAM_FINANCE_FILTER", "Supplier and target IDs must be positive")
	}
	if q.Page <= 0 {
		q.Page = 1
	}
	if q.PageSize <= 0 {
		q.PageSize = 50
	}
	if q.PageSize > 100 {
		q.PageSize = 100
	}
	if q.Page > 100000 {
		return q, infraerrors.BadRequest("INVALID_UPSTREAM_FINANCE_PAGE", "Page is too large")
	}
	return q, nil
}

func (s *UpstreamFinanceService) Summary(ctx context.Context, supplierID, targetID *int64, from, to time.Time) (*UpstreamFinanceSummary, error) {
	q, err := normalizeUpstreamFinanceQuery(UpstreamFinanceQuery{SupplierID: supplierID, TargetID: targetID, From: from, To: to}, s.now())
	if err != nil {
		return nil, err
	}
	return s.repo.Summary(ctx, q)
}

func (s *UpstreamFinanceService) Details(ctx context.Context, query UpstreamFinanceQuery) (*UpstreamFinancePage, error) {
	q, err := normalizeUpstreamFinanceQuery(query, s.now())
	if err != nil {
		return nil, err
	}
	summary, err := s.repo.Summary(ctx, q)
	if err != nil {
		return nil, err
	}
	items, total, err := s.repo.Details(ctx, q)
	if err != nil {
		return nil, err
	}
	if items == nil {
		items = []UpstreamFinanceRow{}
	}
	return &UpstreamFinancePage{Summary: summary, Items: items, Total: total, Page: q.Page, PageSize: q.PageSize}, nil
}

// UpstreamBalanceIdentity lets retention use exactly the same credential scope.
func UpstreamBalanceIdentity(t *UpstreamFinanceTarget) string { return upstreamBalanceIdentity(t) }

func upstreamBalanceIdentity(t *UpstreamFinanceTarget) string {
	supplier := ""
	if t.SupplierID != nil {
		supplier = fmt.Sprint(*t.SupplierID)
	}
	identity := t.Provider + "\x00" + t.Endpoint + "\x00" + t.APIKeyEncrypted + "\x00" + supplier + "\x00" + t.WalletRef
	// Preserve existing Sub2API snapshot identity; console authorization adds a
	// distinct identity so one account's wallet cannot survive a credential swap.
	if t.NewAPIUserID != 0 || t.NewAPIAccessTokenEncrypted != "" {
		identity += fmt.Sprintf("\x00newapi:%d\x00%s", t.NewAPIUserID, t.NewAPIAccessTokenEncrypted)
	}
	digest := sha256.Sum256([]byte(identity))
	return hex.EncodeToString(digest[:])
}

func (s *UpstreamFinanceService) LatestBalance(ctx context.Context, targetID int64) (*UpstreamBalanceSnapshot, error) {
	target, err := s.repo.GetTarget(ctx, targetID)
	if err != nil {
		return nil, err
	}
	snapshot, err := s.repo.LatestBalance(ctx, targetID, upstreamBalanceIdentity(target))
	if err != nil {
		return nil, err
	}
	if snapshot == nil {
		snapshot = &UpstreamBalanceSnapshot{TargetID: targetID, WalletRef: target.WalletRef, Kind: "unknown", Status: "pending"}
	}
	if snapshot.Billing == nil {
		snapshot.Billing = pendingUpstreamRemoteBilling()
	}
	markUpstreamRemoteBillingStale(snapshot.Billing, s.now())
	return snapshot, nil
}

func (s *UpstreamFinanceService) SyncBalance(ctx context.Context, targetID int64) (*UpstreamBalanceSnapshot, error) {
	// Capture identity after obtaining the lease; SaveBalance verifies it again.
	token := uuid.NewString()
	now := s.now().UTC()
	claimed, err := s.repo.ClaimBalance(ctx, targetID, token, now, now.Add(time.Minute))
	if err != nil {
		return nil, err
	}
	if !claimed {
		if _, loadErr := s.repo.GetTarget(ctx, targetID); loadErr != nil {
			return nil, loadErr
		}
		return nil, ErrUpstreamBalanceBusy
	}
	// Also release after a cancelled caller or persistence failure. The lease is
	// token-guarded so a late worker cannot clear a subsequent worker's claim.
	defer func() {
		releaseCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = s.repo.ReleaseBalance(releaseCtx, targetID, token, s.now().Add(upstreamBalanceInterval))
	}()
	target, err := s.repo.GetTarget(ctx, targetID)
	if err != nil {
		return nil, err
	}
	// Adapters can require several reads. Their combined budget must remain
	// shorter than the one-minute lease, including persistence and release.
	fetchCtx, cancelFetch := context.WithTimeout(ctx, 45*time.Second)
	snapshot := s.fetchBalance(fetchCtx, target)
	if snapshot.Billing == nil {
		snapshot.Billing = s.fetchRemoteBilling(fetchCtx, target)
	}
	cancelFetch()
	interval := upstreamBalanceInterval
	if snapshot.Billing != nil && snapshot.Billing.Source == "newapi_token" && snapshot.Status == "ok" {
		// New API's key-usage endpoint defaults to 20 requests / 20 minutes per
		// source IP; this metadata cadence never changes availability probes.
		interval = 20 * time.Minute
	}
	if err = s.repo.SaveBalance(ctx, target, upstreamBalanceIdentity(target), snapshot, token, s.now().Add(interval)); err != nil {
		return nil, err
	}
	return s.LatestBalance(ctx, targetID)
}

func (s *UpstreamFinanceService) SyncDueBalances(ctx context.Context) {
	ids, err := s.repo.DueBalanceTargetIDs(ctx, s.now().UTC(), 32)
	if err != nil {
		return
	}
	var group errgroup.Group
	group.SetLimit(4)
	for _, id := range ids {
		if ctx.Err() != nil {
			break
		}
		id := id
		group.Go(func() error { _, _ = s.SyncBalance(ctx, id); return nil })
	}
	_ = group.Wait()
}

func upstreamUsageURL(endpoint string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(endpoint))
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", errors.New("invalid endpoint")
	}
	path := strings.TrimRight(u.Path, "/")
	for _, suffix := range []string{"/v1/chat/completions", "/v1/responses", "/v1/messages", "/v1/models", "/v1/usage", "/v1beta/models", "/v1beta", "/v1"} {
		if strings.HasSuffix(path, suffix) {
			path = strings.TrimSuffix(path, suffix)
			break
		}
	}
	u.Path = path + "/v1/usage"
	u.RawPath = ""
	return u.String(), nil
}

func (s *UpstreamFinanceService) fetchSub2APIBalance(ctx context.Context, target *UpstreamFinanceTarget) *UpstreamBalanceSnapshot {
	now := s.now().UTC()
	snapshot := &UpstreamBalanceSnapshot{TargetID: target.ID, WalletRef: target.WalletRef, Kind: "unknown", Status: "error", SyncedAt: &now}
	fail := func(code string) *UpstreamBalanceSnapshot { snapshot.Error = code; return snapshot }
	requestURL, err := upstreamUsageURL(target.Endpoint)
	if err != nil {
		return fail("invalid_endpoint")
	}
	// The dialer rechecks DNS at connection time. Validation also disallows URL
	// userinfo/query fields and known metadata hostnames before decryption.
	if err = validateEndpoint(target.Endpoint); err != nil {
		return fail("endpoint_unavailable")
	}
	if s.encryptor == nil {
		return fail("credential_unavailable")
	}
	key, err := s.encryptor.Decrypt(target.APIKeyEncrypted)
	if err != nil || strings.TrimSpace(key) == "" {
		return fail("credential_unavailable")
	}
	reqCtx, cancel := context.WithTimeout(ctx, upstreamBalanceTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, requestURL, nil)
	if err != nil {
		return fail("request_invalid")
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Accept", "application/json")
	resp, err := s.client.Do(req)
	if err != nil {
		return fail("upstream_request_failed")
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusMethodNotAllowed {
		snapshot.Kind = "unsupported"
		snapshot.Status = "unsupported"
		return fail("usage_endpoint_unsupported")
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fail(fmt.Sprintf("upstream_http_%d", resp.StatusCode))
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, upstreamBalanceMaxBody+1))
	if err != nil {
		return fail("response_read_failed")
	}
	if len(body) > upstreamBalanceMaxBody {
		return fail("response_too_large")
	}
	parsed, err := parseUpstreamUsage(body)
	if err != nil {
		snapshot.Kind = "unsupported"
		snapshot.Status = "unsupported"
		return fail("usage_response_unsupported")
	}
	parsed.TargetID = target.ID
	parsed.WalletRef = target.WalletRef
	parsed.SyncedAt = &now
	parsed.Billing = parseUpstreamBillingFromUsage(body)
	if parsed.Billing != nil {
		parsed.Billing.SyncedAt = &now
		parsed.Billing.LastAttemptAt = &now
	}
	return parsed
}

// EstimateMonitorCost uses actual returned token counts and labels the result
// estimated. It never infers the amount from wallet differences or user prices.
func (s *UpstreamFinanceService) EstimateMonitorCost(ctx context.Context, targetID int64, model string, tokens UsageTokens) (*float64, error) {
	if s.billing == nil {
		return nil, nil
	}
	var base *float64
	if s.billing.HasIdentifiedTokenPricing(model) {
		base = tryModelFilePricing(s.billing, model, tokens, "", s.now())
	}
	ids, err := s.repo.ActiveAccountIDs(ctx, targetID)
	if err != nil {
		return nil, err
	}
	if len(ids) == 0 || s.accounts == nil {
		return base, nil
	}
	var estimate *float64
	for _, id := range ids {
		account, err := s.accounts.GetByID(ctx, id)
		if err != nil {
			return nil, err
		}
		if account == nil {
			return nil, nil
		}
		cost := base
		groupCostKnown := false
		// Groups can have different procurement pricing. A shared key with
		// conflicting rules is left unpriced instead of choosing arbitrarily.
		for _, group := range account.Groups {
			if group == nil {
				continue
			}
			candidate := base
			if s.channels != nil {
				channel, err := s.channels.GetChannelForGroup(ctx, group.ID)
				if err != nil {
					return nil, err
				}
				if channel != nil {
					custom := tryCustomRules(channel, account.ID, group.ID, account.Platform, model, tokens, 1)
					if custom != nil {
						candidate = custom
					} else if channel.ApplyPricingToAccountStats {
						// That policy uses the actual customer's total_cost. A
						// direct key probe has no such bill to borrow or invent.
						return nil, nil
					}
				}
			}
			if candidate == nil || (groupCostKnown && math.Abs(*candidate-*cost) > 1e-10) {
				return nil, nil
			}
			cost, groupCostKnown = candidate, true
		}
		if cost == nil {
			return nil, nil
		}
		amount := *cost * account.BillingRateMultiplier()
		if math.IsNaN(amount) || math.IsInf(amount, 0) || amount < 0 {
			return nil, nil
		}
		if estimate != nil && math.Abs(*estimate-amount) > 1e-10 {
			return nil, nil
		}
		estimate = &amount
	}
	return estimate, nil
}
