package repository

import (
	"testing"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/stretchr/testify/require"
)

func TestIntelligenceAccountDeletePostgresIsAtomicThroughAccountRepository(t *testing.T) {
	db, ctx := intelligenceMonitorTestDB(t)
	_, err := db.ExecContext(ctx, `ALTER TABLE accounts ADD COLUMN updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW();
CREATE TABLE account_groups(account_id BIGINT NOT NULL,group_id BIGINT NOT NULL,priority INTEGER NOT NULL DEFAULT 0,created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),PRIMARY KEY(account_id,group_id));
CREATE TABLE scheduled_test_plans(id BIGINT PRIMARY KEY,account_id BIGINT NOT NULL);
INSERT INTO accounts(id) VALUES(901),(902);
INSERT INTO account_groups(account_id,group_id) VALUES(901,1),(902,1);
INSERT INTO scheduled_test_plans(id,account_id) VALUES(1,901),(2,902);
INSERT INTO intelligence_monitor_plans(id,name,source_type,account_id,created_by,model,deleted_at) VALUES
(1,'Astra','openai_oauth',901,1,'gpt-6-astra',NULL),
(2,'Sol','openai_oauth',901,1,'gpt-6.1-sol',NULL),
(3,'Archived','openai_oauth',901,1,'gpt-6-astra',NOW()),
(4,'Another account','openai_oauth',902,1,'gpt-6-astra',NULL);
CREATE FUNCTION reject_account_delete() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN RAISE EXCEPTION 'injected account delete failure'; END $$;
CREATE TRIGGER fail_account_delete BEFORE UPDATE OF deleted_at ON accounts FOR EACH ROW EXECUTE FUNCTION reject_account_delete();`)
	require.NoError(t, err)
	publicPelicanInsertRun(t, ctx, db, 1, "pelican", "openai_oauth", 1, "running")
	publicPelicanInsertRun(t, ctx, db, 2, "candy", "openai_oauth", 1, "pending")
	publicPelicanInsertRun(t, ctx, db, 3, "pelican", "openai_oauth", 1, "succeeded")
	client := dbent.NewClient(dbent.Driver(entsql.OpenDB(dialect.Postgres, db)))
	accounts := newAccountRepositoryWithSQL(client, nil, nil)
	require.Error(t, accounts.Delete(ctx, 901), "an account failure rolls back the earlier monitoring cleanup")
	var count int
	require.NoError(t, db.QueryRowContext(ctx, `SELECT COUNT(*) FROM intelligence_monitor_plans WHERE account_id=901`).Scan(&count))
	require.Equal(t, 3, count)
	require.NoError(t, db.QueryRowContext(ctx, `SELECT COUNT(*) FROM intelligence_monitor_runs`).Scan(&count))
	require.Equal(t, 3, count)
	require.NoError(t, db.QueryRowContext(ctx, `SELECT COUNT(*) FROM account_groups WHERE account_id=901`).Scan(&count))
	require.Equal(t, 1, count)
	_, err = db.ExecContext(ctx, `DROP TRIGGER fail_account_delete ON accounts`)
	require.NoError(t, err)
	require.NoError(t, accounts.Delete(ctx, 901))
	require.NoError(t, db.QueryRowContext(ctx, `SELECT COUNT(*) FROM accounts WHERE id=901 AND deleted_at IS NOT NULL`).Scan(&count))
	require.Equal(t, 1, count)
	require.NoError(t, db.QueryRowContext(ctx, `SELECT COUNT(*) FROM intelligence_monitor_plans WHERE account_id=901`).Scan(&count))
	require.Zero(t, count, "both models and archived plans are physically removed")
	require.NoError(t, db.QueryRowContext(ctx, `SELECT COUNT(*) FROM intelligence_monitor_runs`).Scan(&count))
	require.Zero(t, count, "HTML, raw output and active rows disappear with their account")
	require.NoError(t, db.QueryRowContext(ctx, `SELECT COUNT(*) FROM scheduled_test_plans WHERE account_id=901`).Scan(&count))
	require.Zero(t, count)
	require.NoError(t, db.QueryRowContext(ctx, `SELECT COUNT(*) FROM intelligence_monitor_plans WHERE account_id=902`).Scan(&count))
	require.Equal(t, 1, count, "another account is unaffected")
}
