package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"unicode/utf8"
)

// New API exposes key quota separately from the user's wallet. The model key
// can read only /api/usage/token/; console authorization stays on account APIs.
// Contracts: QuantumNous/new-api controller/{token,user,group,misc}.go,
// verified against v0.10.8 and v1.0.0-rc.40. No generation is performed here.
type newAPIEnvelope struct {
	Success *bool           `json:"success"`
	Code    *bool           `json:"code"`
	Data    json.RawMessage `json:"data"`
	Error   json.RawMessage `json:"error"`
}

func decodeNewAPI(raw []byte, value any) bool {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	if decoder.Decode(value) != nil {
		return false
	}
	return decoder.Decode(new(any)) == io.EOF
}

func newAPIData(raw []byte) (json.RawMessage, bool) {
	var envelope newAPIEnvelope
	if !decodeNewAPI(raw, &envelope) || (envelope.Success == nil && envelope.Code == nil) ||
		(envelope.Success != nil && !*envelope.Success) || (envelope.Code != nil && !*envelope.Code) ||
		len(envelope.Data) == 0 || string(envelope.Data) == "null" ||
		(len(envelope.Error) > 0 && string(envelope.Error) != "null" && string(envelope.Error) != `""`) {
		return nil, false
	}
	return envelope.Data, true
}

func newAPIQuota(raw *float64, allowNegative bool) bool {
	return raw != nil && !math.IsNaN(*raw) && !math.IsInf(*raw, 0) && math.Abs(*raw) < 1e14 &&
		(allowNegative || *raw >= 0)
}

type newAPITokenUsage struct {
	Object         string   `json:"object"`
	TotalUsed      *float64 `json:"total_used"`
	TotalAvailable *float64 `json:"total_available"`
	UnlimitedQuota *bool    `json:"unlimited_quota"`
}

// Endpoint URLs always derive from the target's validated origin/path prefix.
// Query errors and upstream bodies must never be returned: token search is an
// official GET API whose query includes the key and older versions echo it.
func (s *UpstreamFinanceService) newAPIGet(ctx context.Context, base, path, key string, userID int64, query url.Values) ([]byte, string) {
	requestURL := base + path
	if len(query) > 0 {
		requestURL += "?" + query.Encode()
	}
	reqCtx, cancel := context.WithTimeout(ctx, upstreamBalanceTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(reqCtx, http.MethodGet, requestURL, nil)
	if err != nil {
		return nil, "newapi_request_failed"
	}
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	if userID > 0 {
		req.Header.Set("New-Api-User", strconv.FormatInt(userID, 10))
	}
	req.Header.Set("Accept", "application/json")
	resp, err := s.client.Do(req)
	if err != nil {
		return nil, "newapi_request_failed"
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Sprintf("newapi_upstream_http_%d", resp.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, upstreamBalanceMaxBody+1))
	if err != nil || len(raw) > upstreamBalanceMaxBody {
		return nil, "newapi_response_unsupported"
	}
	return raw, ""
}

func (s *UpstreamFinanceService) fetchBalance(ctx context.Context, target *UpstreamFinanceTarget) *UpstreamBalanceSnapshot {
	// Configuring a console credential explicitly opts this target into New API.
	if target.NewAPIUserID > 0 && target.NewAPIAccessTokenEncrypted != "" {
		snapshot, _ := s.fetchNewAPIBalance(ctx, target)
		return snapshot
	}
	snapshot := s.fetchSub2APIBalance(ctx, target)
	if snapshot.Status == "unsupported" || snapshot.Error == "upstream_http_400" || snapshot.Error == "upstream_http_401" || snapshot.Error == "upstream_http_403" {
		if alternate, recognized := s.fetchNewAPIBalance(ctx, target); recognized {
			return alternate
		}
	}
	return snapshot
}

func (s *UpstreamFinanceService) fetchNewAPIBalance(ctx context.Context, target *UpstreamFinanceTarget) (*UpstreamBalanceSnapshot, bool) {
	now := s.now().UTC()
	billing := &UpstreamRemoteBillingSnapshot{Source: "newapi_token", Status: "unsupported", BillingScope: "token", Stale: true, LastAttemptAt: &now, Error: "newapi_account_auth_required"}
	snapshot := &UpstreamBalanceSnapshot{TargetID: target.ID, WalletRef: target.WalletRef, Kind: "unknown", Status: "error", SyncedAt: &now, Billing: billing, Currency: "QUOTA", CurrencySource: "reported"}
	fail := func(code string) (*UpstreamBalanceSnapshot, bool) {
		snapshot.Error = code
		billing.Status, billing.Error = "error", code
		return snapshot, false
	}
	usageURL, err := upstreamUsageURL(target.Endpoint)
	if err != nil || validateEndpoint(target.Endpoint) != nil {
		return fail("endpoint_unavailable")
	}
	if s.encryptor == nil {
		return fail("credential_unavailable")
	}
	key, err := s.encryptor.Decrypt(target.APIKeyEncrypted)
	if err != nil || strings.TrimSpace(key) == "" {
		return fail("credential_unavailable")
	}
	base := strings.TrimSuffix(usageURL, "/v1/usage")
	recognized := false
	if target.NewAPIAccessTokenEncrypted == "" {
		if !s.allowNewAPIQuota(base) {
			_, _ = fail("newapi_rate_limited")
			return snapshot, s.isNewAPISite(base)
		}
		raw, requestError := s.newAPIGet(ctx, base, "/api/usage/token/", key, 0, nil)
		data, ok := newAPIData(raw)
		var usage newAPITokenUsage
		recognized = requestError == "" && ok && decodeNewAPI(data, &usage) && usage.Object == "token_usage" && usage.UnlimitedQuota != nil &&
			newAPIQuota(usage.TotalUsed, false) && (*usage.UnlimitedQuota || newAPIQuota(usage.TotalAvailable, true))
		if !recognized {
			if requestError == "" {
				requestError = "newapi_response_unsupported"
			}
			_, _ = fail(requestError)
			return snapshot, s.isNewAPISite(base)
		}
		s.markNewAPISite(base)
		snapshot.Kind, snapshot.Status = "key_quota", "ok"
		snapshot.UnlimitedQuota = *usage.UnlimitedQuota
		snapshot.TotalUsed = usage.TotalUsed
		if !snapshot.UnlimitedQuota {
			snapshot.QuotaRemaining = usage.TotalAvailable
		}
	}

	// A confirmed unit is required for money. Missing status must leave raw
	// quota points labelled as such, never divide by an assumed default 500000.
	raw, _ := s.newAPIGet(ctx, base, "/api/status", "", 0, nil)
	data, ok := newAPIData(raw)
	var status struct {
		QuotaPerUnit *float64 `json:"quota_per_unit"`
	}
	unit := float64(0)
	if ok && decodeNewAPI(data, &status) && newAPIQuota(status.QuotaPerUnit, false) && *status.QuotaPerUnit > 0 {
		unit = *status.QuotaPerUnit
	}
	defer func() {
		for _, value := range []*float64{snapshot.Balance, snapshot.QuotaRemaining, snapshot.TotalUsed} {
			if unit > 0 && value != nil {
				converted := *value / unit
				if !newAPIQuota(&converted, true) {
					unit = 0
				}
			}
		}
		if unit > 0 {
			for _, value := range []*float64{snapshot.Balance, snapshot.QuotaRemaining, snapshot.TotalUsed} {
				if value != nil {
					*value /= unit
				}
			}
			snapshot.Currency, snapshot.CurrencySource = "USD", "newapi_status"
		} else if snapshot.Status == "ok" {
			snapshot.Error = "newapi_quota_unit_unknown"
		}
	}()
	if target.NewAPIUserID <= 0 || target.NewAPIAccessTokenEncrypted == "" {
		return snapshot, recognized
	}
	billing.Source = "newapi_account"
	authFailure := func(code string) (*UpstreamBalanceSnapshot, bool) {
		billing.Status, billing.Error = "error", code
		if snapshot.Status != "ok" {
			snapshot.Error = code
		}
		return snapshot, recognized
	}
	consoleKey, err := s.encryptor.Decrypt(target.NewAPIAccessTokenEncrypted)
	if err != nil || consoleKey == "" {
		return authFailure("newapi_account_auth_failed")
	}
	raw, requestError := s.newAPIGet(ctx, base, "/api/user/self", consoleKey, target.NewAPIUserID, nil)
	data, ok = newAPIData(raw)
	var user struct {
		ID    int64    `json:"id"`
		Quota *float64 `json:"quota"`
		Group string   `json:"group"`
	}
	if requestError != "" || !ok || !decodeNewAPI(data, &user) {
		return authFailure("newapi_account_auth_failed")
	}
	if user.ID != target.NewAPIUserID {
		return authFailure("newapi_account_identity_mismatch")
	}
	token, lookupError := s.lookupNewAPIToken(ctx, base, key, consoleKey, target)
	if lookupError != "" {
		return authFailure(lookupError)
	}
	if !recognized {
		if token.UnlimitedQuota == nil || !newAPIQuota(token.UsedQuota, false) || (!*token.UnlimitedQuota && !newAPIQuota(token.RemainQuota, true)) {
			return authFailure("newapi_response_unsupported")
		}
		snapshot.Kind, snapshot.Status, snapshot.TotalUsed = "key_quota", "ok", token.UsedQuota
		snapshot.UnlimitedQuota = *token.UnlimitedQuota
		if !snapshot.UnlimitedQuota {
			snapshot.QuotaRemaining = token.RemainQuota
		}
		recognized = true
	}
	// Only a key proven to belong to this user may acquire its shared wallet.
	if newAPIQuota(user.Quota, true) {
		snapshot.Kind, snapshot.Balance = "wallet", user.Quota
	} else {
		snapshot.Error = "newapi_response_unsupported"
	}
	group := *token.Group
	if group == "" {
		group = user.Group
	}
	if group == "" || utf8.RuneCountInString(group) > 100 {
		return authFailure("newapi_group_rate_unavailable")
	}
	billing.GroupName = &group
	if group == "auto" {
		// This is a successful declaration of dynamic routing, not a stale old
		// fixed rate. Saving status=ok intentionally clears any previous rate.
		billing.Status, billing.Stale, billing.Error = "ok", false, "newapi_auto_group"
		billing.SyncedAt, billing.ObservedAt = &now, &now
		return snapshot, true
	}
	raw, requestError = s.newAPIGet(ctx, base, "/api/user/self/groups", consoleKey, target.NewAPIUserID, nil)
	data, ok = newAPIData(raw)
	var groups map[string]struct {
		Ratio json.RawMessage `json:"ratio"`
	}
	if requestError != "" || !ok || !decodeNewAPI(data, &groups) {
		return authFailure("newapi_group_rate_unavailable")
	}
	var ratio *float64
	if !decodeNewAPI(groups[group].Ratio, &ratio) || !newAPIQuota(ratio, false) {
		return authFailure("newapi_group_rate_unavailable")
	}
	// /self/groups includes user-group overrides. It does not expose the base
	// public ratio separately, so leave GroupRate/UserRate unknown.
	billing.ResolvedRateMultiplier, billing.EffectiveRateMultiplier = ratio, ratio
	billing.Status, billing.Stale, billing.Error = "ok", false, ""
	billing.SyncedAt, billing.ObservedAt = &now, &now
	return snapshot, true
}
