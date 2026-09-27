//go:build unit

package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type intelligenceConcurrencyTestRepository struct {
	*intelligenceSchedulerRepository
	limits  *IntelligenceMonitorConcurrency
	saveErr error
}

func (r *intelligenceConcurrencyTestRepository) GetConcurrency(context.Context) (*IntelligenceMonitorConcurrencySettings, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	settings := &IntelligenceMonitorConcurrencySettings{Source: "deployment"}
	if r.limits != nil {
		settings.IntelligenceMonitorConcurrency, settings.Source = *r.limits, "database"
	}
	for _, run := range r.active {
		if run.TestKind == IntelligenceMonitorTestCandy {
			settings.CandyRunning++
		} else {
			settings.PelicanRunning++
		}
	}
	for _, run := range r.pending {
		if run.TestKind == IntelligenceMonitorTestCandy {
			settings.CandyPending++
		} else {
			settings.PelicanPending++
		}
	}
	return settings, nil
}

func (r *intelligenceConcurrencyTestRepository) SaveConcurrency(_ context.Context, limits IntelligenceMonitorConcurrency) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.saveErr != nil {
		return r.saveErr
	}
	r.limits = &limits
	return nil
}

func (r *intelligenceConcurrencyTestRepository) ClaimNextForKind(ctx context.Context, token, kind string, limit int) (*IntelligenceMonitorRun, error) {
	r.mu.Lock()
	if r.limits != nil {
		limit = r.limits.MaxConcurrency
		if kind == IntelligenceMonitorTestCandy {
			limit = r.limits.CandyMaxConcurrency
		}
	}
	r.mu.Unlock()
	return r.intelligenceSchedulerRepository.ClaimNextForKind(ctx, token, kind, limit)
}

func TestIntelligenceConcurrencySettingsFallbackAndPersistence(t *testing.T) {
	for _, tc := range []struct {
		name string
		cfg  *config.Config
		want IntelligenceMonitorConcurrency
	}{
		{"defaults", nil, IntelligenceMonitorConcurrency{8, 4}},
		{"deployment", &config.Config{IntelligenceMonitor: config.IntelligenceMonitorConfig{MaxConcurrency: 5, CandyMaxConcurrency: 2}}, IntelligenceMonitorConcurrency{5, 2}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, base, _ := newIntelligenceSchedulerFixture(t, tc.cfg)
			repo := &intelligenceConcurrencyTestRepository{intelligenceSchedulerRepository: base}
			svc.repo = repo
			settings, err := svc.GetConcurrency(context.Background())
			require.NoError(t, err)
			require.Equal(t, "deployment", settings.Source)
			require.Equal(t, tc.want, settings.IntelligenceMonitorConcurrency)
			settings, err = svc.UpdateConcurrency(context.Background(), IntelligenceMonitorConcurrency{16, 8})
			require.NoError(t, err)
			require.Equal(t, "database", settings.Source)
			require.Equal(t, IntelligenceMonitorConcurrency{16, 8}, settings.IntelligenceMonitorConcurrency)
			restarted := NewIntelligenceMonitorService(repo, nil, nil, nil, nil, nil, tc.cfg)
			defer restarted.Stop()
			settings, err = restarted.GetConcurrency(context.Background())
			require.NoError(t, err)
			require.Equal(t, IntelligenceMonitorConcurrency{16, 8}, settings.IntelligenceMonitorConcurrency)
		})
	}
}

func TestIntelligenceConcurrencyRejectsInvalidOrFailedSave(t *testing.T) {
	svc, base, _ := newIntelligenceSchedulerFixture(t, nil)
	repo := &intelligenceConcurrencyTestRepository{intelligenceSchedulerRepository: base}
	svc.repo = repo
	for _, limits := range []IntelligenceMonitorConcurrency{{0, 4}, {-1, 4}, {257, 4}, {8, 0}, {8, -1}, {8, 129}} {
		_, err := svc.UpdateConcurrency(context.Background(), limits)
		require.ErrorIs(t, err, ErrIntelligenceInvalid)
		require.Nil(t, repo.limits)
	}
	repo.saveErr = errors.New("storage unavailable")
	_, err := svc.UpdateConcurrency(context.Background(), IntelligenceMonitorConcurrency{16, 8})
	require.ErrorIs(t, err, repo.saveErr)
	require.Nil(t, repo.limits)
	require.Empty(t, svc.wake, "a failed write must not signal a settings change")
}

func TestIntelligenceConcurrencyChangesApplyWithoutRestartOrCancellation(t *testing.T) {
	cfg := &config.Config{IntelligenceMonitor: config.IntelligenceMonitorConfig{MaxConcurrency: 2, CandyMaxConcurrency: 1}}
	svc, base, transport := newIntelligenceSchedulerFixture(t, cfg)
	svc.repo = &intelligenceConcurrencyTestRepository{intelligenceSchedulerRepository: base}
	for id := int64(1); id <= 5; id++ {
		_, err := svc.Enqueue(context.Background(), id)
		require.NoError(t, err)
	}
	svc.Start()
	flights := []*intelligenceSchedulerRequest{
		awaitIntelligenceSchedulerRequest(t, transport, 2*time.Second),
		awaitIntelligenceSchedulerRequest(t, transport, 2*time.Second),
	}
	requireNoIntelligenceSchedulerRequest(t, transport)
	_, err := svc.UpdateConcurrency(context.Background(), IntelligenceMonitorConcurrency{4, 1})
	require.NoError(t, err)
	for i := 0; i < 2; i++ {
		flights = append(flights, awaitIntelligenceSchedulerRequest(t, transport, 500*time.Millisecond))
	}
	require.EqualValues(t, 4, transport.active.Load())
	requireNoIntelligenceSchedulerRequest(t, transport)
	settings, err := svc.UpdateConcurrency(context.Background(), IntelligenceMonitorConcurrency{1, 1})
	require.NoError(t, err)
	require.Equal(t, 4, settings.PelicanRunning, "lowering must preserve requests already in flight")
	require.Equal(t, 1, settings.PelicanPending)
	for _, flight := range flights[:3] {
		close(flight.release)
	}
	for i := 0; i < 3; i++ {
		select {
		case run := <-base.completed:
			require.Equal(t, "succeeded", run.Status)
		case <-time.After(time.Second):
			t.Fatal("completed requests were not persisted")
		}
	}
	require.EqualValues(t, 1, transport.active.Load())
	requireNoIntelligenceSchedulerRequest(t, transport)
	close(flights[3].release)
	awaitIntelligenceSchedulerRequest(t, transport, 500*time.Millisecond)
	require.EqualValues(t, 1, transport.active.Load(), "the waiting request starts only after occupancy drops below the new limit")
}
