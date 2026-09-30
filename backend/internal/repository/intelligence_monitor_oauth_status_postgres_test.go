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
CREATE TABLE groups(id BIGINT PRIMARY KEY,name TEXT NOT NULL,status TEXT NOT NULL DEFAULT 'active',deleted_at TIMESTAMPTZ);
CREATE TABLE account_groups(account_id BIGINT NOT NULL,group_id BIGINT NOT NULL,PRIMARY KEY(account_id,group_id));
INSERT INTO accounts(id,type,credentials,extra) VALUES
(501,'oauth','{"email":"current@example.com","chatgpt_account_id":"identity-501","workspace_id":"workspace-501","access_token":"never-load-access","refresh_token":"never-load-refresh","id_token":"never-load-id"}','{"codex_7d_used_percent":100,"codex_7d_reset_at":"2030-10-01T12:00:00Z","email":"current@example.com"}'),
(502,'oauth','{}',NULL),
(503,'oauth','{}','{}');
UPDATE accounts SET deleted_at=NOW() WHERE id=503;
INSERT INTO groups(id,name,status,deleted_at) VALUES
(10,'Primary group','active',NULL),(11,'Paused group','inactive',NULL),(12,'Deleted group','active',NOW()),(13,'Rebound group','active',NULL);
INSERT INTO account_groups(account_id,group_id) VALUES(501,11),(501,10),(501,12),(503,13);`)
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
	require.Len(t, account.Groups, 2, "all current bindings are returned, including inactive but excluding soft-deleted groups")
	require.Equal(t, int64(10), account.Groups[0].ID)
	require.Equal(t, "Primary group", account.Groups[0].Name)
	require.Equal(t, int64(11), account.Groups[1].ID)
	require.Equal(t, "Paused group", account.Groups[1].Name)
	require.Nil(t, accounts[502].Extra)
	require.Empty(t, accounts[502].TempUnschedulableReason)
	require.NotNil(t, accounts[502].Groups, "unassigned accounts use an empty array")
	require.Empty(t, accounts[502].Groups)
	_, err = db.ExecContext(ctx, `UPDATE groups SET name='Renamed group' WHERE id=10;
DELETE FROM account_groups WHERE account_id=501 AND group_id=11;
INSERT INTO account_groups(account_id,group_id) VALUES(502,13);`)
	require.NoError(t, err)
	accounts, err = repo.LoadOAuthMonitorAccounts(ctx, []int64{501, 502})
	require.NoError(t, err)
	require.Len(t, accounts[501].Groups, 1)
	require.Equal(t, "Renamed group", accounts[501].Groups[0].Name, "a list refresh follows group renames without changing the monitoring plan")
	require.Len(t, accounts[502].Groups, 1)
	require.Equal(t, int64(13), accounts[502].Groups[0].ID, "a list refresh follows the account's current bindings")
	require.Equal(t, float64(100), accounts[501].Extra["codex_7d_used_percent"], "group changes preserve quota status")
	_, err = db.ExecContext(ctx, `UPDATE groups SET deleted_at=NOW() WHERE id=10;
DELETE FROM account_groups WHERE account_id=502`)
	require.NoError(t, err)
	accounts, err = repo.LoadOAuthMonitorAccounts(ctx, []int64{501, 502})
	require.NoError(t, err)
	require.Empty(t, accounts[501].Groups, "soft deletion removes a group even while the relation remains")
	require.Empty(t, accounts[502].Groups, "removing all bindings clears the previous group list")
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
