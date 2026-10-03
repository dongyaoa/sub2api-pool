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

// Export backend/migrations from the immutable Pool 21 release commit into
// POOL_UPGRADE_POOL21_MIGRATIONS_DIR. Historical migrations run only in a newly
// created disposable database; the database in the supplied DSN is not migrated.
func TestPoolUpgradePostgresFromPool21(t *testing.T) {
	dsn, baselineDir := os.Getenv("UPSTREAM_TEST_DATABASE_URL"), os.Getenv("POOL_UPGRADE_POOL21_MIGRATIONS_DIR")
	if dsn == "" || baselineDir == "" {
		t.Skip("UPSTREAM_TEST_DATABASE_URL and POOL_UPGRADE_POOL21_MIGRATIONS_DIR are required")
	}
	baseline := os.DirFS(baselineDir)
	oldFiles, err := fs.Glob(baseline, "*.sql")
	require.NoError(t, err)
	require.Greater(t, len(oldFiles), 300, "export the complete Pool 21 migration set")
	for _, name := range oldFiles {
		oldSQL, readErr := fs.ReadFile(baseline, name)
		require.NoError(t, readErr)
		currentSQL, readErr := migrations.FS.ReadFile(name)
		require.NoError(t, readErr)
		require.Equal(t, strings.TrimSpace(string(oldSQL)), strings.TrimSpace(string(currentSQL)), "published migration changed: %s", name)
	}
	parsed, err := url.Parse(dsn)
	require.NoError(t, err)
	require.Contains(t, []string{"postgres", "postgresql"}, parsed.Scheme)
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
		require.NoError(t, dropErr, "remove only this isolated database without terminating other sessions")
	})
	parsed.Path = "/" + databaseName
	db, err := sql.Open("postgres", parsed.String())
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, applyMigrationsFS(ctx, db, baseline))

	// Preserve active, archived and non-OAuth monitoring while cleaning only
	// OAuth plans whose source account has already disappeared or been deleted.
	_, err = db.ExecContext(ctx, `
INSERT INTO users (id,email,password_hash) VALUES (92001,'pool21-upgrade@example.test','test');
INSERT INTO proxies (id,name,protocol,host,port) VALUES (92001,'Pool proxy','http','127.0.0.1',8080);
INSERT INTO accounts (id,name,platform,type,credentials,proxy_id,extra,deleted_at) VALUES
 (92001,'Live OAuth','openai','oauth','{"access_token":"test"}',92001,'{"codex_turn_ticket:gpt-test":{"state":"preserved"}}',NULL),
 (92002,'Deleted OAuth','openai','oauth','{}',NULL,'{}',NOW());
INSERT INTO groups (id,name,platform) VALUES (92001,'Pool group','openai');
INSERT INTO user_platform_quotas(user_id,platform,daily_limit_usd,daily_usage_usd) VALUES (92001,'openai',20,3);
INSERT INTO composite_model_routes(group_id,public_model,target_platform) VALUES (92001,'existing-model','openai');
INSERT INTO payment_orders(id,user_id,amount,pay_amount,status,expires_at) VALUES (92001,92001,25,25,'COMPLETED',NOW());
INSERT INTO intelligence_monitor_plans (id,name,source_type,account_id,created_by,deleted_at) VALUES
 (92001,'Active OAuth','openai_oauth',92001,92001,NULL),
 (92002,'Archived live OAuth','openai_oauth',92001,92001,NOW()),
 (92003,'Deleted account OAuth','openai_oauth',92002,92001,NULL),
 (92004,'Orphan OAuth','openai_oauth',NULL,92001,NOW()),
 (92005,'External','external',NULL,92001,NULL);
INSERT INTO intelligence_monitor_runs (id,plan_id,plan_name,status,trigger,model,reasoning_effort,prompt,source_type,source_name,source_endpoint,api_mode,timeout_seconds,html,raw_text,test_kind)
 SELECT id,id,name,'succeeded','manual','gpt-6-astra','high','original prompt',source_type,name,'','responses',600,'<html>preserved</html>','original output','pelican'
 FROM intelligence_monitor_plans;
INSERT INTO upstream_finance_ledger (usage_id,created_at,target_id,target_name,account_id,user_id,api_key_id,model,revenue,business_cost,billing_type)
 VALUES (92001,NOW(),92001,'Pool target',92001,92001,92001,'test-model',5.25,2.75,0);`)
	require.NoError(t, err)
	require.NoError(t, ApplyMigrations(ctx, db))
	newFiles, err := fs.Glob(migrations.FS, "*.sql")
	require.NoError(t, err)
	var count int
	require.NoError(t, db.QueryRowContext(ctx, `SELECT count(*) FROM schema_migrations`).Scan(&count))
	require.Equal(t, len(newFiles), count)
	require.Greater(t, len(newFiles), len(oldFiles))
	for _, name := range []string{"241_add_payment_order_bonus_amount.sql", "241_add_typesafe_platform.sql", "241_remove_openai_auto_reauth.sql", "262_intelligence_monitor_models.sql", "263_intelligence_deleted_oauth_cleanup.sql"} {
		require.NoError(t, db.QueryRowContext(ctx, `SELECT count(*) FROM schema_migrations WHERE filename=$1`, name).Scan(&count))
		require.Equal(t, 1, count, "same-number official and Pool migrations must all run: %s", name)
	}
	for table, where := range map[string]string{
		"accounts":                "id=92001 AND proxy_id=92001 AND extra->'codex_turn_ticket:gpt-test'->>'state'='preserved'",
		"payment_orders":          "id=92001 AND amount=25 AND pay_amount=25 AND status='COMPLETED' AND bonus_amount=0",
		"user_platform_quotas":    "user_id=92001 AND platform='openai' AND daily_limit_usd=20 AND daily_usage_usd=3",
		"composite_model_routes":  "group_id=92001 AND public_model='existing-model' AND target_platform='openai'",
		"upstream_finance_ledger": "usage_id=92001 AND revenue=5.25 AND business_cost=2.75",
	} {
		require.NoError(t, db.QueryRowContext(ctx, `SELECT count(*) FROM `+pq.QuoteIdentifier(table)+` WHERE `+where).Scan(&count))
		require.Equal(t, 1, count, "upgrade preserves %s", table)
	}
	require.NoError(t, db.QueryRowContext(ctx, `SELECT count(*) FROM intelligence_monitor_plans WHERE id IN (92001,92002,92005) AND model='gpt-6-astra'`).Scan(&count))
	require.Equal(t, 3, count)
	require.NoError(t, db.QueryRowContext(ctx, `SELECT count(*) FROM intelligence_monitor_runs WHERE plan_id IN (92001,92002,92005) AND model='gpt-6-astra' AND prompt='original prompt' AND raw_text='original output' AND html='<html>preserved</html>'`).Scan(&count))
	require.Equal(t, 3, count)
	for _, table := range []string{"intelligence_monitor_plans", "intelligence_monitor_runs"} {
		require.NoError(t, db.QueryRowContext(ctx, `SELECT count(*) FROM `+pq.QuoteIdentifier(table)+` WHERE id IN (92003,92004)`).Scan(&count))
		require.Zero(t, count, "deleted-source OAuth data is removed from %s", table)
	}
	_, err = db.ExecContext(ctx, `INSERT INTO user_platform_quotas(user_id,platform) VALUES (92001,'typesafe');
INSERT INTO composite_model_routes(group_id,public_model,target_platform) VALUES (92001,'systemone','typesafe');
UPDATE payment_orders SET bonus_amount=5 WHERE id=92001;
UPDATE intelligence_monitor_plans SET model='gpt-6.1-sol' WHERE id=92001;`)
	require.NoError(t, err, "new platform constraints and bonus/model fields accept valid updates")
	require.NoError(t, ApplyMigrations(ctx, db), "restart replay remains idempotent")
	var model string
	var bonus float64
	require.NoError(t, db.QueryRowContext(ctx, `SELECT model FROM intelligence_monitor_plans WHERE id=92001`).Scan(&model))
	require.Equal(t, "gpt-6.1-sol", model)
	require.NoError(t, db.QueryRowContext(ctx, `SELECT bonus_amount FROM payment_orders WHERE id=92001`).Scan(&bonus))
	require.Equal(t, 5.0, bonus)
}
