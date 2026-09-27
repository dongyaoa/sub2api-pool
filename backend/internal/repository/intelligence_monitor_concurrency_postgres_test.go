package repository

import (
	"fmt"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestIntelligenceConcurrencyPostgresPersistsAndAppliesImmediately(t *testing.T) {
	db, ctx := intelligenceMonitorTestDB(t)
	repo := &intelligenceMonitorRepository{db: db}
	other := &intelligenceMonitorRepository{db: candyRepositoryConnection(t, ctx, db)}
	initial, err := repo.GetConcurrency(ctx)
	require.NoError(t, err)
	require.Equal(t, "deployment", initial.Source)
	require.Zero(t, initial.MaxConcurrency)
	for i := range 5 {
		plan := candyRepositoryPlan(t, ctx, repo, fmt.Sprintf("Live limit %d", i), false, true)
		require.NoError(t, repo.Enqueue(ctx, candyRepositoryRun(plan, "pelican"), false))
	}
	for i := range 3 {
		plan := candyRepositoryPlan(t, ctx, repo, fmt.Sprintf("Candy limit %d", i), false, true)
		require.NoError(t, repo.Enqueue(ctx, candyRepositoryRun(plan, "candy"), false))
	}
	limits := service.IntelligenceMonitorConcurrency{MaxConcurrency: 1, CandyMaxConcurrency: 1}
	require.NoError(t, repo.SaveConcurrency(ctx, limits))
	first, err := other.ClaimNextForKind(ctx, "first", "pelican", 8)
	require.NoError(t, err)
	require.NotNil(t, first)
	blocked, err := repo.ClaimNextForKind(ctx, "blocked", "pelican", 8)
	require.NoError(t, err)
	require.Nil(t, blocked, "stored limit overrides a higher deployment fallback on every instance")
	candy, err := other.ClaimNextForKind(ctx, "candy-first", "candy", 4)
	require.NoError(t, err)
	require.NotNil(t, candy)
	blocked, err = repo.ClaimNextForKind(ctx, "candy-blocked", "candy", 4)
	require.NoError(t, err)
	require.Nil(t, blocked)
	limits.MaxConcurrency = 3
	require.NoError(t, other.SaveConcurrency(ctx, limits))
	second, err := repo.ClaimNextForKind(ctx, "second", "pelican", 1)
	require.NoError(t, err)
	require.NotNil(t, second, "raising the database limit applies despite a lower deployment fallback")
	third, err := other.ClaimNextForKind(ctx, "third", "pelican", 1)
	require.NoError(t, err)
	require.NotNil(t, third)
	limits.MaxConcurrency = 1
	require.NoError(t, repo.SaveConcurrency(ctx, limits))
	state, err := other.GetConcurrency(ctx)
	require.NoError(t, err)
	require.Equal(t, "database", state.Source)
	require.Equal(t, limits, state.IntelligenceMonitorConcurrency)
	require.Equal(t, 3, state.PelicanRunning, "lowering preserves existing executions")
	require.Equal(t, 2, state.PelicanPending)
	require.Equal(t, 1, state.CandyRunning)
	require.Equal(t, 2, state.CandyPending)
	for _, run := range []*service.IntelligenceMonitorRun{first, second} {
		run.Status = "failed"
		require.NoError(t, repo.CompleteRun(ctx, run))
		blocked, err = other.ClaimNextForKind(ctx, "lowered", "pelican", 8)
		require.NoError(t, err)
		require.Nil(t, blocked, "new tasks wait until active work falls below the lowered limit")
	}
	third.Status = "failed"
	require.NoError(t, repo.CompleteRun(ctx, third))
	next, err := repo.ClaimNextForKind(ctx, "released", "pelican", 8)
	require.NoError(t, err)
	require.NotNil(t, next)
	_, err = db.ExecContext(ctx, `UPDATE intelligence_monitor_runs SET lease_until=NOW()-INTERVAL '1 minute' WHERE id=$1`, next.ID)
	require.NoError(t, err)
	state, err = repo.GetConcurrency(ctx)
	require.NoError(t, err)
	require.Zero(t, state.PelicanRunning, "expired leases are not counted as occupied slots")
}

func TestIntelligenceConcurrencyPostgresRejectsInvalidStoredOrSubmittedLimits(t *testing.T) {
	db, ctx := intelligenceMonitorTestDB(t)
	repo := &intelligenceMonitorRepository{db: db}
	valid := service.IntelligenceMonitorConcurrency{MaxConcurrency: 8, CandyMaxConcurrency: 4}
	require.NoError(t, repo.SaveConcurrency(ctx, valid))
	for _, limits := range []service.IntelligenceMonitorConcurrency{
		{MaxConcurrency: 0, CandyMaxConcurrency: 4}, {MaxConcurrency: 257, CandyMaxConcurrency: 4},
		{MaxConcurrency: 8, CandyMaxConcurrency: -1}, {MaxConcurrency: 8, CandyMaxConcurrency: 129},
	} {
		require.ErrorIs(t, repo.SaveConcurrency(ctx, limits), service.ErrIntelligenceInvalid)
	}
	state, err := repo.GetConcurrency(ctx)
	require.NoError(t, err)
	require.Equal(t, valid, state.IntelligenceMonitorConcurrency)
	for _, corrupt := range []string{`not-json`, `null`, `{}`, `{"max_concurrency":0,"candy_max_concurrency":4}`, `{"max_concurrency":8,"candy_max_concurrency":129}`} {
		_, err = db.ExecContext(ctx, `UPDATE settings SET value=$2 WHERE key=$1`, intelligenceConcurrencySettingKey, corrupt)
		require.NoError(t, err)
		_, err = repo.GetConcurrency(ctx)
		require.Error(t, err)
		for _, kind := range []string{"pelican", "candy"} {
			_, err = repo.ClaimNextForKind(ctx, "must-fail", kind, 8)
			require.Error(t, err, "corrupt settings must not silently disable or bypass the configured limit")
		}
	}
}

func TestIntelligenceConcurrencyPostgresConcurrentInstancesUseStoredLimit(t *testing.T) {
	db, ctx := intelligenceMonitorTestDB(t)
	repo := &intelligenceMonitorRepository{db: db}
	require.NoError(t, repo.SaveConcurrency(ctx, service.IntelligenceMonitorConcurrency{MaxConcurrency: 3, CandyMaxConcurrency: 2}))
	var contenders []*intelligenceMonitorRepository
	for i := range 8 {
		plan := candyRepositoryPlan(t, ctx, repo, fmt.Sprintf("Stored concurrency %d", i), false, false)
		require.NoError(t, repo.Enqueue(ctx, candyRepositoryRun(plan, "pelican"), false))
		contenders = append(contenders, &intelligenceMonitorRepository{db: candyRepositoryConnection(t, ctx, db)})
	}
	type result struct {
		run *service.IntelligenceMonitorRun
		err error
	}
	results := make(chan result, len(contenders))
	start := make(chan struct{})
	for i, contender := range contenders {
		go func() {
			<-start
			run, err := contender.ClaimNextForKind(ctx, fmt.Sprintf("stored-%d", i), "pelican", 8)
			results <- result{run, err}
		}()
	}
	close(start)
	seen := make(map[int64]bool)
	for range contenders {
		select {
		case result := <-results:
			require.NoError(t, result.err)
			if result.run != nil {
				require.False(t, seen[result.run.ID])
				seen[result.run.ID] = true
			}
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	require.Len(t, seen, 3)
}
