package repository

import (
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/Wei-Shaw/sub2api/migrations"
	"github.com/stretchr/testify/require"
)

func TestIntelligenceCandyScheduleMigrationPostgresPreservesIndependentState(t *testing.T) {
	db, ctx := intelligenceMonitorTestDB(t)
	_, err := db.ExecContext(ctx, `ALTER TABLE intelligence_monitor_plans DROP COLUMN candy_interval_seconds,DROP COLUMN candy_last_run_at,DROP COLUMN candy_next_run_at;
INSERT INTO intelligence_monitor_plans(id,name,source_type,created_by,enabled,candy_enabled,next_run_at,deleted_at) VALUES
(1,'Enabled','external',1,TRUE,TRUE,'2026-10-01T00:00:00Z',NULL),
(2,'Paused','external',1,FALSE,TRUE,NULL,NULL),
(3,'Candy off','external',1,TRUE,FALSE,'2026-10-01T00:00:00Z',NULL),
(4,'Running candy','external',1,TRUE,TRUE,NULL,NULL),
(5,'Archived','external',1,FALSE,TRUE,NULL,NOW());
INSERT INTO intelligence_monitor_runs(plan_id,plan_name,status,trigger,model,reasoning_effort,prompt,source_type,source_name,source_endpoint,api_mode,timeout_seconds,test_kind,finished_at,html,raw_text)
VALUES(1,'Enabled','succeeded','manual','m','high','original question','external','source','','responses',600,'candy','2026-09-27T00:00:00Z','','original explanation'),
(4,'Running candy','running','scheduled','m','high','question','external','source','','responses',600,'candy',NULL,'','')`)
	require.NoError(t, err)
	body, err := migrations.FS.ReadFile("258_intelligence_candy_schedule.sql")
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, string(body))
	require.NoError(t, err)
	repo := &intelligenceMonitorRepository{db: db}
	first, err := repo.GetPlan(ctx, 1)
	require.NoError(t, err)
	require.Equal(t, 180, first.CandyIntervalSeconds)
	require.NotNil(t, first.CandyNextRunAt)
	require.Equal(t, time.Date(2026, 9, 27, 0, 0, 0, 0, time.UTC), first.CandyLastRunAt.UTC())
	require.Equal(t, time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), first.NextRunAt.UTC())
	for _, id := range []int64{2, 3} {
		plan, err := repo.GetPlan(ctx, id)
		require.NoError(t, err)
		require.Nil(t, plan.CandyNextRunAt)
	}
	running, err := repo.GetPlan(ctx, 4)
	require.NoError(t, err)
	require.Nil(t, running.CandyNextRunAt, "running candy keeps its consumed schedule")
	require.NotNil(t, running.NextRunAt, "decoupling unblocks an artwork countdown held by the old shared scheduler")
	var archivedNext *time.Time
	require.NoError(t, db.QueryRowContext(ctx, `SELECT candy_next_run_at FROM intelligence_monitor_plans WHERE id=5`).Scan(&archivedNext))
	require.Nil(t, archivedNext)
	// Reapplication must preserve a configured cadence and both countdowns.
	_, err = db.ExecContext(ctx, `UPDATE intelligence_monitor_plans SET candy_interval_seconds=900,candy_next_run_at='2026-10-02T00:00:00Z' WHERE id=1`)
	require.NoError(t, err)
	var before, after string
	require.NoError(t, db.QueryRowContext(ctx, `SELECT jsonb_agg(to_jsonb(p) ORDER BY id)::text FROM intelligence_monitor_plans p`).Scan(&before))
	_, err = db.ExecContext(ctx, string(body))
	require.NoError(t, err)
	require.NoError(t, db.QueryRowContext(ctx, `SELECT jsonb_agg(to_jsonb(p) ORDER BY id)::text FROM intelligence_monitor_plans p`).Scan(&after))
	require.JSONEq(t, before, after)
	detail, err := repo.GetRun(ctx, 1)
	require.NoError(t, err)
	require.Equal(t, "original question", detail.Prompt)
	require.Equal(t, "original explanation", detail.RawText)
	for _, interval := range []int{180, 300, 600, 900} {
		_, err = db.ExecContext(ctx, `UPDATE intelligence_monitor_plans SET candy_interval_seconds=$1 WHERE id=1`, interval)
		require.NoError(t, err)
	}
	for _, interval := range []int{0, 179, 181, 301, 901} {
		_, err = db.ExecContext(ctx, `UPDATE intelligence_monitor_plans SET candy_interval_seconds=$1 WHERE id=1`, interval)
		require.Error(t, err, "database rejects unsupported candy interval %d", interval)
	}
}

func TestIntelligenceCandySchedulePostgresEditsPreserveOtherCountdown(t *testing.T) {
	db, ctx := intelligenceMonitorTestDB(t)
	repo := &intelligenceMonitorRepository{db: db}
	plan := candyRepositoryPlan(t, ctx, repo, "Independent edits", true, true)
	_, err := db.ExecContext(ctx, `UPDATE intelligence_monitor_plans SET next_run_at=NOW()+INTERVAL '2 hours',candy_next_run_at=NOW()+INTERVAL '3 hours' WHERE id=$1`, plan.ID)
	require.NoError(t, err)
	plan, err = repo.GetPlan(ctx, plan.ID)
	require.NoError(t, err)
	originalPelican, originalCandy := *plan.NextRunAt, *plan.CandyNextRunAt
	plan.Name = "Renamed"
	require.NoError(t, repo.SavePlan(ctx, plan))
	require.Equal(t, originalPelican, *plan.NextRunAt)
	require.Equal(t, originalCandy, *plan.CandyNextRunAt, "metadata edits preserve both timers")
	plan.CandyIntervalSeconds = 600
	beforeEdit := time.Now()
	require.NoError(t, repo.SavePlan(ctx, plan))
	require.Equal(t, originalPelican, *plan.NextRunAt)
	require.WithinDuration(t, beforeEdit.Add(600*time.Second), *plan.CandyNextRunAt, 3*time.Second)
	candyNext := *plan.CandyNextRunAt
	plan.IntervalSeconds = 123
	beforeEdit = time.Now()
	require.NoError(t, repo.SavePlan(ctx, plan))
	require.WithinDuration(t, beforeEdit.Add(123*time.Second), *plan.NextRunAt, 3*time.Second)
	require.Equal(t, candyNext, *plan.CandyNextRunAt)
	pelicanNext := *plan.NextRunAt
	plan.CandyEnabled = false
	require.NoError(t, repo.SavePlan(ctx, plan))
	require.Nil(t, plan.CandyNextRunAt)
	require.Equal(t, pelicanNext, *plan.NextRunAt, "disabling candy does not move the artwork schedule")
	plan.CandyEnabled = true
	require.NoError(t, repo.SavePlan(ctx, plan))
	require.NotNil(t, plan.CandyNextRunAt)
	require.Equal(t, pelicanNext, *plan.NextRunAt)
	plan.Enabled = false
	require.NoError(t, repo.SavePlan(ctx, plan))
	require.Nil(t, plan.NextRunAt)
	require.Nil(t, plan.CandyNextRunAt)
	plan.Enabled = true
	require.NoError(t, repo.SavePlan(ctx, plan))
	require.NotNil(t, plan.NextRunAt)
	require.NotNil(t, plan.CandyNextRunAt)
}

func TestIntelligenceCandySchedulePostgresSerialDueWorkNeverAccumulates(t *testing.T) {
	db, ctx := intelligenceMonitorTestDB(t)
	repo := &intelligenceMonitorRepository{db: db}
	plan := candyRepositoryPlan(t, ctx, repo, "Serial independent schedules", true, true)
	due, err := repo.DuePlanIDs(ctx, 10)
	require.NoError(t, err)
	require.Equal(t, []int64{plan.ID}, due)
	_, err = db.ExecContext(ctx, `UPDATE intelligence_monitor_plans SET candy_next_run_at=NOW()+INTERVAL '1 hour' WHERE id=$1`, plan.ID)
	require.NoError(t, err)
	candyDue, err := repo.DueCandyPlanIDs(ctx, 10)
	require.NoError(t, err)
	require.Empty(t, candyDue)
	require.NoError(t, repo.Enqueue(ctx, candyRepositoryRun(plan, "pelican"), true))
	artwork, err := repo.ClaimNext(ctx, "artwork-worker")
	require.NoError(t, err)
	require.NotNil(t, artwork)
	_, err = db.ExecContext(ctx, `UPDATE intelligence_monitor_plans SET candy_next_run_at=NOW() WHERE id=$1`, plan.ID)
	require.NoError(t, err)
	candyDue, err = repo.DueCandyPlanIDs(ctx, 10)
	require.NoError(t, err)
	require.Equal(t, []int64{plan.ID}, candyDue, "a running artwork does not block scheduling candy")
	require.NoError(t, repo.Enqueue(ctx, candyRepositoryRun(plan, "candy"), true))
	for range 3 {
		candyDue, err = repo.DueCandyPlanIDs(ctx, 10)
		require.NoError(t, err)
		require.Empty(t, candyDue)
		require.Error(t, repo.Enqueue(ctx, candyRepositoryRun(plan, "candy"), true))
	}
	var count int
	require.NoError(t, db.QueryRowContext(ctx, `SELECT COUNT(*) FROM intelligence_monitor_runs WHERE plan_id=$1 AND test_kind='candy'`, plan.ID).Scan(&count))
	require.Equal(t, 1, count)
	next, err := repo.ClaimNext(ctx, "blocked-candy-worker")
	require.NoError(t, err)
	require.Nil(t, next)
	// The global switch pauses future schedules but preserves queued work.
	plan.Enabled, plan.AllowWhileBusy = false, true
	require.NoError(t, repo.SavePlan(ctx, plan))
	require.Nil(t, plan.NextRunAt)
	require.Nil(t, plan.CandyNextRunAt)
	artwork.Status = "failed"
	require.NoError(t, repo.CompleteRun(ctx, artwork))
	next, err = repo.ClaimNext(ctx, "paused-candy-worker")
	require.NoError(t, err)
	require.NotNil(t, next)
	require.Equal(t, "candy", next.TestKind)
	next.Status = "failed"
	require.NoError(t, repo.CompleteRun(ctx, next))
	plan, err = repo.GetPlan(ctx, plan.ID)
	require.NoError(t, err)
	require.Nil(t, plan.NextRunAt)
	require.Nil(t, plan.CandyNextRunAt, "completion cannot resume a paused plan")
	require.NotNil(t, plan.LastRunAt)
	require.NotNil(t, plan.CandyLastRunAt)
	plan.Enabled, plan.AllowWhileBusy = true, false
	require.NoError(t, repo.SavePlan(ctx, plan))
	require.NotNil(t, plan.NextRunAt)
	require.NotNil(t, plan.CandyNextRunAt)
	require.NoError(t, repo.ArchivePlan(ctx, plan.ID))
	var active bool
	var artworkNext, candyNext *time.Time
	require.NoError(t, db.QueryRowContext(ctx, `SELECT enabled,next_run_at,candy_next_run_at FROM intelligence_monitor_plans WHERE id=$1`, plan.ID).Scan(&active, &artworkNext, &candyNext))
	require.False(t, active)
	require.Nil(t, artworkNext)
	require.Nil(t, candyNext)
}

func TestIntelligenceBorrowedKeyPostgresSurvivesRebindArchiveAndPurge(t *testing.T) {
	db, ctx := intelligenceMonitorTestDB(t)
	repo := &intelligenceMonitorRepository{db: db}
	_, err := db.ExecContext(ctx, `ALTER TABLE api_keys ADD COLUMN user_id BIGINT NOT NULL DEFAULT 7,ADD COLUMN key TEXT NOT NULL DEFAULT 'fixture-key';
INSERT INTO api_keys(id,status) VALUES(11,'active'),(12,'active'),(13,'active'),(14,'active')`)
	require.NoError(t, err)
	newPlan := func(name string, keyID int64, borrowed bool) *service.IntelligenceMonitorPlan {
		owner, group := int64(7), int64(9)
		p := &service.IntelligenceMonitorPlan{Name: name, SourceType: "local_group", APIMode: "responses", IntervalSeconds: 300, TimeoutSeconds: 600, CreatedBy: owner, GroupID: &group, LocalAPIKeyID: &keyID, LocalKeyOwnerID: &owner, LocalAPIKeyBorrowed: borrowed}
		require.NoError(t, repo.SavePlan(ctx, p))
		return p
	}
	assertStatus := func(id int64, expected string) {
		var status string
		require.NoError(t, db.QueryRowContext(ctx, `SELECT status FROM api_keys WHERE id=$1`, id).Scan(&status))
		require.Equal(t, expected, status)
	}
	borrowed := newPlan("Borrowed", 11, true)
	stored, err := repo.GetPlan(ctx, borrowed.ID)
	require.NoError(t, err)
	require.True(t, stored.LocalAPIKeyBorrowed)
	otherKey := int64(12)
	stored.LocalAPIKeyID = &otherKey
	require.NoError(t, repo.SavePlan(ctx, stored))
	assertStatus(11, "active")
	require.NoError(t, repo.ArchivePlan(ctx, stored.ID))
	assertStatus(12, "active")
	keys, err := (&upstreamCenterRepository{db: db}).PurgeStorage(ctx, service.UpstreamStoragePurgeInput{Kind: "intelligence", ID: stored.ID, ConfirmName: stored.Name})
	require.NoError(t, err)
	require.Empty(t, keys)
	assertStatus(12, "active")
	directPurge := newPlan("Borrowed direct purge", 11, true)
	keys, err = (&upstreamCenterRepository{db: db}).PurgeStorage(ctx, service.UpstreamStoragePurgeInput{Kind: "intelligence", ID: directPurge.ID, ConfirmName: directPurge.Name})
	require.NoError(t, err)
	require.Empty(t, keys)
	assertStatus(11, "active")
	managed := newPlan("Managed legacy", 13, false)
	otherKey = 14
	managed.LocalAPIKeyID = &otherKey
	require.NoError(t, repo.SavePlan(ctx, managed))
	assertStatus(13, "disabled")
	keys, err = (&upstreamCenterRepository{db: db}).PurgeStorage(ctx, service.UpstreamStoragePurgeInput{Kind: "intelligence", ID: managed.ID, ConfirmName: managed.Name})
	require.NoError(t, err)
	require.Equal(t, []string{"fixture-key"}, keys)
	assertStatus(14, "disabled")
}
