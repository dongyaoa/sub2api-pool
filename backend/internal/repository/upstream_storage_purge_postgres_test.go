package repository

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/Wei-Shaw/sub2api/migrations"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// The optional integration test creates and removes only its isolated schema.
func TestUpstreamStoragePurgePostgres(t *testing.T) {
	dsn := os.Getenv("UPSTREAM_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("UPSTREAM_TEST_DATABASE_URL is not configured")
	}
	db, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	db.SetMaxOpenConns(1)
	defer func() { _ = db.Close() }()
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	schema := "upstream_purge_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	_, err = db.ExecContext(ctx, `CREATE SCHEMA "`+schema+`"`)
	require.NoError(t, err)
	defer func() { _, _ = db.ExecContext(context.Background(), `DROP SCHEMA "`+schema+`" CASCADE`) }()
	_, err = db.ExecContext(ctx, `SET search_path TO "`+schema+`"`)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `CREATE TABLE accounts(id BIGINT PRIMARY KEY,name VARCHAR(100) NOT NULL DEFAULT 'OAuth plan',credentials JSONB NOT NULL DEFAULT '{}',platform TEXT NOT NULL DEFAULT 'openai',type TEXT NOT NULL DEFAULT 'apikey',deleted_at TIMESTAMPTZ);
CREATE TABLE api_keys(id BIGINT PRIMARY KEY,user_id BIGINT NOT NULL,key TEXT NOT NULL,status TEXT NOT NULL DEFAULT 'active',updated_at TIMESTAMPTZ,deleted_at TIMESTAMPTZ);
CREATE TABLE usage_logs(id BIGSERIAL PRIMARY KEY,created_at TIMESTAMPTZ NOT NULL,account_id BIGINT NOT NULL,group_id BIGINT,user_id BIGINT NOT NULL DEFAULT 1,api_key_id BIGINT NOT NULL DEFAULT 1,requested_model TEXT,model TEXT NOT NULL DEFAULT 'model',request_id TEXT,actual_cost NUMERIC NOT NULL DEFAULT 0,total_cost NUMERIC NOT NULL DEFAULT 0,account_stats_cost NUMERIC,account_rate_multiplier NUMERIC,billing_type SMALLINT NOT NULL DEFAULT 0);`)
	require.NoError(t, err)
	for _, file := range []string{"242_upstream_center.sql", "243_upstream_finance.sql", "244_upstream_remote_billing.sql", "245_intelligence_monitor.sql", "246_intelligence_monitor_oauth.sql", "251_upstream_manual_order.sql", "253_upstream_newapi_credentials.sql", "254_upstream_storage_retention.sql"} {
		body, err := migrations.FS.ReadFile(file)
		require.NoError(t, err)
		_, err = db.ExecContext(ctx, string(body))
		require.NoError(t, err, file)
	}
	_, err = db.ExecContext(ctx, `INSERT INTO upstream_suppliers(id,name,deleted_at) VALUES(1,'Archive me',NOW()),(2,'Keep supplier',NULL);
INSERT INTO upstream_targets(id,supplier_id,name,provider,endpoint,api_key_encrypted,api_key_fingerprint,deleted_at) VALUES
(11,1,'Current child','openai','https://example.com','secret11','finger11',NOW()),
(12,2,'Moved out','openai','https://example.net','secret12','finger12',NULL),
(13,NULL,'Independent','openai','https://example.org','secret13','finger13',NULL);
INSERT INTO accounts(id) VALUES(101);
INSERT INTO api_keys(id,user_id,key) VALUES(201,301,'dedicated-key');
INSERT INTO upstream_account_bindings(target_id,account_id,supplier_id,target_name,valid_from,valid_until) VALUES
(11,101,2,'Historical old supplier',NOW()-INTERVAL '3 hours',NOW()-INTERVAL '2 hours'),
(11,101,1,'Current child',NOW()-INTERVAL '2 hours',NOW()-INTERVAL '1 hour'),
(12,101,1,'Moved out',NOW()-INTERVAL '1 hour',NOW()-INTERVAL '30 minutes'),
(12,101,2,'Moved out',NOW()-INTERVAL '30 minutes',NULL);
INSERT INTO upstream_monitor_history(target_id,supplier_id,target_name,model,status,checked_at,cost,cost_source) VALUES
(11,1,'Current child','m','operational',NOW(),2,'reported'),
(11,2,'Historical old supplier','m','operational',NOW()-INTERVAL '3 hours',3,'estimated'),
(12,1,'Moved out','m','operational',NOW()-INTERVAL '1 hour',5,'reported'),
(12,2,'Moved out','m','operational',NOW(),7,'reported'),
(13,NULL,'Independent','m','operational',NOW(),11,'reported');
INSERT INTO upstream_balance_snapshots(target_id,supplier_id,identity_hash,kind,status,synced_at) VALUES
(11,1,'id11','wallet','ok',NOW()),(11,2,'old11','wallet','ok',NOW()-INTERVAL '3 hours'),
(12,1,'old12','wallet','ok',NOW()-INTERVAL '1 hour'),(12,2,'id12','wallet','ok',NOW());
INSERT INTO upstream_billing_snapshots(target_id,identity_hash,status,source,data,attempted_at) VALUES
(11,'id11','ok','sub2api_billing','{}',NOW()),(12,'id12','ok','sub2api_billing','{}',NOW());
INSERT INTO upstream_finance_ledger(usage_id,created_at,target_id,target_name,supplier_id,account_id,user_id,api_key_id,model,revenue,business_cost,billing_type) VALUES
(1,NOW(),11,'Current child',1,101,301,201,'m',20,10,0),
(2,NOW(),12,'Moved out',2,101,301,201,'m',30,15,0);
INSERT INTO intelligence_monitor_plans(id,name,source_type,upstream_target_id,api_key_encrypted,created_by,enabled) VALUES
(21,'Retain art','upstream',11,'copied-secret',301,TRUE),
(22,'Other plan','upstream',12,'copied-other',301,FALSE);
INSERT INTO intelligence_monitor_plans(id,name,source_type,local_api_key_id,local_key_owner_id,created_by,deleted_at) VALUES(23,'Local plan','local_group',201,301,301,NOW());
INSERT INTO intelligence_monitor_plans(id,name,source_type,account_id,created_by,deleted_at) VALUES(24,'OAuth plan','openai_oauth',101,301,NOW());
INSERT INTO intelligence_monitor_runs(plan_id,plan_name,status,trigger,model,reasoning_effort,prompt,source_type,source_name,source_endpoint,api_mode,timeout_seconds,html,raw_text) VALUES
(21,'Retain art','succeeded','manual','m','high','draw','upstream','Current child','https://example.com','responses',300,'<html>retained</html>','raw retained'),
(23,'Local plan','succeeded','manual','m','high','draw','local_group','group','','responses',300,'<html>delete</html>','delete raw'),
(24,'OAuth plan','succeeded','manual','m','high','draw','openai_oauth','OAuth','','responses',300,'<html>oauth</html>','oauth raw');`)
	require.NoError(t, err)
	repo := &upstreamCenterRepository{db: db}
	page, err := repo.ListStorageArchives(ctx)
	require.NoError(t, err)
	require.Equal(t, int64(4), page.Total)
	count := func(query string, args ...any) int64 {
		t.Helper()
		var value int64
		require.NoError(t, db.QueryRowContext(ctx, query, args...).Scan(&value))
		return value
	}
	// A real balance lease or pending paid run must roll back the whole purge.
	_, err = db.ExecContext(ctx, `UPDATE upstream_targets SET balance_lease_until=NOW()+INTERVAL '1 minute' WHERE id=11`)
	require.NoError(t, err)
	_, err = repo.PurgeStorage(ctx, service.UpstreamStoragePurgeInput{Kind: "supplier", ID: 1, ConfirmName: "Archive me"})
	require.ErrorIs(t, err, service.ErrUpstreamStorageBusy)
	require.Equal(t, int64(5), count(`SELECT COUNT(*) FROM upstream_monitor_history`))
	_, err = db.ExecContext(ctx, `UPDATE upstream_targets SET balance_lease_until=NULL WHERE id=11;
UPDATE intelligence_monitor_runs SET status='pending' WHERE plan_id=21`)
	require.NoError(t, err)
	_, err = repo.PurgeStorage(ctx, service.UpstreamStoragePurgeInput{Kind: "supplier", ID: 1, ConfirmName: "Archive me"})
	require.ErrorIs(t, err, service.ErrUpstreamStorageBusy)
	require.Equal(t, int64(5), count(`SELECT COUNT(*) FROM upstream_monitor_history`))
	_, err = db.ExecContext(ctx, `UPDATE intelligence_monitor_runs SET status='succeeded' WHERE plan_id=21`)
	require.NoError(t, err)
	_, err = repo.PurgeStorage(ctx, service.UpstreamStoragePurgeInput{Kind: "supplier", ID: 1, ConfirmName: "Archive me"})
	require.NoError(t, err)
	require.Equal(t, int64(0), count(`SELECT COUNT(*) FROM upstream_suppliers WHERE id=1`))
	require.Equal(t, int64(0), count(`SELECT COUNT(*) FROM upstream_targets WHERE id=11`))
	require.Equal(t, int64(2), count(`SELECT COUNT(*) FROM upstream_targets`))
	require.Equal(t, int64(2), count(`SELECT COUNT(*) FROM upstream_monitor_history`))
	require.Equal(t, int64(0), count(`SELECT COUNT(*) FROM upstream_monitor_history WHERE supplier_id=1`))
	require.Equal(t, int64(10), count(`SELECT SUM(cost)::bigint FROM upstream_monitor_cost_rollups`))
	require.Equal(t, int64(7), count(`SELECT SUM(cost)::bigint FROM upstream_monitor_cost_rollups WHERE supplier_id=1`))
	require.Equal(t, int64(3), count(`SELECT SUM(cost)::bigint FROM upstream_monitor_cost_rollups WHERE supplier_id=2`))
	require.Equal(t, int64(1), count(`SELECT COUNT(*) FROM upstream_account_bindings WHERE target_id=12 AND supplier_id=2`))
	require.Equal(t, int64(1), count(`SELECT COUNT(*) FROM upstream_balance_snapshots WHERE target_id=12 AND supplier_id=2`))
	require.Equal(t, int64(1), count(`SELECT COUNT(*) FROM upstream_billing_snapshots WHERE target_id=12`))
	require.Equal(t, int64(2), count(`SELECT COUNT(*) FROM upstream_finance_ledger`))
	require.Equal(t, int64(1), count(`SELECT COUNT(*) FROM intelligence_monitor_plans WHERE id=21 AND NOT enabled AND next_run_at IS NULL AND upstream_target_id IS NULL AND api_key_encrypted=''`))
	require.Equal(t, int64(1), count(`SELECT COUNT(*) FROM intelligence_monitor_runs WHERE plan_id=21 AND html='<html>retained</html>'`))
	keys, err := repo.PurgeStorage(ctx, service.UpstreamStoragePurgeInput{Kind: "intelligence", ID: 23, ConfirmName: "Local plan"})
	require.NoError(t, err)
	require.Equal(t, []string{"dedicated-key"}, keys)
	require.Equal(t, int64(1), count(`SELECT COUNT(*) FROM api_keys WHERE id=201 AND status='disabled'`))
	require.Equal(t, int64(0), count(`SELECT COUNT(*) FROM intelligence_monitor_runs WHERE plan_id=23`))
	keys, err = repo.PurgeStorage(ctx, service.UpstreamStoragePurgeInput{Kind: "intelligence", ID: 24, ConfirmName: "OAuth plan"})
	require.NoError(t, err)
	require.Empty(t, keys)
	require.Equal(t, int64(1), count(`SELECT COUNT(*) FROM accounts WHERE id=101`))
	require.Equal(t, int64(0), count(`SELECT COUNT(*) FROM intelligence_monitor_runs WHERE plan_id=24`))
	_, err = repo.PurgeStorage(ctx, service.UpstreamStoragePurgeInput{Kind: "target", ID: 13, ConfirmName: "Independent"})
	require.NoError(t, err)
	require.Equal(t, int64(1), count(`SELECT COUNT(*) FROM upstream_targets`))
	require.Equal(t, int64(21), count(`SELECT SUM(cost)::bigint FROM upstream_monitor_cost_rollups`))
	require.Equal(t, int64(2), count(`SELECT COUNT(*) FROM upstream_finance_ledger`))
}

func TestUpstreamStoragePurgeOAuthCurrentAccountNamePostgres(t *testing.T) {
	db, ctx, otherDB := upstreamStorageTestDB(t)
	_, err := db.ExecContext(ctx, `ALTER TABLE accounts ADD COLUMN name VARCHAR(100) NOT NULL DEFAULT 'Account'`)
	require.NoError(t, err)
	for _, file := range []string{"245_intelligence_monitor.sql", "246_intelligence_monitor_oauth.sql"} {
		body, err := migrations.FS.ReadFile(file)
		require.NoError(t, err)
		_, err = db.ExecContext(ctx, string(body))
		require.NoError(t, err, file)
	}
	_, err = db.ExecContext(ctx, `INSERT INTO accounts(id,name,deleted_at) VALUES
(301,'Renamed OAuth',NULL),(302,'Deleted OAuth',NOW()),(303,'Removed OAuth',NULL);
INSERT INTO intelligence_monitor_plans(id,name,source_type,account_id,created_by,deleted_at) VALUES
(31,'Original active name','openai_oauth',301,1,NULL),
(32,'Original archive name','openai_oauth',301,1,NOW()),
(33,'Deleted account fallback','openai_oauth',302,1,NOW()),
(34,'Missing account fallback','openai_oauth',303,1,NOW());
DELETE FROM accounts WHERE id=303;
INSERT INTO intelligence_monitor_runs(plan_id,plan_name,status,trigger,model,reasoning_effort,prompt,source_type,source_name,source_endpoint,api_mode,timeout_seconds,html,raw_text) VALUES
(31,'Original active name','succeeded','manual','m','high','draw','openai_oauth','Original active name','','responses',300,'<html>artwork</html>','raw artwork');`)
	require.NoError(t, err)
	repo := &upstreamCenterRepository{db: db}
	archiveNames := func() map[int64]string {
		t.Helper()
		page, err := repo.ListStorageArchives(ctx)
		require.NoError(t, err)
		names := map[int64]string{}
		for _, item := range page.Items {
			if item.Kind == "intelligence" {
				names[item.ID] = item.Name
			}
		}
		return names
	}
	require.Equal(t, map[int64]string{32: "Renamed OAuth", 33: "Deleted account fallback", 34: "Missing account fallback"}, archiveNames())
	_, err = repo.PurgeStorage(ctx, service.UpstreamStoragePurgeInput{Kind: "intelligence", ID: 31, ConfirmName: "Original active name"})
	require.ErrorIs(t, err, service.ErrUpstreamStorageConfirm, "the stale stored plan name must not bypass the displayed account name")
	var retained int
	require.NoError(t, db.QueryRowContext(ctx, `SELECT COUNT(*) FROM intelligence_monitor_runs WHERE plan_id=31`).Scan(&retained))
	require.Equal(t, 1, retained)

	name := strings.Repeat("新", 100)
	_, err = db.ExecContext(ctx, `UPDATE accounts SET name=$1 WHERE id=301`, name)
	require.NoError(t, err)
	require.Equal(t, name, archiveNames()[32])

	// A concurrent account rename must not permit confirming an unstable name;
	// NOWAIT avoids a lock inversion with account deletion's foreign-key update.
	other := otherDB()
	rename, err := other.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer func() { _ = rename.Rollback() }()
	_, err = rename.ExecContext(ctx, `UPDATE accounts SET name='Concurrent rename' WHERE id=301`)
	require.NoError(t, err)
	_, err = repo.PurgeStorage(ctx, service.UpstreamStoragePurgeInput{Kind: "intelligence", ID: 32, ConfirmName: name})
	require.ErrorIs(t, err, service.ErrUpstreamStorageBusy)
	require.NoError(t, rename.Rollback())

	for _, item := range []struct {
		id   int64
		name string
	}{{31, name}, {32, name}, {33, "Deleted account fallback"}, {34, "Missing account fallback"}} {
		keys, err := repo.PurgeStorage(ctx, service.UpstreamStoragePurgeInput{Kind: "intelligence", ID: item.id, ConfirmName: item.name})
		require.NoError(t, err)
		require.Empty(t, keys)
	}
	require.Empty(t, archiveNames())
	require.NoError(t, db.QueryRowContext(ctx, `SELECT COUNT(*) FROM intelligence_monitor_runs`).Scan(&retained))
	require.Zero(t, retained)
	var accountName string
	require.NoError(t, db.QueryRowContext(ctx, `SELECT name FROM accounts WHERE id=301 AND deleted_at IS NULL`).Scan(&accountName))
	require.Equal(t, name, accountName, "removing monitoring must preserve its actual OAuth account")
}
