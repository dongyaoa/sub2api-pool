package service

import (
	"context"
	"encoding/json"
	"fmt"
	"sync/atomic"
	"time"
)

// AccountProxyPoolExtraKey is the durable JSON key used for the optional
// per-account proxy pool. The legacy proxy_id column remains the primary
// compatibility field and points at the first pool entry.
const AccountProxyPoolExtraKey = "proxy_pool"

const (
	accountProxyPoolMaxEntries     = 256
	accountProxyPoolMaxConcurrency = 100000
)

// AccountProxyPoolEntry binds one proxy to an account and gives that proxy its
// own capacity. Proxy is hydrated from the proxy repository and is omitted
// from the durable extra JSON.
type AccountProxyPoolEntry struct {
	ProxyID            int64  `json:"proxy_id"`
	Concurrency        int    `json:"concurrency"`
	CurrentConcurrency int    `json:"current_concurrency,omitempty"`
	Proxy              *Proxy `json:"proxy,omitempty"`
}

var accountProxyPoolRoundRobin atomic.Uint64

// ParseAccountProxyPool accepts values decoded from JSON (including
// []any/float64 values) and validates the user-configurable portion.
func ParseAccountProxyPool(value any) ([]AccountProxyPoolEntry, error) {
	if value == nil {
		return nil, nil
	}
	payload, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("invalid proxy_pool: %w", err)
	}
	var entries []AccountProxyPoolEntry
	if err := json.Unmarshal(payload, &entries); err != nil {
		return nil, fmt.Errorf("invalid proxy_pool: %w", err)
	}
	if len(entries) > accountProxyPoolMaxEntries {
		return nil, fmt.Errorf("proxy_pool cannot contain more than %d entries", accountProxyPoolMaxEntries)
	}
	seen := make(map[int64]struct{}, len(entries))
	total := 0
	for i := range entries {
		entry := &entries[i]
		if entry.ProxyID <= 0 {
			return nil, fmt.Errorf("proxy_pool[%d].proxy_id must be positive", i)
		}
		if entry.Concurrency <= 0 {
			return nil, fmt.Errorf("proxy_pool[%d].concurrency must be positive", i)
		}
		if _, exists := seen[entry.ProxyID]; exists {
			return nil, fmt.Errorf("proxy_pool contains duplicate proxy_id %d", entry.ProxyID)
		}
		seen[entry.ProxyID] = struct{}{}
		if total > accountProxyPoolMaxConcurrency-entry.Concurrency {
			return nil, fmt.Errorf("proxy_pool total concurrency cannot exceed %d", accountProxyPoolMaxConcurrency)
		}
		total += entry.Concurrency
		// Proxy is runtime-only and must never be accepted from a client payload.
		entry.Proxy = nil
	}
	return entries, nil
}

// AccountProxyPoolFromExtra returns a validated pool. Malformed legacy data is
// ignored by readers so one bad optional setting cannot make an account
// disappear from the scheduler; admin writes still return validation errors.
func AccountProxyPoolFromExtra(extra map[string]any) []AccountProxyPoolEntry {
	if len(extra) == 0 {
		return nil
	}
	entries, err := ParseAccountProxyPool(extra[AccountProxyPoolExtraKey])
	if err != nil {
		return nil
	}
	return entries
}

// SetAccountProxyPoolExtra stores only the stable IDs and capacities in extra.
func SetAccountProxyPoolExtra(extra map[string]any, entries []AccountProxyPoolEntry) {
	if extra == nil {
		return
	}
	if len(entries) == 0 {
		delete(extra, AccountProxyPoolExtraKey)
		return
	}
	serialized := make([]map[string]any, 0, len(entries))
	for _, entry := range entries {
		serialized = append(serialized, map[string]any{
			"proxy_id":    entry.ProxyID,
			"concurrency": entry.Concurrency,
		})
	}
	extra[AccountProxyPoolExtraKey] = serialized
}

func AccountProxyPoolConcurrency(entries []AccountProxyPoolEntry) int {
	total := 0
	for _, entry := range entries {
		if entry.Concurrency > 0 && total <= accountProxyPoolMaxConcurrency-entry.Concurrency {
			total += entry.Concurrency
		}
	}
	return total
}

// EffectiveAccountConcurrency returns the actual account-wide slot limit.
// A configured proxy pool owns the capacity for the account, including for
// legacy records whose accounts.concurrency still contains the first proxy's
// old value.
func EffectiveAccountConcurrency(account *Account) int {
	if account == nil {
		return 0
	}
	if poolConcurrency := AccountProxyPoolConcurrency(account.ProxyPool); poolConcurrency > 0 {
		return poolConcurrency
	}
	return account.Concurrency
}

// SelectAccountProxy chooses a proxy with weighted round-robin semantics. It
// preserves the account-wide concurrency cap while distributing requests in
// proportion to each proxy's configured capacity.
func SelectAccountProxy(account *Account) {
	selectAccountProxy(account, nil)
}

// selectAccountProxy chooses one configured proxy, optionally skipping IDs
// that were already rejected while trying to acquire a slot for this request.
func selectAccountProxy(account *Account, excluded map[int64]struct{}) {
	if account == nil || len(account.ProxyPool) == 0 {
		return
	}
	valid := make([]AccountProxyPoolEntry, 0, len(account.ProxyPool))
	for _, entry := range account.ProxyPool {
		if entry.Concurrency <= 0 {
			continue
		}
		if excluded != nil {
			if _, skip := excluded[entry.ProxyID]; skip {
				continue
			}
		}
		if entry.Proxy == nil || (entry.Proxy.Status == "" || entry.Proxy.IsActive()) && !entry.Proxy.IsExpired(time.Now()) {
			valid = append(valid, entry)
		}
	}
	total := AccountProxyPoolConcurrency(valid)
	if total <= 0 {
		account.Proxy = nil
		account.ProxyID = nil
		account.ProxyPoolSelected = true
		return
	}
	point := accountProxyPoolRoundRobin.Add(1) - 1
	remaining := int(point % uint64(total))
	for _, entry := range valid {
		if remaining < entry.Concurrency {
			proxyID := entry.ProxyID
			account.ProxyID = &proxyID
			account.Proxy = entry.Proxy
			account.ProxyPoolSelected = true
			return
		}
		remaining -= entry.Concurrency
	}
}

func selectNextAccountProxy(account *Account, excluded map[int64]struct{}) bool {
	if account == nil {
		return false
	}
	selectAccountProxy(account, excluded)
	return account.ProxyID != nil
}

// CarryAccountProxySelection preserves a proxy chosen before a scheduler
// hydration/recheck. This prevents the second read from advancing round-robin
// and acquiring a slot for a different proxy than the transport uses.
func CarryAccountProxySelection(source, target *Account) {
	if target == nil {
		return
	}
	if source == nil || !source.ProxyPoolSelected || source.ProxyID == nil {
		SelectAccountProxy(target)
		return
	}
	for _, entry := range target.ProxyPool {
		if entry.ProxyID == *source.ProxyID {
			target.ProxyID = source.ProxyID
			target.Proxy = entry.Proxy
			target.ProxyPoolSelected = true
			return
		}
	}
	SelectAccountProxy(target)
}

func normalizeAccountProxyPoolInput(ctx context.Context, proxyRepo ProxyRepository, entries *[]AccountProxyPoolEntry) ([]AccountProxyPoolEntry, error) {
	if entries == nil {
		return nil, nil
	}
	normalized, err := ParseAccountProxyPool(*entries)
	if err != nil {
		return nil, err
	}
	if len(normalized) > 0 && proxyRepo != nil {
		ids := make([]int64, 0, len(normalized))
		for _, entry := range normalized {
			ids = append(ids, entry.ProxyID)
		}
		proxies, err := proxyRepo.ListByIDs(ctx, ids)
		if err != nil {
			return nil, fmt.Errorf("validate proxy_pool: %w", err)
		}
		found := make(map[int64]struct{}, len(proxies))
		for _, proxy := range proxies {
			found[proxy.ID] = struct{}{}
		}
		for _, entry := range normalized {
			if _, ok := found[entry.ProxyID]; !ok {
				return nil, fmt.Errorf("proxy_pool references unknown proxy_id %d", entry.ProxyID)
			}
		}
	}
	return normalized, nil
}
