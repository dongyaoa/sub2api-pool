//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestUpstreamFinanceProbeRejectsConflictingDefaultAndCustomGroupPrices(t *testing.T) {
	for _, reverse := range []bool{false, true} {
		repo := &financeTestRepo{ids: []int64{1}}
		groups := []*Group{{ID: 10}, {ID: 20}}
		if reverse {
			groups[0], groups[1] = groups[1], groups[0]
		}
		accounts := &financeAccounts{accounts: map[int64]*Account{1: {ID: 1, Platform: PlatformAnthropic, Groups: groups, RateMultiplier: financeFloat(.25)}}}
		channels := &ChannelService{}
		cache := newEmptyChannelCache()
		cache.loadedAt = time.Now()
		cache.channelByGroupID[10] = &Channel{ID: 1, Status: StatusActive, AccountStatsPricingRules: []AccountStatsPricingRule{{GroupIDs: []int64{10}, Pricing: []ChannelModelPricing{{Models: []string{"claude-3-5-sonnet"}, InputPrice: financeFloat(.01), OutputPrice: financeFloat(.02)}}}}}
		channels.cache.Store(cache)
		svc := NewUpstreamFinanceService(repo, financeTestCipher{}, NewBillingService(nil, nil), channels, accounts)
		cost, err := svc.EstimateMonitorCost(context.Background(), 1, "claude-3-5-sonnet", UsageTokens{InputTokens: 100, OutputTokens: 10})
		require.NoError(t, err)
		require.Nil(t, cost, "default-priced group and custom-priced group are both possible procurement prices")
		// The same custom rule on both groups is unambiguous and stays priced.
		cache.channelByGroupID[10].AccountStatsPricingRules[0].GroupIDs = []int64{10, 20}
		cache.channelByGroupID[20] = cache.channelByGroupID[10]
		cost, err = svc.EstimateMonitorCost(context.Background(), 1, "claude-3-5-sonnet", UsageTokens{InputTokens: 100, OutputTokens: 10})
		require.NoError(t, err)
		require.NotNil(t, cost)
		require.InDelta(t, .3, *cost, 1e-12)
	}
}

func TestUpstreamFinanceProbeCustomerBillingPolicyRemainsUnknownWithoutActualBill(t *testing.T) {
	repo := &financeTestRepo{ids: []int64{1}}
	accounts := &financeAccounts{accounts: map[int64]*Account{1: {ID: 1, Groups: []*Group{{ID: 10}}}}}
	channels := &ChannelService{}
	cache := newEmptyChannelCache()
	cache.loadedAt = time.Now()
	cache.channelByGroupID[10] = &Channel{ID: 1, Status: StatusActive, ApplyPricingToAccountStats: true}
	channels.cache.Store(cache)
	svc := NewUpstreamFinanceService(repo, financeTestCipher{}, NewBillingService(nil, nil), channels, accounts)
	cost, err := svc.EstimateMonitorCost(context.Background(), 1, "claude-3-5-sonnet", UsageTokens{InputTokens: 100, OutputTokens: 10})
	require.NoError(t, err)
	require.Nil(t, cost)
	delete(accounts.accounts, 1)
	cost, err = svc.EstimateMonitorCost(context.Background(), 1, "claude-3-5-sonnet", UsageTokens{InputTokens: 100, OutputTokens: 10})
	require.NoError(t, err)
	require.Nil(t, cost, "a concurrently missing account must not panic")
}
