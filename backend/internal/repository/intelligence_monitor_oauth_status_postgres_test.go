package repository

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestIntelligenceOAuthStatusPostgresLoadsIdentityAndQuotaWithoutTokens(t *testing.T) {
	db, ctx, _ := manualOrderTestDB(t)
	_, err := db.ExecContext(ctx, `ALTER TABLE accounts
ADD COLUMN name TEXT NOT NULL DEFAULT 'OAuth account',
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
INSERT INTO accounts(id,type,credentials,extra) VALUES
(501,'oauth','{"email":"current@example.com","chatgpt_account_id":"identity-501","workspace_id":"workspace-501","access_token":"never-load-access","refresh_token":"never-load-refresh","id_token":"never-load-id"}','{"codex_7d_used_percent":100,"codex_7d_reset_at":"2030-10-01T12:00:00Z","email":"current@example.com"}'),
(502,'oauth','{}',NULL),
(503,'oauth','{}','{}');
UPDATE accounts SET deleted_at=NOW() WHERE id=503;`)
	require.NoError(t, err)
	repo := &intelligenceMonitorRepository{db: db}
	accounts, err := repo.LoadOAuthMonitorAccounts(ctx, []int64{501, 501, 502, 503, 999})
	require.NoError(t, err)
	require.Len(t, accounts, 2)
	account := accounts[501]
	require.Equal(t, "OAuth account", account.Name)
	require.True(t, account.Schedulable)
	require.Equal(t, float64(100), account.Extra["codex_7d_used_percent"])
	require.Equal(t, "current@example.com", account.Credentials["email"])
	require.Equal(t, "identity-501", account.Credentials["chatgpt_account_id"])
	require.Equal(t, "workspace-501", account.Credentials["workspace_id"])
	require.NotContains(t, account.Credentials, "access_token")
	require.NotContains(t, account.Credentials, "refresh_token")
	require.NotContains(t, account.Credentials, "id_token")
	require.Nil(t, accounts[502].Extra)
	require.Empty(t, accounts[502].TempUnschedulableReason)
	accounts, err = repo.LoadOAuthMonitorAccounts(ctx, nil)
	require.NoError(t, err)
	require.Empty(t, accounts)
}

func TestIntelligenceOAuthStatusPostgresDefersBothTestsWithoutChangingConfiguration(t *testing.T) {
	db, ctx, _ := manualOrderTestDB(t)
	applyManualOrderMigration(t, db, ctx)
	before := time.Now().UTC().Add(-time.Minute).Truncate(time.Microsecond)
	until := time.Now().UTC().Add(30 * time.Second).Truncate(time.Microsecond)
	later := until.Add(time.Hour)
	_, err := db.ExecContext(ctx, `INSERT INTO accounts(id,type) VALUES(901,'oauth')`)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO intelligence_monitor_plans(id,name,source_type,account_id,created_by,enabled,candy_enabled,interval_seconds,candy_interval_seconds,next_run_at,candy_next_run_at,updated_at) VALUES
(401,'Active','openai_oauth',901,1,TRUE,TRUE,300,180,$1,$1,$1),
(402,'Paused','openai_oauth',901,1,FALSE,TRUE,300,180,NULL,NULL,$1),
(403,'Later','openai_oauth',901,1,TRUE,TRUE,600,900,$2,$2,$1),
(404,'No candy','openai_oauth',901,1,TRUE,FALSE,600,180,$1,NULL,$1),
(405,'Other source','external',901,1,TRUE,TRUE,300,180,$1,$1,$1),
(406,'Archived','openai_oauth',901,1,TRUE,TRUE,300,180,$1,$1,$1)`, before, later)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `UPDATE intelligence_monitor_plans SET deleted_at=NOW() WHERE id=406`)
	require.NoError(t, err)
	repo := &intelligenceMonitorRepository{db: db}
	for _, id := range []int64{401, 402, 403, 404, 405, 406} {
		require.NoError(t, repo.DeferOAuthMonitor(ctx, id, 901, until))
	}
	active, err := repo.GetPlan(ctx, 401)
	require.NoError(t, err)
	require.Equal(t, until, active.NextRunAt.UTC())
	require.Equal(t, until, active.CandyNextRunAt.UTC())
	require.True(t, active.Enabled)
	require.True(t, active.CandyEnabled)
	require.Equal(t, 300, active.IntervalSeconds)
	require.Equal(t, 180, active.CandyIntervalSeconds)
	require.Equal(t, before, active.UpdatedAt.UTC(), "polling cooldown must not change the plan revision or user settings")
	paused, err := repo.GetPlan(ctx, 402)
	require.NoError(t, err)
	require.False(t, paused.Enabled)
	require.Nil(t, paused.NextRunAt)
	require.Nil(t, paused.CandyNextRunAt)
	laterPlan, err := repo.GetPlan(ctx, 403)
	require.NoError(t, err)
	require.Equal(t, later, laterPlan.NextRunAt.UTC(), "a cooldown recheck must not shorten a later existing schedule")
	require.Equal(t, later, laterPlan.CandyNextRunAt.UTC())
	noCandy, err := repo.GetPlan(ctx, 404)
	require.NoError(t, err)
	require.Equal(t, until, noCandy.NextRunAt.UTC())
	require.Nil(t, noCandy.CandyNextRunAt)
	otherSource, err := repo.GetPlan(ctx, 405)
	require.NoError(t, err)
	require.Equal(t, before, otherSource.NextRunAt.UTC())
	var archivedNext time.Time
	require.NoError(t, db.QueryRowContext(ctx, `SELECT next_run_at FROM intelligence_monitor_plans WHERE id=406`).Scan(&archivedNext))
	require.Equal(t, before, archivedNext.UTC())
	require.NoError(t, repo.DeferOAuthMonitor(ctx, 401, 999, later))
	active, err = repo.GetPlan(ctx, 401)
	require.NoError(t, err)
	require.Equal(t, until, active.NextRunAt.UTC(), "a concurrent account rebind cannot defer a different account")
}
