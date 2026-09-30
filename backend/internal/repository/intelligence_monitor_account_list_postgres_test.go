package repository

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestIntelligenceAccountPlansPostgresFiltersBeforeLoadingSharedGallery(t *testing.T) {
	db, ctx, _ := manualOrderTestDB(t)
	applyManualOrderMigration(t, db, ctx)
	_, err := db.ExecContext(ctx, `ALTER TABLE accounts
ADD COLUMN name TEXT NOT NULL DEFAULT 'Current OAuth name',
ADD COLUMN platform TEXT NOT NULL DEFAULT 'openai',
ADD COLUMN status TEXT NOT NULL DEFAULT 'active',
ADD COLUMN schedulable BOOLEAN NOT NULL DEFAULT TRUE,
ADD COLUMN expires_at TIMESTAMPTZ,
ADD COLUMN auto_pause_on_expired BOOLEAN NOT NULL DEFAULT FALSE,
ADD COLUMN parent_account_id BIGINT,
ADD COLUMN rate_limit_reset_at TIMESTAMPTZ,
ADD COLUMN temp_unschedulable_until TIMESTAMPTZ,
ADD COLUMN temp_unschedulable_reason TEXT,
ADD COLUMN extra JSONB;
CREATE TABLE groups(id BIGINT PRIMARY KEY,name TEXT NOT NULL,deleted_at TIMESTAMPTZ);
CREATE TABLE account_groups(account_id BIGINT NOT NULL,group_id BIGINT NOT NULL,PRIMARY KEY(account_id,group_id));
INSERT INTO accounts(id,type,credentials,extra) VALUES
(901,'oauth','{"access_token":"private-access-token"}','{"codex_7d_used_percent":100,"codex_7d_reset_at":"2030-10-01T12:00:00Z"}'),
(902,'oauth','{}','{}'),(903,'oauth','{}','{}');
INSERT INTO groups(id,name) VALUES(51,'Current group');
INSERT INTO account_groups(account_id,group_id) VALUES(901,51);
INSERT INTO intelligence_monitor_plans(id,name,source_type,account_id,created_by,enabled,candy_enabled,deleted_at,sort_order) VALUES
(301,'Original name','openai_oauth',901,1,FALSE,TRUE,NULL,2),
(302,'Legacy second','openai_oauth',901,1,FALSE,FALSE,NULL,1),
(303,'Other account','openai_oauth',902,1,FALSE,FALSE,NULL,1),
(304,'Archived','openai_oauth',901,1,FALSE,FALSE,NOW(),0),
(305,'Other source stale link','external',901,1,FALSE,FALSE,NULL,0);
INSERT INTO intelligence_monitor_runs(plan_id,plan_name,status,trigger,model,reasoning_effort,prompt,source_type,source_name,source_endpoint,api_mode,timeout_seconds,html,raw_text,request_key_encrypted,created_at)
SELECT p.id,p.name,'succeeded','manual','m','high','draw',p.source_type,'Historical source','','responses',600,'<html>shared artwork</html>','large raw result','secret-request',NOW()+make_interval(secs=>n)
FROM intelligence_monitor_plans p CROSS JOIN generate_series(1,25) n WHERE p.id BETWEEN 301 AND 305;
INSERT INTO intelligence_monitor_runs(plan_id,plan_name,status,trigger,model,reasoning_effort,prompt,source_type,source_name,source_endpoint,api_mode,timeout_seconds,created_at)
VALUES(301,'Original name','running','manual','m','high','draw','openai_oauth','Historical source','','responses',600,NOW()-INTERVAL '1 hour');
INSERT INTO intelligence_monitor_runs(plan_id,plan_name,status,trigger,model,reasoning_effort,prompt,source_type,source_name,source_endpoint,api_mode,timeout_seconds,test_kind,correct,answer)
VALUES(301,'Original name','succeeded','manual','m','high','candy','openai_oauth','Historical source','','responses',600,'candy',TRUE,'21');
UPDATE intelligence_monitor_runs SET rate_snapshot='"unrelated malformed billing"'::jsonb WHERE plan_id IN (303,304,305);`)
	require.NoError(t, err)
	repo := &intelligenceMonitorRepository{db: db}
	svc := service.NewIntelligenceMonitorService(repo, intelligencePGEncryptor{}, nil, nil, nil, nil, nil)
	plans, err := svc.ListPlansForAccount(ctx, 901)
	require.NoError(t, err, "unrelated, archived, and non-OAuth records must be filtered before decoding summaries")
	require.Len(t, plans, 2)
	require.Equal(t, int64(302), plans[0].ID)
	require.Equal(t, int64(301), plans[1].ID)
	require.Equal(t, "running", plans[1].LatestRun.Status, "an older active request remains visible")
	require.Equal(t, service.IntelligenceMonitorTestCandy, plans[1].CandyLatestRun.TestKind)
	require.Equal(t, "21", plans[1].CandyLatestRun.Answer)
	for _, plan := range plans {
		require.Equal(t, "openai_oauth", plan.SourceType)
		require.Equal(t, int64(901), *plan.AccountID)
		require.Equal(t, "Current OAuth name", plan.Name)
		require.False(t, plan.Enabled, "paused account monitors still reuse their saved history")
		require.Len(t, plan.RecentRuns, 20)
		require.Equal(t, "weekly_limited", plan.OAuthAccountStatus.Status)
		require.Equal(t, []service.IntelligenceOAuthAccountGroup{{ID: 51, Name: "Current group"}}, plan.OAuthAccountStatus.Groups)
		for _, run := range append(plan.RecentRuns, plan.LatestRun) {
			require.Equal(t, plan.ID, run.PlanID)
			require.Empty(t, run.HTML)
			require.Empty(t, run.RawText)
			require.Empty(t, run.RequestKeyEncrypted)
		}
	}
	runID := plans[1].RecentRuns[0].ID
	detail, err := svc.GetRun(ctx, runID)
	require.NoError(t, err)
	require.Equal(t, "<html>shared artwork</html>", detail.HTML, "the popup fetches the existing run's artwork without creating a copy")
	_, err = db.ExecContext(ctx, `UPDATE accounts SET name='Renamed OAuth',extra='{"codex_7d_used_percent":12}' WHERE id=901;
UPDATE groups SET name='Renamed group' WHERE id=51`)
	require.NoError(t, err)
	plans, err = svc.ListPlansForAccount(ctx, 901)
	require.NoError(t, err)
	require.Equal(t, "Renamed OAuth", plans[1].Name)
	require.Equal(t, "normal", plans[1].OAuthAccountStatus.Status)
	require.Equal(t, "Renamed group", plans[1].OAuthAccountStatus.Groups[0].Name)
	require.Equal(t, runID, plans[1].RecentRuns[0].ID)
	for _, id := range []int64{903, 999} {
		plans, err = svc.ListPlansForAccount(ctx, id)
		require.NoError(t, err)
		require.NotNil(t, plans)
		require.Empty(t, plans)
	}
	for _, id := range []int64{0, -1} {
		_, err = repo.ListPlansForAccount(ctx, id)
		require.ErrorIs(t, err, service.ErrIntelligenceInvalid)
	}
	var count int
	require.NoError(t, db.QueryRowContext(ctx, `SELECT COUNT(*) FROM intelligence_monitor_plans WHERE source_type='openai_oauth' AND account_id=901 AND deleted_at IS NULL`).Scan(&count))
	require.Equal(t, 2, count, "opening an account's gallery never creates a duplicate monitoring plan")
}
