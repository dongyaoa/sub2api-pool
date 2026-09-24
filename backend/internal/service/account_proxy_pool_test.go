package service

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestParseAccountProxyPool(t *testing.T) {
	entries, err := ParseAccountProxyPool([]any{
		map[string]any{"proxy_id": float64(11), "concurrency": float64(10)},
		map[string]any{"proxy_id": float64(12), "concurrency": float64(5)},
	})
	require.NoError(t, err)
	require.Equal(t, []AccountProxyPoolEntry{
		{ProxyID: 11, Concurrency: 10},
		{ProxyID: 12, Concurrency: 5},
	}, entries)

	for _, input := range []any{
		[]AccountProxyPoolEntry{{ProxyID: 1, Concurrency: 1}, {ProxyID: 1, Concurrency: 2}},
		[]AccountProxyPoolEntry{{ProxyID: 1, Concurrency: 0}},
		[]AccountProxyPoolEntry{{ProxyID: 0, Concurrency: 1}},
	} {
		_, err := ParseAccountProxyPool(input)
		require.Error(t, err)
	}
}

func TestSelectAccountProxyUsesConfiguredWeights(t *testing.T) {
	account := &Account{ProxyPool: []AccountProxyPoolEntry{
		{ProxyID: 21, Concurrency: 1, Proxy: &Proxy{ID: 21, Status: StatusActive}},
		{ProxyID: 22, Concurrency: 2, Proxy: &Proxy{ID: 22, Status: StatusActive}},
	}}
	counts := map[int64]int{}
	for i := 0; i < 30; i++ {
		SelectAccountProxy(account)
		require.NotNil(t, account.ProxyID)
		counts[*account.ProxyID]++
	}
	require.Equal(t, 10, counts[21])
	require.Equal(t, 20, counts[22])
}

func TestSelectAccountProxyAlternatesEqualCapacityPerAccount(t *testing.T) {
	accounts := []*Account{
		{ID: 9101, ProxyPool: []AccountProxyPoolEntry{
			{ProxyID: 21, Concurrency: 20, Proxy: &Proxy{ID: 21, Status: StatusActive}},
			{ProxyID: 22, Concurrency: 20, Proxy: &Proxy{ID: 22, Status: StatusActive}},
		}},
		{ID: 9102, ProxyPool: []AccountProxyPoolEntry{
			{ProxyID: 21, Concurrency: 20, Proxy: &Proxy{ID: 21, Status: StatusActive}},
			{ProxyID: 22, Concurrency: 20, Proxy: &Proxy{ID: 22, Status: StatusActive}},
		}},
	}
	for i := 0; i < 8; i++ {
		for _, account := range accounts {
			// Scheduling reads fresh Account values for the same ID each time.
			snapshot := *account
			SelectAccountProxy(&snapshot)
			require.NotNil(t, snapshot.ProxyID)
			require.Equal(t, int64(21+i%2), *snapshot.ProxyID)
		}
	}
}

func TestSelectAccountProxySkipsUnavailableEntries(t *testing.T) {
	expired := time.Now().Add(-time.Minute)
	account := &Account{ID: 9103, ProxyPool: []AccountProxyPoolEntry{
		{ProxyID: 1, Concurrency: 20},
		{ProxyID: 2, Concurrency: 20, Proxy: &Proxy{ID: 2, Status: "inactive"}},
		{ProxyID: 3, Concurrency: 20, Proxy: &Proxy{ID: 3, Status: StatusActive, ExpiresAt: &expired}},
		{ProxyID: 4, Concurrency: 20, Proxy: &Proxy{ID: 99, Status: StatusActive}},
		{ProxyID: 5, Concurrency: 0, Proxy: &Proxy{ID: 5, Status: StatusActive}},
		{ProxyID: 6, Concurrency: 20, Proxy: &Proxy{ID: 6, Status: StatusActive}},
	}}
	for i := 0; i < 10; i++ {
		SelectAccountProxy(account)
		require.Equal(t, int64(6), *account.ProxyID)
	}
	selectAccountProxy(account, map[int64]struct{}{6: {}})
	require.Nil(t, account.ProxyID)
	require.Nil(t, account.Proxy)
	require.True(t, account.ProxyPoolSelected)
}

func TestSelectAccountProxyDeletedPoolDoesNotUsePrimary(t *testing.T) {
	id := int64(41)
	account := &Account{ID: 9104, ProxyID: &id, Proxy: &Proxy{ID: id, Status: StatusActive},
		Extra: map[string]any{AccountProxyPoolExtraKey: []AccountProxyPoolEntry{{ProxyID: 42, Concurrency: 20}}}}
	SelectAccountProxy(account)
	require.Nil(t, account.ProxyID)
	require.Nil(t, account.Proxy)
	require.False(t, accountProxySelectionUsable(account))
}

func TestAccountProxyPoolBalancerConcurrentFairness(t *testing.T) {
	var balancer accountProxyPoolBalancer
	entries := []AccountProxyPoolEntry{{ProxyID: 1, Concurrency: 1}, {ProxyID: 2, Concurrency: 3}}
	counts := make([]int, 2)
	var wg sync.WaitGroup
	var countsMu sync.Mutex
	for worker := 0; worker < 8; worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				index := balancer.next(1, entries, nil)
				countsMu.Lock()
				counts[index]++
				countsMu.Unlock()
			}
		}()
	}
	wg.Wait()
	require.Equal(t, []int{200, 600}, counts)
}

func TestAccountProxyPoolBalancerBoundsStateAndResetsChangedWeights(t *testing.T) {
	var balancer accountProxyPoolBalancer
	entries := []AccountProxyPoolEntry{{ProxyID: 1, Concurrency: 20}, {ProxyID: 2, Concurrency: 20}}
	for id := 1; id <= accountProxyPoolMaxStates+1; id++ {
		balancer.next(int64(id), entries, nil)
	}
	require.Len(t, balancer.states, accountProxyPoolMaxStates)
	require.NotContains(t, balancer.states, int64(1))
	entries[0].Concurrency = 1
	entries[1].Concurrency = 3
	counts := make([]int, 2)
	for i := 0; i < 20; i++ {
		counts[balancer.next(2, entries, nil)]++
	}
	require.Equal(t, []int{5, 15}, counts)
}

func TestParseAccountProxyPoolDiscardsRuntimeFields(t *testing.T) {
	entries, err := ParseAccountProxyPool([]AccountProxyPoolEntry{{
		ProxyID: 1, Concurrency: 20, CurrentConcurrency: 999, Proxy: &Proxy{ID: 1, Status: StatusActive},
	}})
	require.NoError(t, err)
	require.Equal(t, []AccountProxyPoolEntry{{ProxyID: 1, Concurrency: 20}}, entries)
}

func TestCarryAccountProxySelection(t *testing.T) {
	source := &Account{ProxyPool: []AccountProxyPoolEntry{
		{ProxyID: 31, Concurrency: 1, Proxy: &Proxy{ID: 31, Status: StatusActive}},
		{ProxyID: 32, Concurrency: 1, Proxy: &Proxy{ID: 32, Status: StatusActive}},
	}}
	SelectAccountProxy(source)
	target := &Account{ProxyPool: []AccountProxyPoolEntry{
		{ProxyID: 31, Concurrency: 1, Proxy: &Proxy{ID: 31, Status: StatusActive}},
		{ProxyID: 32, Concurrency: 1, Proxy: &Proxy{ID: 32, Status: StatusActive}},
	}}
	require.True(t, CarryAccountProxySelection(source, target))
	require.True(t, target.ProxyPoolSelected)
	require.Equal(t, *source.ProxyID, *target.ProxyID)
}

func TestCarryAccountProxySelectionRejectsStaleProxy(t *testing.T) {
	for _, scenario := range []string{"removed", "inactive", "expired", "missing hydration", "mismatched hydration", "zero capacity"} {
		t.Run(scenario, func(t *testing.T) {
			id := int64(31)
			source := &Account{ProxyID: &id, ProxyPoolSelected: true,
				ProxyPool: []AccountProxyPoolEntry{{ProxyID: id, Concurrency: 20}}}
			target := &Account{ProxyPool: []AccountProxyPoolEntry{
				{ProxyID: 31, Concurrency: 20, Proxy: &Proxy{ID: 31, Status: StatusActive}},
				{ProxyID: 32, Concurrency: 20, Proxy: &Proxy{ID: 32, Status: StatusActive}},
			}}
			switch scenario {
			case "removed":
				target.ProxyPool = target.ProxyPool[1:]
			case "inactive":
				target.ProxyPool[0].Proxy.Status = "inactive"
			case "expired":
				expired := time.Now().Add(-time.Minute)
				target.ProxyPool[0].Proxy.ExpiresAt = &expired
			case "missing hydration":
				target.ProxyPool[0].Proxy = nil
			case "mismatched hydration":
				target.ProxyPool[0].Proxy.ID = 99
			case "zero capacity":
				target.ProxyPool[0].Concurrency = 0
			}
			require.False(t, CarryAccountProxySelection(source, target))
			require.Nil(t, target.ProxyID, "must not switch to an IP without acquiring its slot")
			require.Nil(t, target.Proxy)
		})
	}
}

func TestCarryAccountProxySelectionRejectsChangedProxyCapacity(t *testing.T) {
	id := int64(31)
	source := &Account{ProxyID: &id, ProxyPoolSelected: true,
		ProxyPool: []AccountProxyPoolEntry{{ProxyID: id, Concurrency: 20}}}
	target := &Account{ProxyPool: []AccountProxyPoolEntry{
		{ProxyID: id, Concurrency: 1, Proxy: &Proxy{ID: id, Status: StatusActive}},
		{ProxyID: 32, Concurrency: 39, Proxy: &Proxy{ID: 32, Status: StatusActive}},
	}}
	require.False(t, CarryAccountProxySelection(source, target))
	require.Nil(t, target.ProxyID, "a changed per-IP capacity needs a new slot acquisition even if total capacity is unchanged")
}

func TestCarryAccountProxySelectionRejectsNewlyBoundPool(t *testing.T) {
	source := &Account{ID: 9105, Concurrency: 40}
	target := &Account{ID: source.ID, Concurrency: 40, ProxyPool: []AccountProxyPoolEntry{
		{ProxyID: 31, Concurrency: 20, Proxy: &Proxy{ID: 31, Status: StatusActive}},
		{ProxyID: 32, Concurrency: 20, Proxy: &Proxy{ID: 32, Status: StatusActive}},
	}}
	require.False(t, CarryAccountProxySelection(source, target), "an account-only reservation must not authorize a newly bound pool")
	require.Nil(t, target.ProxyID)
	require.Nil(t, target.Proxy)
	require.False(t, target.ProxyPoolSelected)
}
