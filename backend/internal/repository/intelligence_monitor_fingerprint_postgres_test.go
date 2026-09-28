package repository

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/Wei-Shaw/sub2api/migrations"
	"github.com/stretchr/testify/require"
)

func TestIntelligenceFingerprintPostgresProgressSummaryAndCompletion(t *testing.T) {
	db, ctx := intelligenceMonitorTestDB(t)
	repo := &intelligenceMonitorRepository{db: db}
	plan := candyRepositoryPlan(t, ctx, repo, "Fingerprint evidence", false, true)
	require.NoError(t, repo.Enqueue(ctx, candyRepositoryRun(plan, "candy"), false))
	run, err := repo.ClaimNextForKind(ctx, "fingerprint-worker", "candy", 4)
	require.NoError(t, err)
	require.NotNil(t, run)
	correct := true
	run.Correct, run.Answer = &correct, "21"
	run.Fingerprint = intelligenceRepositoryFingerprint()
	run.Fingerprint.Status, run.Fingerprint.Passed = "running", nil
	run.Fingerprint.Done, run.Fingerprint.Valid = 2, 2
	require.NoError(t, repo.SaveCandyProgress(ctx, run))
	detail, err := repo.GetRun(ctx, run.ID)
	require.NoError(t, err)
	require.Equal(t, "running", detail.Status)
	require.Equal(t, run.Correct, detail.Correct)
	require.Equal(t, run.Answer, detail.Answer)
	require.Equal(t, run.Fingerprint, detail.Fingerprint)
	require.Nil(t, detail.FinishedAt)
	assertSummary := func() {
		t.Helper()
		page, e := repo.ListRuns(ctx, service.IntelligenceMonitorRunQuery{PlanID: &plan.ID, TestKind: "candy", Page: 1, PageSize: 60})
		require.NoError(t, e)
		require.Len(t, page.Items, 1)
		summary := page.Items[0]
		require.Equal(t, run.Fingerprint.Summary(), summary.Fingerprint)
		require.Empty(t, summary.RawText)
		require.Empty(t, summary.RequestKeyEncrypted)
		require.Empty(t, summary.LeaseToken)
		require.Empty(t, summary.Fingerprint.Samples)
		require.Empty(t, summary.Fingerprint.Attribution.Comparisons[0].Cells)
		batched := map[int64][]*service.IntelligenceMonitorRun{}
		require.NoError(t, repo.loadPlanKindRuns(ctx, []int64{plan.ID}, "candy", 60, batched))
		require.Len(t, batched[plan.ID], 1)
		require.Equal(t, summary.Fingerprint, batched[plan.ID][0].Fingerprint)
	}
	assertSummary()
	stale := *run
	stale.LeaseToken = "different-worker"
	stale.Answer = "29"
	stale.Fingerprint = nil
	require.ErrorIs(t, repo.SaveCandyProgress(ctx, &stale), service.ErrIntelligenceNotFound)
	detail, err = repo.GetRun(ctx, run.ID)
	require.NoError(t, err)
	require.Equal(t, run.Fingerprint, detail.Fingerprint)
	require.Equal(t, "21", detail.Answer)
	status := 200
	run.Status, run.HTTPStatus, run.RawText = "succeeded", &status, `\boxed{21}`
	run.Fingerprint = intelligenceRepositoryFingerprint()
	require.NoError(t, repo.CompleteRun(ctx, run))
	detail, err = repo.GetRun(ctx, run.ID)
	require.NoError(t, err)
	require.Equal(t, "succeeded", detail.Status)
	require.Equal(t, run.RawText, detail.RawText)
	require.Equal(t, run.Fingerprint, detail.Fingerprint)
	require.Empty(t, detail.RequestKeyEncrypted)
	require.Empty(t, detail.LeaseToken)
	require.NotNil(t, detail.FinishedAt)
	assertSummary()
	var version int
	require.NoError(t, db.QueryRowContext(ctx, `SELECT candy_grade_version FROM intelligence_monitor_runs WHERE id=$1`, run.ID).Scan(&version))
	require.Equal(t, service.IntelligenceMonitorCandyGradeVersion, version)
	run.Answer, run.Fingerprint = "29", nil
	require.ErrorIs(t, repo.SaveCandyProgress(ctx, run), service.ErrIntelligenceNotFound, "terminal records cannot be overwritten by late progress")
	detail, err = repo.GetRun(ctx, run.ID)
	require.NoError(t, err)
	require.Equal(t, "21", detail.Answer)
	require.Equal(t, intelligenceRepositoryFingerprint(), detail.Fingerprint)
	// Older partial writers may have only a summary. Detail reads keep it.
	_, err = db.ExecContext(ctx, `UPDATE intelligence_monitor_runs SET fingerprint_detail='{}'::jsonb WHERE id=$1`, run.ID)
	require.NoError(t, err)
	detail, err = repo.GetRun(ctx, run.ID)
	require.NoError(t, err)
	require.Equal(t, intelligenceRepositoryFingerprint().Summary(), detail.Fingerprint)
}

func TestIntelligenceFingerprintPostgresMigrationKeepsLegacyUnknown(t *testing.T) {
	db, ctx := intelligenceMonitorTestDB(t)
	repo := &intelligenceMonitorRepository{db: db}
	plan := candyRepositoryPlan(t, ctx, repo, "Legacy candy", false, true)
	id := insertIntelligenceCandyRegradeFixture(t, ctx, db, plan.ID, "candy", "succeeded", "21", "", 2)
	_, err := db.ExecContext(ctx, `UPDATE intelligence_monitor_runs SET correct=TRUE,answer='21' WHERE id=$1`, id)
	require.NoError(t, err)
	var before string
	require.NoError(t, db.QueryRowContext(ctx, `SELECT (to_jsonb(r)-'fingerprint'-'fingerprint_detail')::text FROM intelligence_monitor_runs r WHERE id=$1`, id).Scan(&before))
	_, err = db.ExecContext(ctx, `ALTER TABLE intelligence_monitor_runs DROP COLUMN fingerprint,DROP COLUMN fingerprint_detail`)
	require.NoError(t, err)
	body, err := migrations.FS.ReadFile("261_intelligence_candy_fingerprint.sql")
	require.NoError(t, err)
	for range 2 {
		_, err = db.ExecContext(ctx, string(body))
		require.NoError(t, err, "fingerprint migration must be additive and idempotent")
	}
	var after string
	require.NoError(t, db.QueryRowContext(ctx, `SELECT (to_jsonb(r)-'fingerprint'-'fingerprint_detail')::text FROM intelligence_monitor_runs r WHERE id=$1`, id).Scan(&after))
	require.JSONEq(t, before, after)
	detail, err := repo.GetRun(ctx, id)
	require.NoError(t, err)
	require.NotNil(t, detail.Correct)
	require.True(t, *detail.Correct)
	require.Nil(t, detail.Fingerprint, "a legacy correct answer has no collected fingerprint and cannot imply a pass")
	page, err := repo.ListRuns(ctx, service.IntelligenceMonitorRunQuery{PlanID: &plan.ID, TestKind: "candy", Page: 1, PageSize: 60})
	require.NoError(t, err)
	require.Len(t, page.Items, 1)
	require.Nil(t, page.Items[0].Fingerprint)
}

func TestIntelligenceFingerprintPostgresExpiryFinalizesEvidenceAndKeepsCandyGrade(t *testing.T) {
	db, ctx := intelligenceMonitorTestDB(t)
	repo := &intelligenceMonitorRepository{db: db}
	runs := []*service.IntelligenceMonitorRun{}
	for _, phase := range []string{"collecting", "comparing", "legacy"} {
		plan := candyRepositoryPlan(t, ctx, repo, phase, true, true)
		require.NoError(t, repo.Enqueue(ctx, candyRepositoryRun(plan, "candy"), false))
		run, err := repo.ClaimNextForKind(ctx, "expiry-"+phase, "candy", 4)
		require.NoError(t, err)
		require.NotNil(t, run)
		if phase != "legacy" {
			correct := true
			run.Correct, run.Answer = &correct, "21"
			run.Fingerprint = intelligenceRepositoryFingerprint()
			run.Fingerprint.Status, run.Fingerprint.Passed = phase, nil
			run.Fingerprint.Done, run.Fingerprint.Valid = 2, 2
			require.NoError(t, repo.SaveCandyProgress(ctx, run))
		}
		require.ErrorIs(t, repo.ArchivePlan(ctx, plan.ID), service.ErrIntelligenceBusy, "archival cannot terminate or hide active evidence")
		if phase == "comparing" {
			paused, err := repo.GetPlan(ctx, plan.ID)
			require.NoError(t, err)
			paused.Enabled, paused.AllowWhileBusy = false, true
			require.NoError(t, repo.SavePlan(ctx, paused))
			active, err := repo.GetRun(ctx, run.ID)
			require.NoError(t, err)
			require.Equal(t, "running", active.Status, "pausing stops new schedules and does not cancel this round")
			require.Equal(t, "comparing", active.Fingerprint.Status)
		}
		_, err = db.ExecContext(ctx, `UPDATE intelligence_monitor_runs SET lease_until=NOW()-INTERVAL '1 minute' WHERE id=$1`, run.ID)
		require.NoError(t, err)
		runs = append(runs, run)
	}
	require.NoError(t, repo.ExpireRuns(ctx))
	for _, run := range runs {
		detail, err := repo.GetRun(ctx, run.ID)
		require.NoError(t, err)
		require.Equal(t, "failed", detail.Status)
		require.Equal(t, run.Correct, detail.Correct, "completed candy grading survives interrupted fingerprint collection")
		require.Equal(t, run.Answer, detail.Answer)
		require.Empty(t, detail.RequestKeyEncrypted)
		require.Empty(t, detail.LeaseToken)
		require.NotNil(t, detail.FinishedAt)
		page, err := repo.ListRuns(ctx, service.IntelligenceMonitorRunQuery{PlanID: &run.PlanID, TestKind: "candy", Page: 1, PageSize: 60})
		require.NoError(t, err)
		require.Len(t, page.Items, 1)
		if run.Fingerprint == nil {
			require.Nil(t, detail.Fingerprint)
			require.Nil(t, page.Items[0].Fingerprint, "legacy empty fingerprints remain unknown")
			continue
		}
		require.NotNil(t, detail.Fingerprint.Passed)
		require.False(t, *detail.Fingerprint.Passed)
		require.Equal(t, "timeout", detail.Fingerprint.Status)
		require.Equal(t, "Fingerprint collection interrupted or monitoring lease expired", detail.Fingerprint.Error)
		require.Equal(t, run.Fingerprint.Samples, detail.Fingerprint.Samples)
		require.Equal(t, run.Fingerprint.Done, detail.Fingerprint.Done)
		require.Equal(t, detail.Fingerprint.Summary(), page.Items[0].Fingerprint)
		require.ErrorIs(t, repo.SaveCandyProgress(ctx, run), service.ErrIntelligenceNotFound, "an expired worker cannot revive collecting state")
		if run.PlanName == "comparing" {
			paused, err := repo.GetPlan(ctx, run.PlanID)
			require.NoError(t, err)
			require.Nil(t, paused.CandyNextRunAt, "expiry must preserve the explicit pause")
		}
	}
}

func TestIntelligenceFingerprintPostgresInterruptedCompletionCannotStayCollecting(t *testing.T) {
	db, ctx := intelligenceMonitorTestDB(t)
	repo := &intelligenceMonitorRepository{db: db}
	for _, phase := range []string{"collecting", "comparing"} {
		plan := candyRepositoryPlan(t, ctx, repo, phase, false, true)
		require.NoError(t, repo.Enqueue(ctx, candyRepositoryRun(plan, "candy"), false))
		run, err := repo.ClaimNextForKind(ctx, "cancel-"+phase, "candy", 4)
		require.NoError(t, err)
		require.NotNil(t, run)
		correct := true
		run.Correct, run.Answer, run.RawText = &correct, "21", "21"
		run.Fingerprint = intelligenceRepositoryFingerprint()
		run.Fingerprint.Status, run.Fingerprint.Passed = phase, nil
		run.Fingerprint.Done, run.Fingerprint.Valid = 2, 2
		require.NoError(t, repo.SaveCandyProgress(ctx, run))
		run.Status, run.Error = "failed", "execution interrupted"
		require.NoError(t, repo.CompleteRun(ctx, run))
		detail, err := repo.GetRun(ctx, run.ID)
		require.NoError(t, err)
		require.Equal(t, "failed", detail.Status)
		require.Equal(t, "21", detail.Answer)
		require.Equal(t, run.Correct, detail.Correct)
		require.Equal(t, "failed", detail.Fingerprint.Status)
		require.NotNil(t, detail.Fingerprint.Passed)
		require.False(t, *detail.Fingerprint.Passed)
		require.Equal(t, "Fingerprint collection interrupted before comparison completed", detail.Fingerprint.Error)
		require.Equal(t, run.Fingerprint.Samples, detail.Fingerprint.Samples)
		require.Equal(t, phase, run.Fingerprint.Status, "repository serialization must not mutate the caller's evidence")
		page, err := repo.ListRuns(ctx, service.IntelligenceMonitorRunQuery{PlanID: &plan.ID, TestKind: "candy", Page: 1, PageSize: 60})
		require.NoError(t, err)
		require.Len(t, page.Items, 1)
		require.Equal(t, detail.Fingerprint.Summary(), page.Items[0].Fingerprint)
	}
}
