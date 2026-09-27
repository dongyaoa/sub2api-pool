//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

type intelligenceUpstreamListStub struct {
	IntelligenceMonitorRepository
	targetID int64
	planIDs  []int64
	empty    bool
}

func (r *intelligenceUpstreamListStub) ListPlansForUpstream(_ context.Context, targetID int64) ([]*IntelligenceMonitorPlan, error) {
	r.targetID = targetID
	if r.empty {
		return nil, nil
	}
	return []*IntelligenceMonitorPlan{{ID: 9, Name: "Selected group", SourceType: "upstream", UpstreamTargetID: &targetID}}, nil
}

func (r *intelligenceUpstreamListStub) LoadPlanListData(_ context.Context, ids []int64) (*IntelligenceMonitorPlanListData, error) {
	r.planIDs = ids
	return &IntelligenceMonitorPlanListData{Runs: map[int64][]*IntelligenceMonitorRun{9: {{ID: 90, PlanID: 9, Status: "running"}}}, SourceNames: map[int64]string{9: "Selected group"}}, nil
}

func TestIntelligenceUpstreamPlansOnlyLoadsSelectedPlanSummaries(t *testing.T) {
	repo := &intelligenceUpstreamListStub{}
	svc := NewIntelligenceMonitorService(repo, nil, nil, nil, nil, nil, nil)
	plans, err := svc.ListPlansForUpstream(context.Background(), 27)
	require.NoError(t, err)
	require.Equal(t, int64(27), repo.targetID)
	require.Equal(t, []int64{9}, repo.planIDs)
	require.Len(t, plans, 1)
	require.Equal(t, "running", plans[0].LatestRun.Status)
	repo.empty, repo.planIDs = true, nil
	plans, err = svc.ListPlansForUpstream(context.Background(), 28)
	require.NoError(t, err)
	require.NotNil(t, plans)
	require.Empty(t, plans)
	require.Nil(t, repo.planIDs, "empty filtered lists never fetch artwork summaries")
	repo.targetID = 0
	_, err = svc.ListPlansForUpstream(context.Background(), -1)
	require.ErrorIs(t, err, ErrIntelligenceInvalid)
	require.Zero(t, repo.targetID)
}

type intelligenceUpstreamConflictStub struct {
	intelligenceTestRepository
	writeCount int
}

func (r *intelligenceUpstreamConflictStub) SavePlan(context.Context, *IntelligenceMonitorPlan) error {
	r.writeCount++
	return ErrIntelligenceUpstreamPlanExists
}

func TestIntelligenceUpstreamPlansKeepSourceValidationAndConflict(t *testing.T) {
	repo := &intelligenceUpstreamConflictStub{}
	upstreams := &upstreamTestRepo{}
	svc := NewIntelligenceMonitorService(repo, nil, upstreams, nil, nil, nil, nil)
	name, source := "Selected group", "upstream"
	input := IntelligenceMonitorInput{Name: &name, SourceType: &source, UpstreamTargetID: []byte(`27`)}
	_, err := svc.SavePlan(context.Background(), 0, 1, input)
	require.ErrorIs(t, err, ErrUpstreamNotFound, "missing or archived target must still fail source lookup")
	require.Zero(t, repo.writeCount)
	upstreams.target = &UpstreamTarget{ID: 27, Name: name, Provider: "anthropic"}
	_, err = svc.SavePlan(context.Background(), 0, 1, input)
	require.ErrorIs(t, err, ErrIntelligenceInvalid)
	require.Zero(t, repo.writeCount)
	upstreams.target.Provider = MonitorProviderOpenAI
	_, err = svc.SavePlan(context.Background(), 0, 1, input)
	require.ErrorIs(t, err, ErrIntelligenceUpstreamPlanExists)
	require.Equal(t, 1, repo.writeCount)
}
