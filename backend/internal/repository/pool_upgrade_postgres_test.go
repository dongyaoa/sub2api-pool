package repository

import (
	"context"
	"database/sql"
	"io/fs"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/migrations"
	"github.com/google/uuid"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

// Export backend/migrations from the immutable pool-v0.2.7.20 commit into
// POOL_UPGRADE_BASE_MIGRATIONS_DIR. This optional regression uses the actual
// historical SQL and runner, including public-qualified legacy migrations, in
// its own disposable database. It never migrates the database named by the DSN.
func TestPoolUpgradePostgresFromPool20(t *testing.T) {
	dsn, baselineDir := os.Getenv("UPSTREAM_TEST_DATABASE_URL"), os.Getenv("POOL_UPGRADE_BASE_MIGRATIONS_DIR")
	if dsn == "" || baselineDir == "" {
		t.Skip("UPSTREAM_TEST_DATABASE_URL and POOL_UPGRADE_BASE_MIGRATIONS_DIR are required")
	}
	parsed, err := url.Parse(dsn)
	require.NoError(t, err)
	require.Contains(t, []string{"postgres", "postgresql"}, parsed.Scheme)
	baseline := os.DirFS(baselineDir)
	oldFiles, err := fs.Glob(baseline, "*.sql")
	require.NoError(t, err)
	require.Greater(t, len(oldFiles), 250, "must export the complete Pool 20 migration set")
	for _, name := range oldFiles {
		oldSQL, readErr := fs.ReadFile(baseline, name)
		require.NoError(t, readErr)
		currentSQL, readErr := migrations.FS.ReadFile(name)
		require.NoError(t, readErr, "published Pool migration must remain present: %s", name)
		require.Equal(t, strings.TrimSpace(string(oldSQL)), strings.TrimSpace(string(currentSQL)), "published migration changed: %s", name)
	}
	adminDB, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	t.Cleanup(func() { _ = adminDB.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	t.Cleanup(cancel)
	databaseName := "pool_upgrade_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	require.NotEqual(t, strings.Trim(parsed.Path, "/"), databaseName)
	_, err = adminDB.ExecContext(ctx, `CREATE DATABASE `+pq.QuoteIdentifier(databaseName))
	require.NoError(t, err)
	t.Cleanup(func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cleanupCancel()
		_, dropErr := adminDB.ExecContext(cleanupCtx, `DROP DATABASE `+pq.QuoteIdentifier(databaseName))
		require.NoError(t, dropErr, "remove only the test database; never terminate other sessions")
	})
	parsed.Path = "/" + databaseName
	db, err := sql.Open("postgres", parsed.String())
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, applyMigrationsFS(ctx, db, baseline))
	var originalMigrationCount int
	require.NoError(t, db.QueryRowContext(ctx, `SELECT count(*) FROM schema_migrations`).Scan(&originalMigrationCount))
	require.Equal(t, len(oldFiles), originalMigrationCount)

	// Pool records and official legacy pricing coexist before the upgrade.
	_, err = db.ExecContext(ctx, `
INSERT INTO users (id,email,password_hash) VALUES (91001,'pool-upgrade@example.test','test');
INSERT INTO proxies (id,name,protocol,host,port) VALUES (91001,'Pool proxy','http','127.0.0.1',8080);
INSERT INTO accounts (id,name,platform,type,credentials,proxy_id,extra) VALUES
 (91001,'Pool account','openai','oauth','{"access_token":"test"}',91001,'{"codex_turn_ticket:gpt-test":{"state":"preserved"}}');
INSERT INTO upstream_suppliers (id,name) VALUES (91001,'Pool supplier');
INSERT INTO intelligence_monitor_plans (id,name,source_type,created_by) VALUES (91001,'Pool monitor','external',91001);
INSERT INTO upstream_finance_ledger
 (usage_id,created_at,target_id,target_name,account_id,user_id,api_key_id,model,revenue,business_cost,billing_type)
 VALUES (91001,NOW(),91001,'Pool target',91001,91001,91001,'test-model',5.25,2.75,0);
INSERT INTO channels (id,name) VALUES (91001,'Pool channel');
INSERT INTO channel_model_pricing (id,channel_id,models,max_reasoning_effort_multiplier)
 VALUES (91001,91001,'["test-model"]',2.5),(91002,91001,'["unconfigured-model"]',NULL);
INSERT INTO groups (id,name,platform,model_pricing) VALUES (91001,'Pool group','anthropic',
 '[{"models":["test-model"],"max_reasoning_effort_multiplier":3},{"models":["existing-map"],"max_reasoning_effort_multiplier":4,"reasoning_effort_multipliers":{}}]');
INSERT INTO user_affiliate_ledger (user_id,action,amount) VALUES (91001,'transfer',1.25);`)
	require.NoError(t, err)
	require.NoError(t, ApplyMigrations(ctx, db))
	var migratedCount int
	require.NoError(t, db.QueryRowContext(ctx, `SELECT count(*) FROM schema_migrations`).Scan(&migratedCount))
	newFiles, err := fs.Glob(migrations.FS, "*.sql")
	require.NoError(t, err)
	require.Equal(t, len(newFiles), migratedCount)
	require.Greater(t, migratedCount, originalMigrationCount)
	for _, name := range []string{"238b_content_moderation_engine_meta.sql", "239_channel_reasoning_effort_multipliers.sql", "240_affiliate_ledger_operation_id.sql", "239_openai_auto_reauth.sql", "240_openai_auto_reauth_stage.sql"} {
		var count int
		require.NoError(t, db.QueryRowContext(ctx, `SELECT count(*) FROM schema_migrations WHERE filename=$1`, name).Scan(&count))
		require.Equal(t, 1, count, "numeric prefixes must not suppress distinct migration filenames")
	}
	for table, where := range map[string]string{
		"proxies":                    "id=91001 AND name='Pool proxy'",
		"accounts":                   "id=91001 AND proxy_id=91001 AND extra->'codex_turn_ticket:gpt-test'->>'state'='preserved'",
		"upstream_suppliers":         "id=91001 AND name='Pool supplier'",
		"intelligence_monitor_plans": "id=91001 AND name='Pool monitor'",
		"upstream_finance_ledger":    "usage_id=91001 AND revenue=5.25 AND business_cost=2.75",
		"user_affiliate_ledger":      "user_id=91001 AND amount=1.25 AND operation_id IS NULL",
	} {
		var count int
		require.NoError(t, db.QueryRowContext(ctx, `SELECT count(*) FROM `+pq.QuoteIdentifier(table)+` WHERE `+where).Scan(&count))
		require.Equal(t, 1, count, "upgrade must preserve existing %s records", table)
	}
	var columnType, pricing, unsetPricing, groupPricing string
	require.NoError(t, db.QueryRowContext(ctx, `SELECT data_type FROM information_schema.columns WHERE table_schema='public' AND table_name='content_moderation_logs' AND column_name='engine_meta'`).Scan(&columnType))
	require.Equal(t, "jsonb", columnType)
	require.NoError(t, db.QueryRowContext(ctx, `SELECT reasoning_effort_multipliers::text FROM channel_model_pricing WHERE id=91001`).Scan(&pricing))
	require.JSONEq(t, `{"max":2.5}`, pricing)
	require.NoError(t, db.QueryRowContext(ctx, `SELECT reasoning_effort_multipliers::text FROM channel_model_pricing WHERE id=91002`).Scan(&unsetPricing))
	require.JSONEq(t, `{}`, unsetPricing)
	require.NoError(t, db.QueryRowContext(ctx, `SELECT model_pricing::text FROM groups WHERE id=91001`).Scan(&groupPricing))
	require.JSONEq(t, `[{"models":["test-model"],"reasoning_effort_multipliers":{"max":3}},{"models":["existing-map"],"reasoning_effort_multipliers":{}}]`, groupPricing)
	_, err = db.ExecContext(ctx, `INSERT INTO user_affiliate_ledger (user_id,action,amount,operation_id) VALUES (91001,'withdraw',2,'pool-operation')`)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO user_affiliate_ledger (user_id,action,amount,operation_id) VALUES (91001,'withdraw',2,'pool-operation')`)
	var pgErr *pq.Error
	require.ErrorAs(t, err, &pgErr)
	require.Equal(t, pq.ErrorCode("23505"), pgErr.Code)

	// Startup replay must preserve newer administrator pricing choices.
	_, err = db.ExecContext(ctx, `UPDATE channel_model_pricing SET reasoning_effort_multipliers='{}' WHERE id=91001`)
	require.NoError(t, err)
	require.NoError(t, ApplyMigrations(ctx, db))
	require.NoError(t, db.QueryRowContext(ctx, `SELECT reasoning_effort_multipliers::text FROM channel_model_pricing WHERE id=91001`).Scan(&pricing))
	require.JSONEq(t, `{}`, pricing)
}
