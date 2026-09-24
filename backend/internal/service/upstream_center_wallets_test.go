//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestUpstreamSupplierWalletOmitsDuplicateUnknownKeyWithoutHidingTargetError(t *testing.T) {
	now := time.Now()
	errorAt := now.Add(time.Minute)
	good := &UpstreamBalanceSnapshot{TargetID: 1, Kind: "wallet", WalletRef: "default", Balance: financeFloat(13), Currency: "USD", Status: "ok", SyncedAt: &now, LastAttemptAt: &now}
	failed := &UpstreamBalanceSnapshot{TargetID: 2, Kind: "unknown", WalletRef: "default", Status: "error", Error: "upstream_http_401", LastAttemptAt: &errorAt}
	first := &UpstreamTarget{ID: 1, WalletRef: "default", Balance: good}
	second := &UpstreamTarget{ID: 2, WalletRef: "default", Balance: failed}
	for _, targets := range [][]*UpstreamTarget{{first, second}, {second, first}} {
		wallets := upstreamSupplierWallets(targets)
		require.Len(t, wallets, 1)
		require.Equal(t, good, wallets[0])
		require.NotSame(t, good, wallets[0])
		require.Equal(t, "error", second.Balance.Status)
		require.Equal(t, "upstream_http_401", second.Balance.Error)
		require.Nil(t, second.Balance.Balance)
	}
}

func TestUpstreamSupplierWalletKeepsAllowanceIdentitiesAndCurrenciesSeparate(t *testing.T) {
	targets := []*UpstreamTarget{
		{ID: 1, WalletRef: "default", Balance: &UpstreamBalanceSnapshot{TargetID: 1, Kind: "wallet", Balance: financeFloat(10), Currency: "USD", Status: "ok"}},
		{ID: 2, WalletRef: "default", Balance: &UpstreamBalanceSnapshot{TargetID: 2, Kind: "wallet", Balance: financeFloat(20), Currency: "CNY", Status: "ok"}},
		{ID: 3, WalletRef: "default", Balance: &UpstreamBalanceSnapshot{TargetID: 3, Kind: "key_quota", QuotaRemaining: financeFloat(3), Status: "ok"}},
		{ID: 4, WalletRef: "default", Balance: &UpstreamBalanceSnapshot{TargetID: 4, Kind: "key_quota", QuotaRemaining: financeFloat(4), Status: "error", Error: "upstream_http_401"}},
		{ID: 5, WalletRef: "default", Balance: &UpstreamBalanceSnapshot{TargetID: 5, Kind: "subscription", QuotaRemaining: financeFloat(5), Status: "ok"}},
		{ID: 6, WalletRef: "default", Balance: &UpstreamBalanceSnapshot{TargetID: 6, Kind: "subscription", QuotaRemaining: financeFloat(6), Status: "ok"}},
		{ID: 7, WalletRef: "second", Balance: &UpstreamBalanceSnapshot{TargetID: 7, Kind: "wallet", Balance: financeFloat(7), Currency: "USD", Status: "ok"}},
	}
	wallets := upstreamSupplierWallets(targets)
	require.Len(t, wallets, len(targets))
	for i, wallet := range wallets {
		require.Equal(t, targets[i].ID, wallet.TargetID)
		require.Equal(t, targets[i].Balance.Balance, wallet.Balance)
		require.Equal(t, targets[i].Balance.QuotaRemaining, wallet.QuotaRemaining)
	}
}

func TestUpstreamSupplierWalletSelectsLatestAmountWithoutSummingOrMixingObservations(t *testing.T) {
	old := time.Now().Add(-time.Hour)
	newer := old.Add(time.Minute)
	attempt := newer.Add(time.Minute)
	first := &UpstreamTarget{ID: 1, WalletRef: "default", Balance: &UpstreamBalanceSnapshot{TargetID: 1, Kind: "wallet", Balance: financeFloat(10), Currency: "USD", Status: "error", Error: "upstream_http_401", SyncedAt: &old, LastAttemptAt: &attempt}}
	second := &UpstreamTarget{ID: 2, WalletRef: "default", Balance: &UpstreamBalanceSnapshot{TargetID: 2, Kind: "wallet", Balance: financeFloat(8), Currency: "USD", Status: "ok", SyncedAt: &newer, LastAttemptAt: &newer}}
	for _, targets := range [][]*UpstreamTarget{{first, second}, {second, first}} {
		wallets := upstreamSupplierWallets(targets)
		require.Len(t, wallets, 1)
		require.Equal(t, 8.0, *wallets[0].Balance)
		require.Equal(t, "ok", wallets[0].Status)
		require.Equal(t, int64(2), wallets[0].TargetID)
		require.Equal(t, newer, *wallets[0].LastAttemptAt)
	}
	// If every key fails, keep the last known real amount and its honest status.
	second.Balance.Status, second.Balance.Error = "error", "upstream_http_503"
	wallets := upstreamSupplierWallets([]*UpstreamTarget{first, second})
	require.Equal(t, 8.0, *wallets[0].Balance)
	require.Equal(t, "error", wallets[0].Status)
	require.Equal(t, "upstream_http_503", wallets[0].Error)
}

func TestUpstreamSupplierWalletWithoutSuccessPreservesOneUnknownPlaceholder(t *testing.T) {
	old, recent := time.Now().Add(-time.Minute), time.Now()
	first := &UpstreamTarget{ID: 1, WalletRef: "default", Balance: &UpstreamBalanceSnapshot{TargetID: 1, Kind: "unknown", Status: "error", Error: "upstream_http_401", LastAttemptAt: &old}}
	second := &UpstreamTarget{ID: 2, WalletRef: "default", Balance: &UpstreamBalanceSnapshot{TargetID: 2, Kind: "unsupported", Status: "unsupported", Error: "usage_endpoint_unsupported", LastAttemptAt: &recent}}
	wallets := upstreamSupplierWallets([]*UpstreamTarget{first, second})
	require.Len(t, wallets, 1)
	require.Equal(t, int64(2), wallets[0].TargetID)
	require.Nil(t, wallets[0].Balance)
	require.Equal(t, "unsupported", wallets[0].Kind)
	require.Equal(t, "usage_endpoint_unsupported", wallets[0].Error)
	require.Equal(t, "upstream_http_401", first.Balance.Error)
}

type upstreamWalletOverviewRepo struct {
	UpstreamCenterRepository
	suppliers []*UpstreamSupplier
	targets   []*UpstreamTarget
}

func (r *upstreamWalletOverviewRepo) ListSuppliers(context.Context) ([]*UpstreamSupplier, error) {
	return r.suppliers, nil
}
func (r *upstreamWalletOverviewRepo) ListTargets(context.Context) ([]*UpstreamTarget, error) {
	return r.targets, nil
}
func (*upstreamWalletOverviewRepo) PopulateStatistics(context.Context, []*UpstreamTarget, time.Time) error {
	return nil
}

func TestUpstreamWalletOverviewScopesDefaultWalletToSupplier(t *testing.T) {
	one, two := int64(1), int64(2)
	repo := &upstreamWalletOverviewRepo{suppliers: []*UpstreamSupplier{{ID: one}, {ID: two}}, targets: []*UpstreamTarget{
		{ID: 1, SupplierID: &one, WalletRef: "default", Balance: &UpstreamBalanceSnapshot{TargetID: 1, Kind: "wallet", Status: "ok", Balance: financeFloat(1), Currency: "USD"}},
		{ID: 2, SupplierID: &one, WalletRef: "default", Balance: &UpstreamBalanceSnapshot{TargetID: 2, Kind: "unknown", Status: "error", Error: "upstream_http_401"}},
		{ID: 3, SupplierID: &two, WalletRef: "default", Balance: &UpstreamBalanceSnapshot{TargetID: 3, Kind: "wallet", Status: "ok", Balance: financeFloat(3), Currency: "USD"}},
	}}
	svc := NewUpstreamCenterService(repo, upstreamTestEncryptor{}, nil, nil)
	overview, err := svc.Overview(context.Background(), "24h")
	require.NoError(t, err)
	require.Len(t, overview.Suppliers[0].Wallets, 1)
	require.Len(t, overview.Suppliers[1].Wallets, 1)
	require.Equal(t, 1.0, *overview.Suppliers[0].Wallets[0].Balance)
	require.Equal(t, 3.0, *overview.Suppliers[1].Wallets[0].Balance)
	require.Equal(t, "upstream_http_401", overview.Suppliers[0].Targets[1].Balance.Error)
}
