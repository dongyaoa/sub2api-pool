//go:build unit

package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type intelligenceAccountListStub struct {
	intelligenceOAuthStatusRepository
	accountID int64
	planIDs   []int64
	empty     bool
	err       error
}

func (r *intelligenceAccountListStub) ListPlansForAccount(_ context.Context, accountID int64) ([]*IntelligenceMonitorPlan, error) {
	r.accountID = accountID
	if r.empty || r.err != nil {
		return nil, r.err
	}
	return []*IntelligenceMonitorPlan{{ID: 9, Name: "Old account name", SourceType: "openai_oauth", AccountID: &accountID, Enabled: false, CandyEnabled: true}}, nil
}

func (r *intelligenceAccountListStub) LoadPlanListData(_ context.Context, ids []int64) (*IntelligenceMonitorPlanListData, error) {
	r.planIDs = ids
	return &IntelligenceMonitorPlanListData{
		Runs:        map[int64][]*IntelligenceMonitorRun{9: {{ID: 90, PlanID: 9, Status: "running"}, {ID: 89, PlanID: 9, Status: "succeeded"}}},
		CandyRuns:   map[int64][]*IntelligenceMonitorRun{9: {{ID: 88, PlanID: 9, Status: "succeeded"}}},
		SourceNames: map[int64]string{9: "Current account name"},
	}, nil
}

func TestIntelligenceAccountPlansReuseCurrentOAuthGalleryAndStatus(t *testing.T) {
	reset := time.Now().Add(time.Hour)
	repo := &intelligenceAccountListStub{intelligenceOAuthStatusRepository: intelligenceOAuthStatusRepository{accounts: map[int64]*Account{
		27: {ID: 27, Name: "Current account name", Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true,
			Extra:  map[string]any{"codex_7d_used_percent": 100, "codex_7d_reset_at": reset.Format(time.RFC3339)},
			Groups: []*Group{{ID: 3, Name: "Current group"}}},
	}}}
	svc := NewIntelligenceMonitorService(repo, nil, nil, nil, nil, nil, nil)
	plans, err := svc.ListPlansForAccount(context.Background(), 27)
	require.NoError(t, err)
	require.Equal(t, int64(27), repo.accountID)
	require.Equal(t, []int64{9}, repo.planIDs, "only this account's plans reach the batched gallery loader")
	require.Equal(t, []int64{27}, repo.loadIDs)
	require.Len(t, plans, 1)
	require.Equal(t, "Current account name", plans[0].Name)
	require.False(t, plans[0].Enabled, "manually paused plans remain visible")
	require.Equal(t, "running", plans[0].LatestRun.Status)
	require.Equal(t, int64(89), plans[0].RecentRuns[0].ID)
	require.Equal(t, int64(88), plans[0].CandyLatestRun.ID)
	require.Equal(t, "weekly_limited", plans[0].OAuthAccountStatus.Status)
	require.Equal(t, []IntelligenceOAuthAccountGroup{{ID: 3, Name: "Current group"}}, plans[0].OAuthAccountStatus.Groups)
	repo.empty, repo.planIDs, repo.loads = true, nil, 0
	plans, err = svc.ListPlansForAccount(context.Background(), 28)
	require.NoError(t, err)
	require.NotNil(t, plans)
	require.Empty(t, plans)
	require.Nil(t, repo.planIDs)
	require.Zero(t, repo.loads, "unmonitored accounts do not load statuses or artwork")
}

func TestIntelligenceAccountPlansValidateFilterAndPropagateFailure(t *testing.T) {
	repo := &intelligenceAccountListStub{}
	svc := NewIntelligenceMonitorService(repo, nil, nil, nil, nil, nil, nil)
	for _, id := range []int64{0, -1} {
		_, err := svc.ListPlansForAccount(context.Background(), id)
		require.ErrorIs(t, err, ErrIntelligenceInvalid)
	}
	require.Zero(t, repo.accountID)
	repo.err = errors.New("filtered query failed")
	_, err := svc.ListPlansForAccount(context.Background(), 27)
	require.ErrorIs(t, err, repo.err)
	require.Nil(t, repo.planIDs)
	svc.repo = &intelligenceTestRepository{}
	_, err = svc.ListPlansForAccount(context.Background(), 27)
	require.ErrorIs(t, err, ErrIntelligenceFilterUnavailable, "unsupported stores never fall back to loading all accounts' plans")
}
