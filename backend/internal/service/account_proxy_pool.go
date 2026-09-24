package service

import (
	"container/list"
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

// AccountProxyPoolExtraKey is the durable JSON key used for the optional
// per-account proxy pool. The legacy proxy_id column remains the primary
// compatibility field and points at the first pool entry.
const AccountProxyPoolExtraKey = "proxy_pool"

const (
	accountProxyPoolMaxEntries     = 256
	accountProxyPoolMaxConcurrency = 100000
	accountProxyPoolMaxStates      = 4096
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

type accountProxyPoolWeight struct {
	proxyID     int64
	concurrency int
	current     int
}

type accountProxyPoolState struct {
	accountID int64
	weights   []accountProxyPoolWeight
}

// Snapshots are recreated during scheduling, so rotation state belongs to the
// account ID, not an Account pointer. Bound the LRU to avoid retaining deleted
// or idle accounts indefinitely. Only IDs and weights are kept here.
type accountProxyPoolBalancer struct {
	mu     sync.Mutex
	states map[int64]*list.Element
	lru    list.List
}

var accountProxyPools accountProxyPoolBalancer

func (b *accountProxyPoolBalancer) next(accountID int64, entries []AccountProxyPoolEntry, excluded map[int64]struct{}) int {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.states == nil {
		b.states = make(map[int64]*list.Element)
	}
	element := b.states[accountID]
	if element == nil {
		if len(b.states) >= accountProxyPoolMaxStates {
			oldest := b.lru.Back()
			// This private LRU only stores *accountProxyPoolState values.
			oldestState, _ := oldest.Value.(*accountProxyPoolState)
			delete(b.states, oldestState.accountID)
			b.lru.Remove(oldest)
		}
		element = b.lru.PushFront(&accountProxyPoolState{accountID: accountID})
		b.states[accountID] = element
	} else {
		b.lru.MoveToFront(element)
	}
	state, _ := element.Value.(*accountProxyPoolState)
	changed := len(state.weights) != len(entries)
	if !changed {
		for i, entry := range entries {
			if state.weights[i].proxyID != entry.ProxyID || state.weights[i].concurrency != entry.Concurrency {
				changed = true
				break
			}
		}
	}
	if changed {
		state.weights = make([]accountProxyPoolWeight, len(entries))
		for i, entry := range entries {
			state.weights[i] = accountProxyPoolWeight{proxyID: entry.ProxyID, concurrency: entry.Concurrency}
		}
	}
	selected, total := -1, 0
	for i := range state.weights {
		weight := &state.weights[i]
		if _, skip := excluded[weight.proxyID]; skip {
			continue
		}
		weight.current += weight.concurrency
		total += weight.concurrency
		if selected < 0 || weight.current > state.weights[selected].current {
			selected = i
		}
	}
	if selected >= 0 {
		state.weights[selected].current -= total
	}
	return selected
}

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
		entry.CurrentConcurrency = 0
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

// SelectAccountProxy uses smooth weighted round-robin for each account. Equal
// capacities alternate IPs on every request; unequal capacities are distributed
// proportionally without sending a capacity-sized burst to a single IP.
func SelectAccountProxy(account *Account) {
	selectAccountProxy(account, nil)
}

// selectAccountProxy chooses one configured proxy, optionally skipping IDs
// that were already rejected while trying to acquire a slot for this request.
func selectAccountProxy(account *Account, excluded map[int64]struct{}) {
	if account == nil || !accountHasProxyPool(account) {
		return
	}
	now := time.Now()
	valid := make([]AccountProxyPoolEntry, 0, len(account.ProxyPool))
	for _, entry := range account.ProxyPool {
		if accountProxyPoolEntryUsable(entry, now) {
			valid = append(valid, entry)
		}
	}
	account.Proxy, account.ProxyID = nil, nil
	account.ProxyPoolSelected = true
	if len(valid) > 0 {
		if index := accountProxyPools.next(account.ID, valid, excluded); index >= 0 {
			entry := valid[index]
			account.ProxyID, account.Proxy = &entry.ProxyID, entry.Proxy
		}
	}
}

func accountHasProxyPool(account *Account) bool {
	return account != nil && (len(account.ProxyPool) > 0 || len(AccountProxyPoolFromExtra(account.Extra)) > 0)
}

func accountProxyPoolEntryUsable(entry AccountProxyPoolEntry, now time.Time) bool {
	return entry.ProxyID > 0 && entry.Concurrency > 0 && entry.Proxy != nil && entry.Proxy.ID == entry.ProxyID &&
		(entry.Proxy.Status == "" || entry.Proxy.IsActive()) && !entry.Proxy.IsExpired(now)
}

func accountProxySelectionUsable(account *Account) bool {
	if !accountHasProxyPool(account) {
		return true
	}
	if account.ProxyID == nil || account.Proxy == nil || account.Proxy.ID != *account.ProxyID {
		return false
	}
	for _, entry := range account.ProxyPool {
		if entry.ProxyID == *account.ProxyID {
			return accountProxyPoolEntryUsable(entry, time.Now())
		}
	}
	return false
}

func selectNextAccountProxy(account *Account, excluded map[int64]struct{}) bool {
	if account == nil {
		return false
	}
	selectAccountProxy(account, excluded)
	return account.ProxyID != nil
}

// CarryAccountProxySelection preserves a proxy chosen before a scheduler
// hydration/recheck. A stale selection is rejected instead of changing to an IP
// whose concurrency slot the caller has not acquired.
func CarryAccountProxySelection(source, target *Account) bool {
	if target == nil || target.ProxyPoolMetadata {
		return false
	}
	if source != nil && accountHasProxyPool(source) != accountHasProxyPool(target) {
		// Pool topology changed after the account slot was acquired. The old
		// slot cannot authorize a newly bound proxy; retry with a fresh snapshot.
		target.Proxy, target.ProxyID = nil, nil
		target.ProxyPoolSelected = false
		return false
	}
	if source == nil || !source.ProxyPoolSelected {
		SelectAccountProxy(target)
		return accountProxySelectionUsable(target)
	}
	selectedID := source.ProxyID
	target.Proxy, target.ProxyID = nil, nil
	target.ProxyPoolSelected = true
	if selectedID == nil {
		return false
	}
	now := time.Now()
	for _, entry := range target.ProxyPool {
		if entry.ProxyID == *selectedID && accountProxyPoolEntryUsable(entry, now) {
			for _, previous := range source.ProxyPool {
				if previous.ProxyID == entry.ProxyID && previous.Concurrency != entry.Concurrency {
					return false
				}
			}
			proxyID := entry.ProxyID
			target.ProxyID = &proxyID
			target.Proxy = entry.Proxy
			return true
		}
	}
	return false
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
