package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func publicPelicanTestDB(t *testing.T) (*sql.DB, context.Context, *intelligenceMonitorRepository) {
	t.Helper()
	db, ctx := intelligenceMonitorTestDB(t)
	_, err := db.ExecContext(ctx, `CREATE TABLE groups(id BIGINT PRIMARY KEY,name TEXT,rate_multiplier NUMERIC,status TEXT,deleted_at TIMESTAMPTZ);
INSERT INTO groups VALUES(1,'Public group',1.5,'active',NULL),(2,'Restricted group',2,'active',NULL),(3,'Paused group',3,'disabled',NULL),(4,'Deleted group',4,'active',NOW());
INSERT INTO intelligence_monitor_plans(id,name,source_type,group_id,created_by,enabled,sort_order) VALUES
(101,'private administrator plan','local_group',1,1,TRUE,1),(102,'restricted administrator plan','local_group',2,1,TRUE,2),(103,'paused group plan','local_group',3,1,TRUE,3),(104,'deleted group plan','local_group',4,1,TRUE,4),(105,'not selected local plan','local_group',1,1,TRUE,5),(106,'external plan','external',1,1,TRUE,6),(107,'archived local plan','local_group',1,1,TRUE,7);
UPDATE intelligence_monitor_plans SET deleted_at=NOW() WHERE id=107;`)
	require.NoError(t, err)
	return db, ctx, &intelligenceMonitorRepository{db: db}
}

func publicPelicanInsertRun(t *testing.T, ctx context.Context, db *sql.DB, planID int64, kind, source string, groupID int64, status string) int64 {
	t.Helper()
	var id int64
	require.NoError(t, db.QueryRowContext(ctx, `INSERT INTO intelligence_monitor_runs(plan_id,plan_name,status,trigger,model,reasoning_effort,prompt,source_type,source_name,source_endpoint,source_snapshot,rate_snapshot,notes_snapshot,api_mode,timeout_seconds,test_kind,raw_text,error,http_status,html,request_key_encrypted,lease_token)
VALUES($1,'private plan',$2,'manual','gpt-6-astra','high','private prompt',$3,'private account','https://private-upstream.test',jsonb_build_object('group_id',$4::bigint,'account_name','secret account','api_key','sk-secret'),'{"effective_rate_multiplier":0.1}','{"private_note":"admin only"}','responses',600,$5,'private raw answer','private upstream HTTP 502 diagnostic',502,'<!doctype html><html><svg></svg></html>','private credential','private lease') RETURNING id`, planID, status, source, groupID, kind).Scan(&id))
	return id
}

func TestIntelligencePublicDisplayPostgresValidatesSelectionAndPersists(t *testing.T) {
	db, ctx, repo := publicPelicanTestDB(t)
	cfg, err := repo.GetPublicDisplay(ctx)
	require.NoError(t, err)
	require.Equal(t, service.DefaultIntelligencePublicDisplay(), *cfg)
	cfg.Enabled = true
	cfg.Title = "Site artwork"
	cfg.PlanIDs = []int64{101, 102}
	require.NoError(t, repo.SavePublicDisplay(ctx, *cfg))
	other := &intelligenceMonitorRepository{db: candyRepositoryConnection(t, ctx, db)}
	stored, err := other.GetPublicDisplay(ctx)
	require.NoError(t, err)
	require.Equal(t, cfg, stored)
	for _, ids := range [][]int64{{103}, {104}, {106}, {107}, {999}, {101, 106}} {
		invalid := *cfg
		invalid.PlanIDs = ids
		require.ErrorIs(t, repo.SavePublicDisplay(ctx, invalid), service.ErrIntelligenceInvalid)
	}
	stored, err = repo.GetPublicDisplay(ctx)
	require.NoError(t, err)
	require.Equal(t, cfg, stored, "invalid selection must not partially replace visibility")
}

func TestIntelligencePublicPelicanPostgresFilteredBoundedAndRedacted(t *testing.T) {
	db, ctx, repo := publicPelicanTestDB(t)
	cfg := service.DefaultIntelligencePublicDisplay()
	cfg.Enabled = true
	cfg.PlanIDs = []int64{101, 102, 103, 104}
	// Groups can become inactive after an administrator selects valid plans.
	_, err := db.ExecContext(ctx, `UPDATE groups SET status='active',deleted_at=NULL WHERE id IN (3,4)`)
	require.NoError(t, err)
	require.NoError(t, repo.SavePublicDisplay(ctx, cfg))
	_, err = db.ExecContext(ctx, `UPDATE groups SET status='disabled' WHERE id=3; UPDATE groups SET deleted_at=NOW() WHERE id=4`)
	require.NoError(t, err)
	var successID, failedID int64
	for i := 0; i < 25; i++ {
		successID = publicPelicanInsertRun(t, ctx, db, 101, "pelican", "local_group", 1, "succeeded")
	}
	failedID = publicPelicanInsertRun(t, ctx, db, 101, "pelican", "local_group", 1, "failed")
	activeID := publicPelicanInsertRun(t, ctx, db, 101, "pelican", "local_group", 1, "running")
	_, err = db.ExecContext(ctx, `UPDATE intelligence_monitor_runs SET created_at=NOW()-INTERVAL '1 day' WHERE id=$1`, activeID)
	require.NoError(t, err)
	blocked := []int64{
		publicPelicanInsertRun(t, ctx, db, 101, "candy", "local_group", 1, "succeeded"),
		publicPelicanInsertRun(t, ctx, db, 101, "pelican", "external", 1, "succeeded"),
		publicPelicanInsertRun(t, ctx, db, 101, "pelican", "local_group", 2, "succeeded"),
		publicPelicanInsertRun(t, ctx, db, 102, "pelican", "local_group", 2, "succeeded"),
		publicPelicanInsertRun(t, ctx, db, 103, "pelican", "local_group", 3, "succeeded"),
		publicPelicanInsertRun(t, ctx, db, 104, "pelican", "local_group", 4, "succeeded"),
		publicPelicanInsertRun(t, ctx, db, 105, "pelican", "local_group", 1, "succeeded"),
		publicPelicanInsertRun(t, ctx, db, 106, "pelican", "external", 1, "succeeded"),
		publicPelicanInsertRun(t, ctx, db, 107, "pelican", "local_group", 1, "succeeded"),
	}
	page, err := repo.ListPublicPelican(ctx, []int64{1, 3, 4})
	require.NoError(t, err)
	require.Len(t, page.Items, 1)
	plan := page.Items[0]
	require.Equal(t, int64(101), plan.ID)
	require.Equal(t, "Public group", plan.GroupName)
	require.Equal(t, 1.5, *plan.GroupRateMultiplier, "publish the group's base rate, never an administrator's private effective discount")
	require.Equal(t, activeID, plan.LatestRun.ID, "an older active run must not disappear behind recent terminal history")
	require.Len(t, plan.RecentRuns, 20)
	require.Equal(t, failedID, plan.RecentRuns[0].ID)
	require.NotEmpty(t, plan.RecentRuns[0].Error)
	require.WithinDuration(t, time.Now(), page.ServerTime, 5*time.Second)
	for _, run := range plan.RecentRuns {
		require.NotContains(t, blocked, run.ID)
	}
	encoded, err := json.Marshal(page)
	require.NoError(t, err)
	for _, secret := range []string{"private", "sk-secret", "source_snapshot", "source_name", "source_type", "raw_text", "html", "candy", "http_status", "api_key", "lease_token", "plan_ids", "account_name"} {
		require.NotContains(t, string(encoded), secret)
	}
	detail, err := repo.GetPublicPelicanRun(ctx, successID, []int64{1})
	require.NoError(t, err)
	require.Contains(t, detail.HTML, "<svg>")
	require.Empty(t, detail.Error, "successful records must not expose stored internal diagnostic fields")
	encoded, err = json.Marshal(detail)
	require.NoError(t, err)
	var fields map[string]any
	require.NoError(t, json.Unmarshal(encoded, &fields))
	require.Len(t, fields, 11)
	for _, field := range []string{"id", "plan_id", "status", "model", "reasoning_effort", "created_at", "started_at", "finished_at", "duration_ms", "error", "html"} {
		require.Contains(t, fields, field)
	}
	failed, err := repo.GetPublicPelicanRun(ctx, failedID, []int64{1})
	require.NoError(t, err)
	require.Empty(t, failed.HTML)
	require.NotContains(t, failed.Error, "private")
	for _, id := range append(blocked, int64(999999)) {
		_, err = repo.GetPublicPelicanRun(ctx, id, []int64{1, 3, 4})
		require.ErrorIs(t, err, service.ErrIntelligenceNotFound)
	}
	// An empty permission set is deny-all, never unrestricted.
	page, err = repo.ListPublicPelican(ctx, nil)
	require.NoError(t, err)
	require.Empty(t, page.Items)
	_, err = repo.GetPublicPelicanRun(ctx, successID, nil)
	require.ErrorIs(t, err, service.ErrIntelligenceNotFound)
}

func TestIntelligencePublicPelicanPostgresRechecksCurrentGateAndGroup(t *testing.T) {
	db, ctx, repo := publicPelicanTestDB(t)
	cfg := service.DefaultIntelligencePublicDisplay()
	cfg.Enabled = true
	cfg.PlanIDs = []int64{101}
	require.NoError(t, repo.SavePublicDisplay(ctx, cfg))
	id := publicPelicanInsertRun(t, ctx, db, 101, "pelican", "local_group", 1, "succeeded")
	for _, mutation := range []string{
		`UPDATE intelligence_monitor_plans SET group_id=2 WHERE id=101`,
		`UPDATE intelligence_monitor_plans SET group_id=1,source_type='external' WHERE id=101`,
		`UPDATE intelligence_monitor_plans SET source_type='local_group',deleted_at=NOW() WHERE id=101`,
	} {
		_, err := db.ExecContext(ctx, mutation)
		require.NoError(t, err)
		_, err = repo.GetPublicPelicanRun(ctx, id, []int64{1, 2})
		require.ErrorIs(t, err, service.ErrIntelligenceNotFound)
		page, err := repo.ListPublicPelican(ctx, []int64{1, 2})
		require.NoError(t, err)
		for _, plan := range page.Items {
			require.Nil(t, plan.LatestRun, "rebinding must not expose artwork from the previous group")
			require.Empty(t, plan.RecentRuns)
		}
	}
	_, err := db.ExecContext(ctx, `UPDATE intelligence_monitor_plans SET source_type='local_group',group_id=1,deleted_at=NULL WHERE id=101`)
	require.NoError(t, err)
	cfg.Enabled = false
	require.NoError(t, repo.SavePublicDisplay(ctx, cfg))
	page, err := repo.ListPublicPelican(ctx, []int64{1})
	require.NoError(t, err)
	require.False(t, page.Config.Enabled)
	require.Empty(t, page.Items)
	_, err = repo.GetPublicPelicanRun(ctx, id, []int64{1})
	require.ErrorIs(t, err, service.ErrIntelligenceNotFound)
}
