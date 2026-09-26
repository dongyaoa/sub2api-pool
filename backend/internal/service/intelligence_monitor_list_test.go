//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

type intelligenceBatchedListRepository struct {
	intelligenceTestRepository
	loads int
}

func (r *intelligenceBatchedListRepository) ListPlans(context.Context) ([]*IntelligenceMonitorPlan, error) {
	return []*IntelligenceMonitorPlan{
		{ID: 41, Name: "stored OAuth name", SourceType: "openai_oauth", AccountID: listPtrInt64(7)},
		{ID: 42, Name: "external", SourceType: "external"},
	}, nil
}

func (r *intelligenceBatchedListRepository) LoadPlanListData(context.Context, []int64) (*IntelligenceMonitorPlanListData, error) {
	r.loads++
	return &IntelligenceMonitorPlanListData{
		Runs: map[int64][]*IntelligenceMonitorRun{
			41: {{ID: 410, PlanID: 41, Status: "running", RateSnapshot: &UpstreamRemoteBillingSnapshot{EffectiveRateMultiplier: listPtrFloat64(2)}}},
			42: {{ID: 420, PlanID: 42, Status: "succeeded"}},
		},
		SourceNames: map[int64]string{41: "Current OAuth account"},
	}, nil
}

func listPtrInt64(v int64) *int64 { return &v }

func listPtrFloat64(v float64) *float64 { return &v }

func TestIntelligenceListPlansUsesOneBatchedReadAndPreservesLiveAndOAuthSemantics(t *testing.T) {
	repo := &intelligenceBatchedListRepository{}
	svc := NewIntelligenceMonitorService(repo, upstreamTestEncryptor{}, nil, nil, nil, nil, nil)
	plans, err := svc.ListPlans(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, repo.loads)
	require.Equal(t, "Current OAuth account", plans[0].Name)
	require.Equal(t, "Current OAuth account", plans[0].SourceName)
	require.Equal(t, "running", plans[0].LatestRun.Status)
	require.Equal(t, 2.0, *plans[0].RateSnapshot.EffectiveRateMultiplier)
	require.Len(t, plans[0].RecentRuns, 0, "active generation is separate from artwork history")
	require.Equal(t, "succeeded", plans[1].LatestRun.Status)
	require.Len(t, plans[1].RecentRuns, 1)
}
