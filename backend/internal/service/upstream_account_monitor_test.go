//go:build unit

package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
	"github.com/stretchr/testify/require"
)

type accountMonitorTestRepo struct {
	upstreamTestRepo
	identity      UpstreamAccountMonitorIdentity
	ensureCalls   int
	statisticsIDs []int64
	findErr       error
}

func (r *accountMonitorTestRepo) FindAccountMonitor(_ context.Context, identity UpstreamAccountMonitorIdentity) (*UpstreamTarget, error) {
	r.identity = identity
	return r.target, r.findErr
}
func (r *accountMonitorTestRepo) EnsureAccountMonitor(_ context.Context, identity UpstreamAccountMonitorIdentity, candidate *UpstreamTarget) (*UpstreamTarget, error) {
	r.ensureCalls++
	r.identity = identity
	if r.target == nil {
		supplierID := int64(15)
		candidate.ID, candidate.SupplierID = 27, &supplierID
		r.target, r.saved = candidate, candidate
	}
	return r.target, nil
}
func (r *accountMonitorTestRepo) PopulateStatistics(_ context.Context, targets []*UpstreamTarget, _ time.Time) error {
	for _, target := range targets {
		r.statisticsIDs = append(r.statisticsIDs, target.ID)
		target.Statistics = []*UpstreamModelStatistics{{Model: "test", Status: "operational", Timeline: []*UpstreamHistoryRecord{{ID: 99}}}}
	}
	return nil
}

func monitorTestAccount() *Account {
	return &Account{ID: 6, Name: "Supplier key", Type: AccountTypeAPIKey, Platform: MonitorProviderOpenAI, Credentials: map[string]any{"api_key": "private-account-key", "base_url": "https://8.8.8.8/v1/"}}
}

type accountMonitorFinanceRepo struct {
	UpstreamFinanceRepository
	readIDs        []int64
	summaryQueries []UpstreamFinanceQuery
	summary        *UpstreamFinanceSummary
	summaryErr     error
}

func (r *accountMonitorFinanceRepo) Summary(_ context.Context, q UpstreamFinanceQuery) (*UpstreamFinanceSummary, error) {
	r.summaryQueries = append(r.summaryQueries, q)
	if r.summaryErr != nil {
		return nil, r.summaryErr
	}
	if r.summary != nil {
		return r.summary, nil
	}
	zero := float64(0)
	return &UpstreamFinanceSummary{MonitorCost: &zero, Profit: &zero, Currency: "USD", From: q.From, To: q.To}, nil
}

func (r *accountMonitorFinanceRepo) GetTarget(_ context.Context, id int64) (*UpstreamFinanceTarget, error) {
	return &UpstreamFinanceTarget{ID: id, Provider: "openai", Endpoint: "https://8.8.8.8", APIKeyEncrypted: "encrypted:key", WalletRef: "default"}, nil
}
func (r *accountMonitorFinanceRepo) LatestBalance(_ context.Context, id int64, _ string) (*UpstreamBalanceSnapshot, error) {
	r.readIDs = append(r.readIDs, id)
	balance, todayUsed, rate := 8.5, 2.75, 0.23
	return &UpstreamBalanceSnapshot{TargetID: id, Balance: &balance, TodayUsed: &todayUsed, Billing: &UpstreamRemoteBillingSnapshot{GroupRateMultiplier: &rate, Status: "ok"}}, nil
}

func TestUpstreamAccountMonitorReadsCachedFinanceWithoutRemoteRequests(t *testing.T) {
	repo := &accountMonitorTestRepo{}
	repo.target = validUpstreamTestTarget()
	financeRepo := &accountMonitorFinanceRepo{}
	finance := NewUpstreamFinanceService(financeRepo, upstreamTestEncryptor{}, nil, nil, nil)
	finance.client.Transport = financeRoundTrip(func(*http.Request) (*http.Response, error) {
		t.Fatal("opening the account dialog must not query an upstream wallet")
		return nil, nil
	})
	svc := NewUpstreamCenterService(repo, upstreamTestEncryptor{}, upstreamTestAccounts{account: monitorTestAccount()}, finance)
	svc.checkModel = func(context.Context, string, string, string, string, *CheckOptions) *CheckResult {
		t.Fatal("opening the account dialog must not run an availability check")
		return nil
	}
	view, err := svc.AccountMonitor(context.Background(), 6, false)
	require.NoError(t, err)
	require.Equal(t, []int64{7}, financeRepo.readIDs)
	require.InDelta(t, 8.5, *view.Target.Balance.Balance, 0.0001)
	require.InDelta(t, 2.75, *view.Target.Balance.TodayUsed, 0.0001)
	require.InDelta(t, 0.23, *view.Target.Balance.Billing.GroupRateMultiplier, 0.0001)
	require.NotNil(t, view.Target.Finance)
	require.Len(t, financeRepo.summaryQueries, 1)
	require.Zero(t, repo.ensureCalls)
}

func TestUpstreamAccountMonitorFinanceUsesCurrentGroupAndSiteDay(t *testing.T) {
	supplierID := int64(15)
	for _, scope := range []struct {
		name       string
		supplierID *int64
	}{
		{name: "supplier group", supplierID: &supplierID},
		{name: "independent monitor"},
	} {
		t.Run(scope.name, func(t *testing.T) {
			repo := &accountMonitorTestRepo{}
			repo.target = validUpstreamTestTarget()
			repo.target.SupplierID = scope.supplierID
			profit, monitorCost := 6.5, 0.5
			financeRepo := &accountMonitorFinanceRepo{summary: &UpstreamFinanceSummary{Revenue: 12, BusinessCost: 5, MonitorCost: &monitorCost, Profit: &profit}}
			finance := NewUpstreamFinanceService(financeRepo, upstreamTestEncryptor{}, nil, nil, nil)
			svc := NewUpstreamCenterService(repo, upstreamTestEncryptor{}, upstreamTestAccounts{account: monitorTestAccount()}, finance)
			dayBefore := timezone.Today()
			view, err := svc.AccountMonitor(context.Background(), 6, false)
			dayAfter := timezone.Today()
			require.NoError(t, err)
			require.Len(t, financeRepo.summaryQueries, 1, "only the selected group is aggregated")
			query := financeRepo.summaryQueries[0]
			require.Equal(t, scope.supplierID, query.SupplierID, "moving a group must not include a previous supplier's ledger")
			require.NotNil(t, query.TargetID, "a nil target would aggregate all groups")
			require.Equal(t, int64(7), *query.TargetID, "use the resolved target ID, not the account ID")
			require.True(t, query.From.Equal(dayBefore) || query.From.Equal(dayAfter))
			require.Equal(t, timezone.Location(), query.From.Location())
			require.Equal(t, query.From.AddDate(0, 0, 1), query.To)
			require.Same(t, financeRepo.summary, view.Target.Finance, "reuse the authoritative ledger's amounts and nullable costs")
			require.InDelta(t, 12, view.Target.Finance.Revenue, 0.0001)
			require.InDelta(t, 6.5, *view.Target.Finance.Profit, 0.0001)
			require.InDelta(t, 2.75, *view.Target.Balance.TodayUsed, 0.0001, "remote usage remains independent from local business cost")
			if scope.supplierID != nil {
				require.Same(t, view.Target.Finance, view.Supplier.Targets[0].Finance)
			}
		})
	}
}

func TestUpstreamAccountMonitorFinancePreservesUnknownProfit(t *testing.T) {
	repo := &accountMonitorTestRepo{}
	repo.target = validUpstreamTestTarget()
	financeRepo := &accountMonitorFinanceRepo{summary: &UpstreamFinanceSummary{Revenue: 12, BusinessCost: 5, UnpricedMonitorCount: 1}}
	finance := NewUpstreamFinanceService(financeRepo, upstreamTestEncryptor{}, nil, nil, nil)
	svc := NewUpstreamCenterService(repo, upstreamTestEncryptor{}, upstreamTestAccounts{account: monitorTestAccount()}, finance)
	view, err := svc.AccountMonitor(context.Background(), 6, false)
	require.NoError(t, err)
	require.Nil(t, view.Target.Finance.Profit, "unknown monitoring cost must not turn into an apparent profit")
	require.Nil(t, view.Target.Finance.MonitorCost)
	require.Equal(t, int64(1), view.Target.Finance.UnpricedMonitorCount)
	encoded, err := json.Marshal(view)
	require.NoError(t, err)
	require.Contains(t, string(encoded), `"profit":null`)
}

func TestUpstreamAccountMonitorFinanceSkipsUnresolvedTargetAndPropagatesFailure(t *testing.T) {
	repo := &accountMonitorTestRepo{}
	financeRepo := &accountMonitorFinanceRepo{summaryErr: errors.New("ledger unavailable")}
	finance := NewUpstreamFinanceService(financeRepo, upstreamTestEncryptor{}, nil, nil, nil)
	svc := NewUpstreamCenterService(repo, upstreamTestEncryptor{}, upstreamTestAccounts{account: monitorTestAccount()}, finance)
	view, err := svc.AccountMonitor(context.Background(), 6, false)
	require.NoError(t, err)
	require.Nil(t, view.Target)
	require.Empty(t, financeRepo.summaryQueries, "an unlinked account must not load global finance")
	require.Empty(t, financeRepo.readIDs)

	repo.target = validUpstreamTestTarget()
	view, err = svc.AccountMonitor(context.Background(), 6, false)
	require.ErrorIs(t, err, financeRepo.summaryErr)
	require.Nil(t, view, "a failed finance read must not be presented as zero usage or profit")
	require.Empty(t, financeRepo.readIDs)
}

func TestUpstreamAccountMonitorReadOnlyAndScoped(t *testing.T) {
	repo := &accountMonitorTestRepo{}
	account := monitorTestAccount()
	svc := NewUpstreamCenterService(repo, upstreamTestEncryptor{}, upstreamTestAccounts{account: account}, nil)
	view, err := svc.AccountMonitor(context.Background(), 6, false)
	require.NoError(t, err)
	require.Equal(t, "Supplier key", view.AccountName)
	require.Equal(t, int64(6), view.AccountID)
	require.True(t, view.PelicanSupported)
	require.Nil(t, view.Target)
	require.Nil(t, view.Supplier)
	require.Zero(t, repo.ensureCalls)
	require.Empty(t, repo.statisticsIDs)
	require.Equal(t, "https://8.8.8.8/v1", repo.identity.Endpoint)
	require.Equal(t, "https://8.8.8.8/v1/", repo.identity.Credentials.BaseURL, "retain the unmodified snapshot for atomic account validation")

	supplierID := int64(15)
	repo.target = validUpstreamTestTarget()
	repo.target.SupplierID = &supplierID
	view, err = svc.AccountMonitor(context.Background(), 6, false)
	require.NoError(t, err)
	require.Equal(t, []int64{7}, repo.statisticsIDs)
	require.Same(t, view.Target, view.Supplier.Targets[0])
	require.Len(t, view.Supplier.Targets, 1)
	require.Equal(t, "operational", view.Target.Statistics[0].Status)
	require.Zero(t, repo.ensureCalls)
	encoded, err := json.Marshal(view)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "private-account-key")
	require.NotContains(t, string(encoded), "secret-testing-key")
	require.NotContains(t, string(encoded), "api_key_encrypted")
	require.NotContains(t, string(encoded), "Credentials")
}

func TestUpstreamAccountMonitorCreatesPausedSharedTarget(t *testing.T) {
	repo := &accountMonitorTestRepo{}
	svc := NewUpstreamCenterService(repo, upstreamTestEncryptor{}, upstreamTestAccounts{account: monitorTestAccount()}, nil)
	view, err := svc.AccountMonitor(context.Background(), 6, true)
	require.NoError(t, err)
	require.Equal(t, int64(27), view.Target.ID)
	require.False(t, view.Target.Enabled)
	require.Nil(t, view.Target.NextCheckAt)
	require.Equal(t, []int64{6}, view.Target.AccountIDs)
	require.Equal(t, "Supplier key", view.Target.Name)
	require.Equal(t, "encrypted:private-account-key", repo.saved.APIKeyEncrypted)
	require.Equal(t, repo.identity.Fingerprint, repo.saved.APIKeyFingerprint)
	require.Equal(t, "https://8.8.8.8/v1", view.Target.Endpoint)
	require.Equal(t, 1, repo.ensureCalls)

	view, err = svc.AccountMonitor(context.Background(), 6, true)
	require.NoError(t, err)
	require.Equal(t, int64(27), view.Target.ID, "the same identity remains one inventory target")
}

func TestUpstreamAccountMonitorIndependentAndOtherProviders(t *testing.T) {
	repo := &accountMonitorTestRepo{}
	repo.target = validUpstreamTestTarget()
	account := monitorTestAccount()
	account.Platform = MonitorProviderAnthropic
	svc := NewUpstreamCenterService(repo, upstreamTestEncryptor{}, upstreamTestAccounts{account: account}, nil)
	view, err := svc.AccountMonitor(context.Background(), 6, true)
	require.NoError(t, err)
	require.False(t, view.PelicanSupported)
	require.Nil(t, view.Supplier)
	require.Equal(t, int64(7), view.Target.ID)
	require.True(t, view.Target.Enabled, "reuse never resets an existing monitor's schedule")
	require.Empty(t, view.Target.AccountIDs)
	for provider, model := range map[string]string{MonitorProviderAnthropic: "claude-haiku-4-5", MonitorProviderGemini: "gemini-2.5-flash"} {
		account.Platform = provider
		repo.target = nil
		view, err = svc.AccountMonitor(context.Background(), 6, true)
		require.NoError(t, err)
		require.False(t, view.PelicanSupported)
		require.False(t, view.Target.Enabled)
		require.Equal(t, []string{model}, view.Target.Models)
	}
}

func TestUpstreamAccountMonitorRejectsInvalidSourcesAndAmbiguity(t *testing.T) {
	for _, change := range []func(*Account){
		func(a *Account) { a.Type = AccountTypeOAuth },
		func(a *Account) { parent := int64(1); a.ParentAccountID = &parent },
		func(a *Account) { a.Extra = map[string]any{"synthetic_ui_test": true} },
		func(a *Account) { a.Platform = "unsupported" },
		func(a *Account) { a.Credentials["api_key"] = "" },
	} {
		account := monitorTestAccount()
		change(account)
		repo := &accountMonitorTestRepo{}
		svc := NewUpstreamCenterService(repo, upstreamTestEncryptor{}, upstreamTestAccounts{account: account}, nil)
		for _, ensure := range []bool{false, true} {
			_, err := svc.AccountMonitor(context.Background(), 6, ensure)
			require.ErrorIs(t, err, ErrUpstreamInvalid)
		}
		require.Zero(t, repo.ensureCalls)
	}
	repo := &accountMonitorTestRepo{findErr: ErrUpstreamAccountMonitorAmbiguous}
	svc := NewUpstreamCenterService(repo, upstreamTestEncryptor{}, upstreamTestAccounts{account: monitorTestAccount()}, nil)
	_, err := svc.AccountMonitor(context.Background(), 6, true)
	require.ErrorIs(t, err, ErrUpstreamAccountMonitorAmbiguous)
	require.Zero(t, repo.ensureCalls)
}
