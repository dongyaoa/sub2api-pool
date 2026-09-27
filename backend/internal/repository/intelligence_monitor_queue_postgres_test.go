package repository

import (
	"fmt"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestIntelligenceQueuePostgresIndependentPoolsAndFIFO(t *testing.T) {
	db, ctx := intelligenceMonitorTestDB(t)
	repo := &intelligenceMonitorRepository{db: db}
	var artwork []*service.IntelligenceMonitorRun
	var firstPlan *service.IntelligenceMonitorPlan
	for i := range 10 {
		plan := candyRepositoryPlan(t, ctx, repo, fmt.Sprintf("Artwork %d", i), false, true)
		if i == 0 {
			firstPlan = plan
		}
		run := candyRepositoryRun(plan, "pelican")
		run.TimeoutSeconds = 900
		require.NoError(t, repo.Enqueue(ctx, run, false))
		artwork = append(artwork, run)
	}
	// The oldest candy must wait for its own plan, without blocking other keys.
	companion := candyRepositoryRun(firstPlan, "candy")
	require.NoError(t, repo.Enqueue(ctx, companion, false))
	var candy []*service.IntelligenceMonitorRun
	for i := range 5 {
		plan := candyRepositoryPlan(t, ctx, repo, fmt.Sprintf("Candy %d", i), false, true)
		run := candyRepositoryRun(plan, "candy")
		require.NoError(t, repo.Enqueue(ctx, run, false))
		candy = append(candy, run)
	}
	// Equal timestamps exercise the id tie-breaker as well as insertion order.
	_, err := db.ExecContext(ctx, `UPDATE intelligence_monitor_runs SET created_at='2026-09-01T00:00:00Z'`)
	require.NoError(t, err)
	var runningArtwork, runningCandy []*service.IntelligenceMonitorRun
	for i := range 8 {
		run, err := repo.ClaimNextForKind(ctx, fmt.Sprintf("art-%d", i), "pelican", 8)
		require.NoError(t, err)
		require.NotNil(t, run)
		require.Equal(t, artwork[i].ID, run.ID)
		runningArtwork = append(runningArtwork, run)
	}
	run, err := repo.ClaimNextForKind(ctx, "art-full", "pelican", 8)
	require.NoError(t, err)
	require.Nil(t, run)
	for i := range 4 {
		run, err := repo.ClaimNextForKind(ctx, fmt.Sprintf("candy-%d", i), "candy", 4)
		require.NoError(t, err)
		require.NotNil(t, run, "eight long drawings must leave the independent candy pool available")
		require.Equal(t, candy[i].ID, run.ID, "skip only the candy sharing a running plan")
		runningCandy = append(runningCandy, run)
	}
	run, err = repo.ClaimNextForKind(ctx, "candy-full", "candy", 4)
	require.NoError(t, err)
	require.Nil(t, run)
	var active, duplicates int
	require.NoError(t, db.QueryRowContext(ctx, `SELECT COUNT(*) FROM intelligence_monitor_runs WHERE status='running'`).Scan(&active))
	require.Equal(t, 12, active)
	require.NoError(t, db.QueryRowContext(ctx, `SELECT COUNT(*) FROM (SELECT plan_id FROM intelligence_monitor_runs WHERE status='running' GROUP BY plan_id HAVING COUNT(*)>1) duplicate_plans`).Scan(&duplicates))
	require.Zero(t, duplicates)
	var leaseSeconds float64
	require.NoError(t, db.QueryRowContext(ctx, `SELECT EXTRACT(EPOCH FROM lease_until-started_at) FROM intelligence_monitor_runs WHERE id=$1`, runningArtwork[0].ID).Scan(&leaseSeconds))
	require.Equal(t, float64(900+service.IntelligenceMonitorLeaseGraceSeconds), leaseSeconds)
	// Completion releases capacity immediately; the next eligible oldest run wins.
	runningArtwork[0].Status = "failed"
	require.NoError(t, repo.CompleteRun(ctx, runningArtwork[0]))
	run, err = repo.ClaimNextForKind(ctx, "art-released", "pelican", 8)
	require.NoError(t, err)
	require.NotNil(t, run)
	require.Equal(t, artwork[8].ID, run.ID)
	runningCandy[0].Status = "failed"
	require.NoError(t, repo.CompleteRun(ctx, runningCandy[0]))
	run, err = repo.ClaimNextForKind(ctx, "companion-released", "candy", 4)
	require.NoError(t, err)
	require.NotNil(t, run)
	require.Equal(t, companion.ID, run.ID, "the previously blocked oldest candy resumes before newer candy")
	require.ErrorIs(t, repo.Enqueue(ctx, candyRepositoryRun(firstPlan, "candy"), false), service.ErrIntelligenceBusy)
}

func TestIntelligenceQueuePostgresConcurrentInstancesRespectPoolLimit(t *testing.T) {
	db, ctx := intelligenceMonitorTestDB(t)
	repo := &intelligenceMonitorRepository{db: db}
	var queued []*service.IntelligenceMonitorRun
	var contenders []*intelligenceMonitorRepository
	for i := range 12 {
		plan := candyRepositoryPlan(t, ctx, repo, fmt.Sprintf("Concurrent %d", i), false, false)
		run := candyRepositoryRun(plan, "pelican")
		require.NoError(t, repo.Enqueue(ctx, run, false))
		queued = append(queued, run)
		contenders = append(contenders, &intelligenceMonitorRepository{db: candyRepositoryConnection(t, ctx, db)})
	}
	lock, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer func() { _ = lock.Rollback() }()
	_, err = lock.ExecContext(ctx, `SELECT pg_advisory_xact_lock(245,1)`)
	require.NoError(t, err)
	type result struct {
		run *service.IntelligenceMonitorRun
		err error
	}
	results := make(chan result, len(contenders))
	started := make(chan struct{}, len(contenders))
	for i, contender := range contenders {
		go func() {
			started <- struct{}{}
			run, err := contender.ClaimNextForKind(ctx, fmt.Sprintf("instance-%d", i), "pelican", 8)
			results <- result{run, err}
		}()
	}
	for range contenders {
		<-started
	}
	select {
	case result := <-results:
		t.Fatalf("claim bypassed the global lock: run=%v error=%v", result.run, result.err)
	case <-time.After(50 * time.Millisecond):
	}
	require.NoError(t, lock.Commit())
	seen := make(map[int64]bool)
	for range contenders {
		select {
		case result := <-results:
			require.NoError(t, result.err)
			if result.run != nil {
				require.False(t, seen[result.run.ID], "the same run must not be claimed by two instances")
				seen[result.run.ID] = true
			}
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	require.Len(t, seen, 8, "the pool limit applies across independent database connections")
	for i, run := range queued {
		require.Equal(t, i < 8, seen[run.ID], "claims preserve FIFO despite racing instances")
	}
}

func TestIntelligenceQueuePostgresConcurrentKindsSerializeSamePlan(t *testing.T) {
	db, ctx := intelligenceMonitorTestDB(t)
	repo := &intelligenceMonitorRepository{db: db}
	plan := candyRepositoryPlan(t, ctx, repo, "Shared credential", false, true)
	for _, kind := range []string{"pelican", "candy"} {
		require.NoError(t, repo.Enqueue(ctx, candyRepositoryRun(plan, kind), false))
	}
	type result struct {
		run *service.IntelligenceMonitorRun
		err error
	}
	results := make(chan result, 2)
	start := make(chan struct{})
	for _, kind := range []string{"pelican", "candy"} {
		other := &intelligenceMonitorRepository{db: candyRepositoryConnection(t, ctx, db)}
		go func() {
			<-start
			run, err := other.ClaimNextForKind(ctx, "same-plan-"+kind, kind, 4)
			results <- result{run, err}
		}()
	}
	close(start)
	var claimed *service.IntelligenceMonitorRun
	for range 2 {
		select {
		case result := <-results:
			require.NoError(t, result.err)
			if result.run != nil {
				require.Nil(t, claimed, "independent pools still serialize a shared plan")
				claimed = result.run
			}
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	require.NotNil(t, claimed)
	otherKind := "candy"
	if claimed.TestKind == otherKind {
		otherKind = "pelican"
	}
	claimed.Status = "failed"
	require.NoError(t, repo.CompleteRun(ctx, claimed))
	next, err := repo.ClaimNextForKind(ctx, "same-plan-after-completion", otherKind, 4)
	require.NoError(t, err)
	require.NotNil(t, next)
	require.Equal(t, plan.ID, next.PlanID)
	require.Equal(t, otherKind, next.TestKind)
}
