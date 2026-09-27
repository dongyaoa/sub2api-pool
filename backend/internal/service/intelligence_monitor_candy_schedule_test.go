//go:build unit

package service

import (
	"context"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIntelligenceCandyScheduleValidatesIndependentIntervals(t *testing.T) {
	for _, seconds := range []int{0, 179, 180, 181, 300, 600, 900, 901} {
		t.Run(strconv.Itoa(seconds), func(t *testing.T) {
			repo := &intelligenceTestRepository{}
			svc := NewIntelligenceMonitorService(repo, upstreamTestEncryptor{}, nil, nil, nil, nil, nil)
			name, endpoint, key := "Candy interval", "https://8.8.8.8", "fixture-secret"
			plan, err := svc.SavePlan(context.Background(), 0, 1, IntelligenceMonitorInput{Name: &name, Endpoint: &endpoint, APIKey: &key, CandyIntervalSeconds: &seconds})
			if seconds != 180 && seconds != 300 && seconds != 600 && seconds != 900 {
				require.ErrorIs(t, err, ErrIntelligenceInvalid)
				require.Nil(t, repo.saved)
				return
			}
			require.NoError(t, err)
			require.Equal(t, seconds, plan.CandyIntervalSeconds)
			require.Equal(t, 300, plan.IntervalSeconds, "candy interval does not modify artwork cadence")
		})
	}
	repo := &intelligenceTestRepository{}
	svc := NewIntelligenceMonitorService(repo, upstreamTestEncryptor{}, nil, nil, nil, nil, nil)
	name, endpoint, key := "Candy default", "https://8.8.8.8", "fixture-secret"
	plan, err := svc.SavePlan(context.Background(), 0, 1, IntelligenceMonitorInput{Name: &name, Endpoint: &endpoint, APIKey: &key})
	require.NoError(t, err)
	require.Equal(t, 180, plan.CandyIntervalSeconds)
	plan.ID, plan.CandyIntervalSeconds = 3, 600
	repo.plan = plan
	enabled := false
	paused, err := svc.SavePlan(context.Background(), 3, 1, IntelligenceMonitorInput{Enabled: &enabled})
	require.NoError(t, err)
	require.Equal(t, 600, paused.CandyIntervalSeconds, "pausing keeps the configured cadence")
	require.True(t, paused.AllowWhileBusy)
	seconds := 900
	require.False(t, intelligenceEnabledOnly(IntelligenceMonitorInput{Enabled: &enabled, CandyIntervalSeconds: &seconds}), "editing candy cadence cannot bypass busy validation")
}

type intelligenceIndependentScheduleRepository struct {
	intelligenceTestRepository
	artworkDue []int64
	candyDue   []int64
	queuedRuns []*IntelligenceMonitorRun
	scheduled  []bool
	claims     int
}

func (r *intelligenceIndependentScheduleRepository) ExpireRuns(context.Context) error { return nil }
func (r *intelligenceIndependentScheduleRepository) DuePlanIDs(context.Context, int) ([]int64, error) {
	return r.artworkDue, nil
}
func (r *intelligenceIndependentScheduleRepository) DueCandyPlanIDs(context.Context, int) ([]int64, error) {
	return r.candyDue, nil
}
func (r *intelligenceIndependentScheduleRepository) Enqueue(_ context.Context, run *IntelligenceMonitorRun, scheduled bool) error {
	run.ID, run.Status = int64(len(r.queuedRuns)+1), "pending"
	r.queuedRuns = append(r.queuedRuns, run)
	r.scheduled = append(r.scheduled, scheduled)
	return nil
}
func (r *intelligenceIndependentScheduleRepository) ClaimNext(context.Context, string) (*IntelligenceMonitorRun, error) {
	r.claims++
	return nil, nil // These tests never execute a model request.
}

func TestIntelligenceCandySchedulerUsesIndependentDueQueries(t *testing.T) {
	for _, tc := range []struct {
		name       string
		artworkDue []int64
		candyDue   []int64
		kinds      []string
	}{
		{"only artwork", []int64{3}, nil, []string{IntelligenceMonitorTestPelican}},
		{"only candy", nil, []int64{3}, []string{IntelligenceMonitorTestCandy}},
		{"both due", []int64{3}, []int64{3}, []string{IntelligenceMonitorTestPelican, IntelligenceMonitorTestCandy}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &intelligenceIndependentScheduleRepository{
				intelligenceTestRepository: intelligenceTestRepository{plan: &IntelligenceMonitorPlan{ID: 3, Name: "Schedule", SourceType: "external", Enabled: true, CandyEnabled: true, APIKeyEncrypted: "encrypted:secret"}},
				artworkDue:                 tc.artworkDue, candyDue: tc.candyDue,
			}
			svc := NewIntelligenceMonitorService(repo, upstreamTestEncryptor{}, nil, nil, nil, nil, nil)
			svc.tick()
			require.Len(t, repo.queuedRuns, len(tc.kinds))
			for i, kind := range tc.kinds {
				require.Equal(t, kind, repo.queuedRuns[i].TestKind)
				require.Equal(t, "scheduled", repo.queuedRuns[i].Trigger)
				require.True(t, repo.scheduled[i])
				if kind == IntelligenceMonitorTestCandy {
					require.Equal(t, IntelligenceMonitorCandyPrompt, repo.queuedRuns[i].Prompt)
				}
			}
			require.Equal(t, 1, repo.claims)
		})
	}
}

func TestIntelligenceCandyScheduledEnqueueRespectsGlobalPauseAndOptIn(t *testing.T) {
	repo := &intelligenceIndependentScheduleRepository{intelligenceTestRepository: intelligenceTestRepository{plan: &IntelligenceMonitorPlan{ID: 3, SourceType: "external", CandyEnabled: true, Enabled: false}}}
	svc := NewIntelligenceMonitorService(repo, nil, nil, nil, nil, nil, nil)
	for _, kind := range []string{IntelligenceMonitorTestPelican, IntelligenceMonitorTestCandy} {
		_, err := svc.enqueueTest(context.Background(), 3, true, kind)
		require.ErrorIs(t, err, ErrIntelligenceNotFound)
	}
	require.Empty(t, repo.queuedRuns)
	_, err := svc.EnqueueCandy(context.Background(), 3)
	require.NoError(t, err, "manual candy remains available while automatic monitoring is paused")
	require.Len(t, repo.queuedRuns, 1)
	repo.plan.Enabled, repo.plan.CandyEnabled = true, false
	_, err = svc.enqueueTest(context.Background(), 3, true, IntelligenceMonitorTestCandy)
	require.ErrorIs(t, err, ErrIntelligenceInvalid)
	_, err = svc.enqueueTest(context.Background(), 3, true, "unknown")
	require.ErrorIs(t, err, ErrIntelligenceInvalid)
	require.Len(t, repo.queuedRuns, 1)
}
