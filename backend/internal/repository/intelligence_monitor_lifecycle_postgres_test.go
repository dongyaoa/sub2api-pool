package repository

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/Wei-Shaw/sub2api/migrations"
	"github.com/stretchr/testify/require"
)

func TestIntelligenceLifecyclePostgresDeleteArtworkAndPublicVisibility(t *testing.T) {
	db, ctx, repo := publicPelicanTestDB(t)
	cfg := service.DefaultIntelligencePublicDisplay()
	cfg.Enabled, cfg.HideFailed, cfg.PlanIDs = true, true, []int64{101, 105}
	require.NoError(t, repo.SavePublicDisplay(ctx, cfg))
	_, err := db.ExecContext(ctx, `UPDATE intelligence_monitor_plans SET model='gpt-6.1-sol' WHERE id=105`)
	require.NoError(t, err)
	success := publicPelicanInsertRun(t, ctx, db, 101, "pelican", "local_group", 1, "succeeded")
	failure := publicPelicanInsertRun(t, ctx, db, 101, "pelican", "local_group", 1, "failed")
	sol := publicPelicanInsertRun(t, ctx, db, 105, "pelican", "local_group", 1, "succeeded")
	_, err = db.ExecContext(ctx, `UPDATE intelligence_monitor_runs SET model='gpt-6.1-sol' WHERE id=$1`, sol)
	require.NoError(t, err)
	page, err := repo.ListPublicPelican(ctx, []int64{1})
	require.NoError(t, err)
	require.Len(t, page.Items, 2, "both model plans for one group are public")
	require.Equal(t, success, page.Items[0].LatestRun.ID)
	require.Len(t, page.Items[0].RecentRuns, 1)
	require.Equal(t, "gpt-6.1-sol", page.Items[1].Model)
	require.Equal(t, "gpt-6.1-sol", page.Items[1].LatestRun.Model)
	oldModel := publicPelicanInsertRun(t, ctx, db, 105, "pelican", "local_group", 1, "succeeded")
	_, err = repo.GetPublicPelicanRun(ctx, oldModel, []int64{1})
	require.ErrorIs(t, err, service.ErrIntelligenceNotFound, "model changes must not publish the previous model under the new model card")
	_, err = repo.GetPublicPelicanRun(ctx, failure, []int64{1})
	require.ErrorIs(t, err, service.ErrIntelligenceNotFound)
	_, err = repo.GetRun(ctx, failure)
	require.NoError(t, err, "hiding failed artwork preserves admin diagnostics")
	require.NoError(t, repo.DeletePelicanRun(ctx, success))
	_, err = repo.GetRun(ctx, success)
	require.ErrorIs(t, err, service.ErrIntelligenceNotFound)
	_, err = repo.GetPublicPelicanRun(ctx, success, []int64{1})
	require.ErrorIs(t, err, service.ErrIntelligenceNotFound)
	page, err = repo.ListPublicPelican(ctx, []int64{1})
	require.NoError(t, err)
	require.Nil(t, page.Items[0].LatestRun)
	require.Empty(t, page.Items[0].RecentRuns)
	active := publicPelicanInsertRun(t, ctx, db, 101, "pelican", "local_group", 1, "running")
	require.ErrorIs(t, repo.DeletePelicanRun(ctx, active), service.ErrIntelligenceBusy)
	page, err = repo.ListPublicPelican(ctx, []int64{1})
	require.NoError(t, err)
	require.Equal(t, active, page.Items[0].LatestRun.ID, "failure filter keeps progress visible")
	cfg.HideFailed = false
	require.NoError(t, repo.SavePublicDisplay(ctx, cfg))
	_, err = repo.GetPublicPelicanRun(ctx, failure, []int64{1})
	require.NoError(t, err)
	candy := publicPelicanInsertRun(t, ctx, db, 101, "candy", "local_group", 1, "succeeded")
	require.ErrorIs(t, repo.DeletePelicanRun(ctx, candy), service.ErrIntelligenceNotFound)
}

func TestIntelligenceLifecyclePostgresPermanentOAuthDeletionAndAccountCleanup(t *testing.T) {
	db, ctx := intelligenceMonitorTestDB(t)
	repo := &intelligenceMonitorRepository{db: db}
	_, err := db.ExecContext(ctx, `INSERT INTO accounts(id) VALUES(1),(2);
INSERT INTO intelligence_monitor_plans(id,name,source_type,account_id,created_by,deleted_at) VALUES
(1,'OAuth','openai_oauth',1,1,NULL),(2,'Archived OAuth','openai_oauth',1,1,NOW()),(3,'Other OAuth','openai_oauth',2,1,NULL),(4,'External','external',1,1,NULL);`)
	require.NoError(t, err)
	running := publicPelicanInsertRun(t, ctx, db, 1, "pelican", "openai_oauth", 1, "running")
	publicPelicanInsertRun(t, ctx, db, 2, "candy", "openai_oauth", 1, "succeeded")
	publicPelicanInsertRun(t, ctx, db, 3, "pelican", "openai_oauth", 1, "running")
	live, err := repo.LiveOAuthExecutionIDs(ctx, []int64{running})
	require.NoError(t, err)
	require.Equal(t, []int64{running}, live)
	require.ErrorIs(t, repo.DeleteOAuthPlanPermanently(ctx, 4), service.ErrIntelligenceInvalid)
	// Account and monitor deletion share one transaction; rollback restores both.
	tx, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	require.NoError(t, purgeAccountOAuthMonitors(ctx, tx, 1))
	_, err = tx.ExecContext(ctx, `UPDATE accounts SET deleted_at=NOW() WHERE id=1`)
	require.NoError(t, err)
	require.NoError(t, tx.Rollback())
	_, err = repo.GetRun(ctx, running)
	require.NoError(t, err)
	tx, err = db.BeginTx(ctx, nil)
	require.NoError(t, err)
	require.NoError(t, purgeAccountOAuthMonitors(ctx, tx, 1))
	_, err = tx.ExecContext(ctx, `UPDATE accounts SET deleted_at=NOW() WHERE id=1`)
	require.NoError(t, err)
	require.NoError(t, tx.Commit())
	var count int
	require.NoError(t, db.QueryRowContext(ctx, `SELECT COUNT(*) FROM intelligence_monitor_plans WHERE id IN (1,2)`).Scan(&count))
	require.Zero(t, count)
	require.NoError(t, db.QueryRowContext(ctx, `SELECT COUNT(*) FROM intelligence_monitor_runs WHERE plan_id IN (1,2)`).Scan(&count))
	require.Zero(t, count)
	live, err = repo.LiveOAuthExecutionIDs(ctx, []int64{running})
	require.NoError(t, err)
	require.Empty(t, live)
	require.ErrorIs(t, repo.CompleteRun(ctx, &service.IntelligenceMonitorRun{ID: running, PlanID: 1}), service.ErrIntelligenceNotFound)
	_, err = repo.GetPlan(ctx, 4)
	require.NoError(t, err, "non-OAuth monitoring remains untouched")
	require.NoError(t, repo.DeleteOAuthPlanPermanently(ctx, 3), "active OAuth deletion does not wait for generation")
	require.NoError(t, db.QueryRowContext(ctx, `SELECT COUNT(*) FROM intelligence_monitor_runs WHERE plan_id=3`).Scan(&count))
	require.Zero(t, count)
	tx, err = db.BeginTx(ctx, nil)
	require.NoError(t, err)
	accountID := int64(1)
	require.ErrorIs(t, validateIntelligenceOAuthAccountExists(ctx, tx, &accountID), service.ErrIntelligenceOAuthUnavailable)
	require.NoError(t, tx.Rollback())
}

func TestIntelligenceLifecyclePostgresOrphanMigration(t *testing.T) {
	db, ctx := intelligenceMonitorTestDB(t)
	_, err := db.ExecContext(ctx, `INSERT INTO accounts(id,deleted_at) VALUES(1,NULL),(2,NOW());
INSERT INTO intelligence_monitor_plans(id,name,source_type,account_id,created_by,deleted_at) VALUES
(1,'Valid','openai_oauth',1,1,NULL),(2,'Deleted','openai_oauth',2,1,NULL),(3,'Missing','openai_oauth',NULL,1,NOW()),(4,'External','external',NULL,1,NULL);`)
	require.NoError(t, err)
	publicPelicanInsertRun(t, ctx, db, 2, "pelican", "openai_oauth", 1, "running")
	publicPelicanInsertRun(t, ctx, db, 3, "candy", "openai_oauth", 1, "succeeded")
	body, err := migrations.FS.ReadFile("263_intelligence_deleted_oauth_cleanup.sql")
	require.NoError(t, err)
	for i := 0; i < 2; i++ {
		_, err = db.ExecContext(ctx, string(body))
		require.NoError(t, err)
	}
	var count int
	require.NoError(t, db.QueryRowContext(ctx, `SELECT COUNT(*) FROM intelligence_monitor_plans`).Scan(&count))
	require.Equal(t, 2, count)
	require.NoError(t, db.QueryRowContext(ctx, `SELECT COUNT(*) FROM intelligence_monitor_runs`).Scan(&count))
	require.Zero(t, count)
}

func TestIntelligenceLifecyclePostgresBulkSchedulePreservesActiveRunsAndIntervals(t *testing.T) {
	db, ctx := intelligenceMonitorTestDB(t)
	repo := &intelligenceMonitorRepository{db: db}
	_, err := db.ExecContext(ctx, `INSERT INTO intelligence_monitor_plans(id,name,source_type,created_by,enabled,candy_enabled,interval_seconds,candy_interval_seconds,next_run_at,candy_next_run_at,deleted_at) VALUES
(1,'Active','external',1,TRUE,TRUE,300,180,NOW(),NOW(),NULL),
(2,'Paused','local_group',1,FALSE,FALSE,600,300,NULL,NULL,NULL),
(3,'Archived','external',1,FALSE,TRUE,900,900,NULL,NULL,NOW());`)
	require.NoError(t, err)
	running := publicPelicanInsertRun(t, ctx, db, 1, "pelican", "external", 1, "running")
	queued := publicPelicanInsertRun(t, ctx, db, 1, "candy", "external", 1, "pending")
	scheduled := publicPelicanInsertRun(t, ctx, db, 2, "pelican", "local_group", 1, "pending")
	_, err = db.ExecContext(ctx, `UPDATE intelligence_monitor_runs SET trigger='scheduled' WHERE id=$1`, scheduled)
	require.NoError(t, err)
	off, err := repo.SetPlansEnabled(ctx, false)
	require.NoError(t, err)
	require.Equal(t, int64(1), off.Updated)
	require.Equal(t, int64(2), off.Total)
	require.Zero(t, off.Enabled)
	_, err = repo.GetRun(ctx, scheduled)
	require.ErrorIs(t, err, service.ErrIntelligenceNotFound, "unstarted scheduled work is withdrawn while running and manual requests are kept")
	plan, err := repo.GetPlan(ctx, 1)
	require.NoError(t, err)
	require.Nil(t, plan.NextRunAt)
	require.Nil(t, plan.CandyNextRunAt)
	run, err := repo.GetRun(ctx, running)
	require.NoError(t, err)
	require.Equal(t, "running", run.Status)
	run.Status, run.HTML = "succeeded", "<html>finished</html>"
	require.NoError(t, repo.CompleteRun(ctx, run))
	plan, err = repo.GetPlan(ctx, 1)
	require.NoError(t, err)
	require.Nil(t, plan.NextRunAt, "finishing an in-flight run must not re-enable a paused schedule")
	on, err := repo.SetPlansEnabled(ctx, true)
	require.NoError(t, err)
	require.Equal(t, int64(2), on.Updated)
	require.Equal(t, int64(2), on.Enabled)
	plan, err = repo.GetPlan(ctx, 1)
	require.NoError(t, err)
	require.NotNil(t, plan.NextRunAt)
	require.Nil(t, plan.CandyNextRunAt, "existing pending candy keeps its consumed timer")
	require.Equal(t, 300, plan.IntervalSeconds)
	require.Equal(t, 180, plan.CandyIntervalSeconds)
	require.True(t, plan.CandyEnabled)
	run, err = repo.GetRun(ctx, queued)
	require.NoError(t, err)
	require.Equal(t, "pending", run.Status)
	status, err := repo.ScheduleStatus(ctx)
	require.NoError(t, err)
	require.Equal(t, on.IntelligenceScheduleStatus, *status)
	on, err = repo.SetPlansEnabled(ctx, true)
	require.NoError(t, err)
	require.Zero(t, on.Updated, "repeated enable is idempotent")
}
