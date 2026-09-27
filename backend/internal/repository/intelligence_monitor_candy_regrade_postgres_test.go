package repository

import (
	"context"
	"database/sql"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/Wei-Shaw/sub2api/migrations"
	"github.com/stretchr/testify/require"
)

func insertIntelligenceCandyRegradeFixture(t *testing.T, ctx context.Context, db *sql.DB, planID int64, kind, status, text, message string, version int) int64 {
	t.Helper()
	var id int64
	require.NoError(t, db.QueryRowContext(ctx, `INSERT INTO intelligence_monitor_runs(plan_id,plan_name,status,trigger,model,reasoning_effort,prompt,source_type,source_name,source_endpoint,api_mode,timeout_seconds,test_kind,raw_text,error,correct,answer,candy_grade_version,http_status,finished_at,duration_ms) VALUES($1,'saved plan',$2,'manual','fixed model','high','saved question','external','saved source','https://example.test','responses',600,$3,$4,$5,FALSE,'',$6,200,NOW(),58100) RETURNING id`, planID, status, kind, text, message, version).Scan(&id))
	return id
}

func TestIntelligenceCandyRegradePostgresRepairsBoundedHistoryWithoutChangingRequests(t *testing.T) {
	db, ctx := intelligenceMonitorTestDB(t)
	repo := &intelligenceMonitorRepository{db: db}
	plan := candyRepositoryPlan(t, ctx, repo, "Regrade history", false, true)
	correctID := insertIntelligenceCandyRegradeFixture(t, ctx, db, plan.ID, "candy", "succeeded", `\boxed{21\text{个}}`, "", 0)
	wrongID := insertIntelligenceCandyRegradeFixture(t, ctx, db, plan.ID, "candy", "succeeded", "最终答案是29个。", "", 0)
	oversizedID := insertIntelligenceCandyRegradeFixture(t, ctx, db, plan.ID, "candy", "succeeded", strings.Repeat("x", service.IntelligenceMonitorCandyMaxGradeBytes+1)+"21", "", 0)
	ignoredIDs := []int64{
		insertIntelligenceCandyRegradeFixture(t, ctx, db, plan.ID, "pelican", "succeeded", "21", "", 0),
		insertIntelligenceCandyRegradeFixture(t, ctx, db, plan.ID, "candy", "failed", "21", "HTTP 502", 0),
		insertIntelligenceCandyRegradeFixture(t, ctx, db, plan.ID, "candy", "pending", "21", "", 0),
		insertIntelligenceCandyRegradeFixture(t, ctx, db, plan.ID, "candy", "succeeded", "21", "", service.IntelligenceMonitorCandyGradeVersion),
		insertIntelligenceCandyRegradeFixture(t, ctx, db, plan.ID, "candy", "succeeded", "21", "saved upstream error", 0),
	}
	runningPlan := candyRepositoryPlan(t, ctx, repo, "Running untouched", false, true)
	ignoredIDs = append(ignoredIDs, insertIntelligenceCandyRegradeFixture(t, ctx, db, runningPlan.ID, "candy", "running", "21", "", 0))
	before := map[int64]string{}
	allIDs := append([]int64{correctID, wrongID, oversizedID}, ignoredIDs...)
	for _, id := range allIDs {
		var snapshot string
		require.NoError(t, db.QueryRowContext(ctx, `SELECT (to_jsonb(r)-'correct'-'answer'-'candy_grade_version')::text FROM intelligence_monitor_runs r WHERE id=$1`, id).Scan(&snapshot))
		before[id] = snapshot
	}
	grade := func(raw string) (string, bool) {
		if strings.Contains(raw, "21") {
			return "21", true
		}
		if strings.Contains(raw, "29") {
			return "29", false
		}
		return "", false
	}
	for _, want := range []int{2, 1, 0} {
		count, err := repo.RegradeCandyRuns(ctx, 2, service.IntelligenceMonitorCandyGradeVersion, grade)
		require.NoError(t, err)
		require.Equal(t, want, count)
	}
	for _, tc := range []struct {
		id      int64
		answer  string
		correct bool
	}{{correctID, "21", true}, {wrongID, "29", false}, {oversizedID, "", false}} {
		var answer string
		var correct bool
		var version int
		require.NoError(t, db.QueryRowContext(ctx, `SELECT answer,correct,candy_grade_version FROM intelligence_monitor_runs WHERE id=$1`, tc.id).Scan(&answer, &correct, &version))
		require.Equal(t, tc.answer, answer)
		require.Equal(t, tc.correct, correct)
		require.Equal(t, service.IntelligenceMonitorCandyGradeVersion, version)
	}
	for _, id := range ignoredIDs {
		var answer string
		var correct bool
		require.NoError(t, db.QueryRowContext(ctx, `SELECT answer,correct FROM intelligence_monitor_runs WHERE id=$1`, id).Scan(&answer, &correct))
		require.Empty(t, answer)
		require.False(t, correct)
	}
	for _, id := range allIDs {
		var snapshot string
		require.NoError(t, db.QueryRowContext(ctx, `SELECT (to_jsonb(r)-'correct'-'answer'-'candy_grade_version')::text FROM intelligence_monitor_runs r WHERE id=$1`, id).Scan(&snapshot))
		require.Equal(t, before[id], snapshot, "grading must not change responses, timestamps, status or source snapshots")
	}
	// Applying the additive migration again must preserve current grades.
	migration, err := migrations.FS.ReadFile("260_intelligence_candy_grading_version.sql")
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, string(migration))
	require.NoError(t, err)
	count, err := repo.RegradeCandyRuns(ctx, 2, service.IntelligenceMonitorCandyGradeVersion, grade)
	require.NoError(t, err)
	require.Zero(t, count)
}

func TestIntelligenceCandyRegradePostgresCancellationRollsBackBatch(t *testing.T) {
	db, ctx := intelligenceMonitorTestDB(t)
	repo := &intelligenceMonitorRepository{db: db}
	plan := candyRepositoryPlan(t, ctx, repo, "Interrupted regrade", false, true)
	insertIntelligenceCandyRegradeFixture(t, ctx, db, plan.ID, "candy", "succeeded", "21", "", 0)
	// Cancellation may discard a driver connection; keep the fixture's schema
	// connection intact for verification and cleanup.
	cancelRepo := &intelligenceMonitorRepository{db: candyRepositoryConnection(t, ctx, db)}
	cancelCtx, cancel := context.WithCancel(ctx)
	count, err := cancelRepo.RegradeCandyRuns(cancelCtx, 2, service.IntelligenceMonitorCandyGradeVersion, func(string) (string, bool) {
		cancel()
		return "21", true
	})
	require.Error(t, err)
	require.Zero(t, count)
	count, err = repo.RegradeCandyRuns(ctx, 2, service.IntelligenceMonitorCandyGradeVersion, func(string) (string, bool) { return "21", true })
	require.NoError(t, err)
	require.Equal(t, 1, count, "cancelled repair remains eligible for retry")
}

func TestIntelligenceCandyRegradePostgresNewCompletionsUseCurrentVersion(t *testing.T) {
	db, ctx := intelligenceMonitorTestDB(t)
	repo := &intelligenceMonitorRepository{db: db}
	plan := candyRepositoryPlan(t, ctx, repo, "Current grading", false, true)
	require.NoError(t, repo.Enqueue(ctx, candyRepositoryRun(plan, "candy"), false))
	run, err := repo.ClaimNextForKind(ctx, "current-grade-worker", "candy", 4)
	require.NoError(t, err)
	require.NotNil(t, run)
	correct := true
	httpStatus := 200
	run.Status, run.HTTPStatus, run.RawText, run.Answer, run.Correct = "succeeded", &httpStatus, "21", "21", &correct
	require.NoError(t, repo.CompleteRun(ctx, run))
	count, err := repo.RegradeCandyRuns(ctx, 2, service.IntelligenceMonitorCandyGradeVersion, func(string) (string, bool) {
		t.Error("fresh completion should not need historical regrading")
		return "", false
	})
	require.NoError(t, err)
	require.Zero(t, count)
}

func TestIntelligenceCandyRegradePostgresMultipleInstancesSkipLockedRows(t *testing.T) {
	db, ctx := intelligenceMonitorTestDB(t)
	repo := &intelligenceMonitorRepository{db: db}
	other := &intelligenceMonitorRepository{db: candyRepositoryConnection(t, ctx, db)}
	plan := candyRepositoryPlan(t, ctx, repo, "Concurrent regrade", false, true)
	insertIntelligenceCandyRegradeFixture(t, ctx, db, plan.ID, "candy", "succeeded", "first response", "", 0)
	insertIntelligenceCandyRegradeFixture(t, ctx, db, plan.ID, "candy", "succeeded", "second response", "", 0)
	locked := make(chan string, 1)
	release := make(chan struct{})
	defer close(release)
	type result struct {
		count int
		err   error
	}
	done := make(chan result, 1)
	go func() {
		count, err := other.RegradeCandyRuns(ctx, 1, service.IntelligenceMonitorCandyGradeVersion, func(raw string) (string, bool) {
			locked <- raw
			select {
			case <-release:
			case <-ctx.Done():
			}
			return "21", true
		})
		done <- result{count, err}
	}()
	select {
	case raw := <-locked:
		require.Equal(t, "first response", raw)
	case <-ctx.Done():
		t.Fatal("first worker did not lock its batch")
	}
	count, err := repo.RegradeCandyRuns(ctx, 1, service.IntelligenceMonitorCandyGradeVersion, func(raw string) (string, bool) {
		require.Equal(t, "second response", raw, "another instance skips the first instance's locked record")
		return "29", false
	})
	require.NoError(t, err)
	require.Equal(t, 1, count)
	release <- struct{}{}
	select {
	case got := <-done:
		require.NoError(t, got.err)
		require.Equal(t, 1, got.count)
	case <-ctx.Done():
		t.Fatal("first worker did not complete")
	}
	count, err = repo.RegradeCandyRuns(ctx, 2, service.IntelligenceMonitorCandyGradeVersion, func(string) (string, bool) {
		t.Error("already repaired records must not be graded again")
		return "", false
	})
	require.NoError(t, err)
	require.Zero(t, count)
}
