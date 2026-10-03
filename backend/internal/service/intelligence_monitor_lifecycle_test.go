package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

type intelligenceLifecycleTestRepo struct {
	IntelligenceMonitorRepository
	live                    []int64
	deletedPlan, deletedRun int64
	enabled                 bool
	calls                   int
	plan                    *IntelligenceMonitorPlan
	archived                bool
}

func (r *intelligenceLifecycleTestRepo) GetPlan(context.Context, int64) (*IntelligenceMonitorPlan, error) {
	return r.plan, nil
}

func (r *intelligenceLifecycleTestRepo) ArchivePlan(context.Context, int64) error {
	r.archived = true
	return nil
}

func (r *intelligenceLifecycleTestRepo) DeletePelicanRun(_ context.Context, id int64) error {
	r.deletedRun = id
	return nil
}
func (r *intelligenceLifecycleTestRepo) DeleteOAuthPlanPermanently(_ context.Context, id int64) error {
	r.deletedPlan = id
	r.live = nil
	return nil
}
func (r *intelligenceLifecycleTestRepo) ScheduleStatus(context.Context) (*IntelligenceScheduleStatus, error) {
	return &IntelligenceScheduleStatus{Total: 2}, nil
}
func (r *intelligenceLifecycleTestRepo) SetPlansEnabled(_ context.Context, enabled bool) (*IntelligenceScheduleUpdate, error) {
	r.enabled = enabled
	return &IntelligenceScheduleUpdate{Updated: 2}, nil
}
func (r *intelligenceLifecycleTestRepo) LiveOAuthExecutionIDs(context.Context, []int64) ([]int64, error) {
	r.calls++
	return r.live, nil
}

func TestIntelligenceLifecyclePauseKeepsActiveExecutionAndDeleteCancels(t *testing.T) {
	repo := &intelligenceLifecycleTestRepo{live: []int64{12}}
	svc := NewIntelligenceMonitorService(repo, nil, nil, nil, nil, nil, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	unregister := svc.registerOAuthExecution(ctx, &IntelligenceMonitorRun{ID: 12, SourceType: "openai_oauth"}, cancel)
	defer unregister()
	require.NoError(t, ctx.Err())
	_, err := svc.SetPlansEnabled(context.Background(), false)
	require.NoError(t, err)
	svc.cancelDeletedOAuthExecutions(context.Background())
	require.NoError(t, ctx.Err(), "schedule stop never cancels the current request")
	require.NoError(t, svc.DeleteOAuthPlanPermanently(context.Background(), 9))
	require.ErrorIs(t, ctx.Err(), context.Canceled)
	require.Equal(t, int64(9), repo.deletedPlan)
	unregister()
	require.Empty(t, svc.oauthExecutions)
}

func TestIntelligenceLifecycleDeletedBeforeExecutionIsNeverForwarded(t *testing.T) {
	repo := &intelligenceLifecycleTestRepo{}
	svc := NewIntelligenceMonitorService(repo, nil, nil, nil, nil, nil, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	unregister := svc.registerOAuthExecution(ctx, &IntelligenceMonitorRun{ID: 12, SourceType: "openai_oauth"}, cancel)
	defer unregister()
	require.ErrorIs(t, ctx.Err(), context.Canceled)
	require.ErrorIs(t, svc.DeletePelicanRun(context.Background(), 0), ErrIntelligenceInvalid)
	require.ErrorIs(t, svc.DeleteOAuthPlanPermanently(context.Background(), -1), ErrIntelligenceInvalid)
	require.Zero(t, repo.deletedPlan)
	require.Zero(t, repo.deletedRun)
}

func TestIntelligenceLifecycleLegacyOAuthDeleteIsPermanent(t *testing.T) {
	repo := &intelligenceLifecycleTestRepo{plan: &IntelligenceMonitorPlan{ID: 7, SourceType: "openai_oauth"}}
	svc := NewIntelligenceMonitorService(repo, nil, nil, nil, nil, nil, nil)
	require.NoError(t, svc.DeletePlan(context.Background(), 7))
	require.Equal(t, int64(7), repo.deletedPlan)
	require.False(t, repo.archived, "legacy DELETE must not archive an OAuth monitor")
}
