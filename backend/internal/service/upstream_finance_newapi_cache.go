package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	newAPIFinanceCacheLimit = 2048
	newAPITokenCacheTTL     = 24 * time.Hour
	newAPIQuotaRequestGap   = 65 * time.Second
	newAPISearchRequestGap  = 7 * time.Second
)

// Keep only verified identifiers, never keys, response bodies, or quota values.
// Looking up the ID once avoids New API's separate token-search rate limit.
type newAPIFinanceCache struct {
	mu          sync.Mutex
	tokenIDs    map[string]newAPITokenIDEntry
	quotaSites  map[string]newAPIQuotaSite
	searchAfter map[string]time.Time
}

type newAPIQuotaSite struct {
	after      time.Time
	knownUntil time.Time
}

type newAPITokenIDEntry struct {
	id        int64
	expiresAt time.Time
}

type newAPIToken struct {
	ID             int64    `json:"id"`
	UserID         int64    `json:"user_id"`
	Group          *string  `json:"group"`
	RemainQuota    *float64 `json:"remain_quota"`
	UsedQuota      *float64 `json:"used_quota"`
	UnlimitedQuota *bool    `json:"unlimited_quota"`
}

func (cache *newAPIFinanceCache) tokenID(identity string, now time.Time) int64 {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	entry, ok := cache.tokenIDs[identity]
	if !ok {
		return 0
	}
	if !now.Before(entry.expiresAt) {
		delete(cache.tokenIDs, identity)
		return 0
	}
	return entry.id
}

func (cache *newAPIFinanceCache) forgetTokenID(identity string, id int64) {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	if cache.tokenIDs[identity].id == id {
		delete(cache.tokenIDs, identity)
	}
}

func (cache *newAPIFinanceCache) rememberTokenID(identity string, id int64, now time.Time) {
	cache.mu.Lock()
	defer cache.mu.Unlock()
	if cache.tokenIDs == nil {
		cache.tokenIDs = make(map[string]newAPITokenIDEntry)
	}
	if _, exists := cache.tokenIDs[identity]; !exists && len(cache.tokenIDs) >= newAPIFinanceCacheLimit {
		oldestKey := ""
		var oldest time.Time
		for key, entry := range cache.tokenIDs {
			if oldestKey == "" || entry.expiresAt.Before(oldest) {
				oldestKey, oldest = key, entry.expiresAt
			}
		}
		delete(cache.tokenIDs, oldestKey)
	}
	cache.tokenIDs[identity] = newAPITokenIDEntry{id: id, expiresAt: now.Add(newAPITokenCacheTTL)}
}

func (s *UpstreamFinanceService) lookupNewAPIToken(ctx context.Context, base, key, consoleKey string, target *UpstreamFinanceTarget) (*newAPIToken, string) {
	// '%' is deliberately treated as a wildcard by New API's search endpoint.
	// Older versions trim every edge 's', 'k' and '-' before searching. If
	// nothing remains, they omit the key filter and return unrelated tokens.
	if strings.Contains(key, "%") || strings.TrimSpace(key) == "" || strings.Trim(key, "sk-") == "" {
		return nil, "newapi_token_lookup_unsupported"
	}
	identity := upstreamBalanceIdentity(target)
	id := s.newAPICache.tokenID(identity, s.now())
	if id > 0 {
		raw, requestError := s.newAPIGet(ctx, base, "/api/token/"+strconv.FormatInt(id, 10), consoleKey, target.NewAPIUserID, nil)
		if requestError == "newapi_upstream_http_404" {
			s.newAPICache.forgetTokenID(identity, id)
		} else {
			if requestError != "" {
				return nil, requestError
			}
			data, ok := newAPIData(raw)
			var token newAPIToken
			if !ok || !decodeNewAPI(data, &token) {
				return nil, "newapi_token_lookup_unsupported"
			}
			if token.ID != id || token.UserID != target.NewAPIUserID || token.Group == nil {
				s.newAPICache.forgetTokenID(identity, id)
				return nil, "newapi_account_identity_mismatch"
			}
			return &token, ""
		}
	}
	if !s.allowNewAPISearch(base, target.NewAPIUserID) {
		return nil, "newapi_rate_limited"
	}
	raw, requestError := s.newAPIGet(ctx, base, "/api/token/search", consoleKey, target.NewAPIUserID, url.Values{"token": {key}, "p": {"1"}, "page_size": {"2"}})
	if requestError != "" {
		return nil, requestError
	}
	data, ok := newAPIData(raw)
	var result struct {
		Total *int64        `json:"total"`
		Items []newAPIToken `json:"items"`
	}
	if !ok || !decodeNewAPI(data, &result) || result.Total == nil {
		return nil, "newapi_token_lookup_unsupported"
	}
	if *result.Total == 0 && len(result.Items) == 0 {
		return nil, "newapi_token_not_found"
	}
	if *result.Total != 1 || len(result.Items) != 1 {
		return nil, "newapi_token_ambiguous"
	}
	token := result.Items[0]
	if token.ID <= 0 || token.UserID != target.NewAPIUserID || token.Group == nil {
		return nil, "newapi_account_identity_mismatch"
	}
	s.newAPICache.rememberTokenID(identity, token.ID, s.now())
	return &token, ""
}

// The key-only usage endpoint shares a default 20 requests / 20 minutes limit
// across the calling IP. Different keys on the same site must share this gate.
func (s *UpstreamFinanceService) allowNewAPIQuota(base string) bool {
	identity := newAPIOriginIdentity(base)
	if identity == "" {
		return false
	}
	now := s.now()
	cache := &s.newAPICache
	cache.mu.Lock()
	defer cache.mu.Unlock()
	entry := cache.quotaSites[identity]
	if now.Before(entry.after) || !cache.reserveQuotaSite(identity, now) {
		return false
	}
	entry.after = now.Add(newAPIQuotaRequestGap)
	cache.quotaSites[identity] = entry
	return true
}

func newAPIOriginIdentity(base string) string {
	return newAPIURLIdentity(base, true)
}

func newAPIURLIdentity(base string, originOnly bool) string {
	u, err := url.Parse(strings.TrimSpace(base))
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return ""
	}
	u.Host = strings.ToLower(u.Host)
	if u.Port() == "443" {
		u.Host = strings.TrimSuffix(u.Host, ":443")
	}
	u.Path, u.RawPath = strings.TrimRight(u.Path, "/"), ""
	if originOnly {
		u.Path = ""
	}
	digest := sha256.Sum256([]byte(u.String()))
	return hex.EncodeToString(digest[:])
}

// Called while mu is held. Active gates must survive cache churn.
func (cache *newAPIFinanceCache) reserveQuotaSite(identity string, now time.Time) bool {
	if cache.quotaSites == nil {
		cache.quotaSites = make(map[string]newAPIQuotaSite)
	}
	if _, exists := cache.quotaSites[identity]; exists || len(cache.quotaSites) < newAPIFinanceCacheLimit {
		return true
	}
	oldestKey := ""
	var oldest time.Time
	for key, entry := range cache.quotaSites {
		if !now.Before(entry.after) && (oldestKey == "" || entry.knownUntil.Before(oldest)) {
			oldestKey, oldest = key, entry.knownUntil
		}
	}
	if oldestKey == "" {
		return false
	}
	delete(cache.quotaSites, oldestKey)
	return true
}

func (s *UpstreamFinanceService) markNewAPISite(base string) {
	identity := newAPIURLIdentity(base, false)
	if identity == "" {
		return
	}
	identity = "site:" + identity
	now := s.now()
	cache := &s.newAPICache
	cache.mu.Lock()
	defer cache.mu.Unlock()
	if !cache.reserveQuotaSite(identity, now) {
		return
	}
	entry := cache.quotaSites[identity]
	entry.knownUntil = now.Add(newAPITokenCacheTTL)
	cache.quotaSites[identity] = entry
}

func (s *UpstreamFinanceService) isNewAPISite(base string) bool {
	identity := newAPIURLIdentity(base, false)
	if identity == "" {
		return false
	}
	identity = "site:" + identity
	cache := &s.newAPICache
	cache.mu.Lock()
	defer cache.mu.Unlock()
	return s.now().Before(cache.quotaSites[identity].knownUntil)
}

func (s *UpstreamFinanceService) allowNewAPISearch(base string, userID int64) bool {
	origin := newAPIOriginIdentity(base)
	if origin == "" || userID <= 0 {
		return false
	}
	identity := origin + ":" + strconv.FormatInt(userID, 10)
	now := s.now()
	cache := &s.newAPICache
	cache.mu.Lock()
	defer cache.mu.Unlock()
	if now.Before(cache.searchAfter[identity]) {
		return false
	}
	if cache.searchAfter == nil {
		cache.searchAfter = make(map[string]time.Time)
	}
	if _, exists := cache.searchAfter[identity]; !exists && len(cache.searchAfter) >= newAPIFinanceCacheLimit {
		for key, after := range cache.searchAfter {
			if !now.Before(after) {
				delete(cache.searchAfter, key)
			}
		}
		if len(cache.searchAfter) >= newAPIFinanceCacheLimit {
			return false
		}
	}
	cache.searchAfter[identity] = now.Add(newAPISearchRequestGap)
	return true
}
