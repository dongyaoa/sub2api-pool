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

func upstreamStorageTestDB(t *testing.T) (*sql.DB, context.Context, func() *sql.DB) {
	t.Helper()
	dsn := os.Getenv("UPSTREAM_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("UPSTREAM_TEST_DATABASE_URL is not configured")
	}
	db, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	t.Cleanup(cancel)
	schema := "upstream_storage_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	_, err = db.ExecContext(ctx, `CREATE SCHEMA `+pqQuoteIdentifier(schema))
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), `DROP SCHEMA `+pqQuoteIdentifier(schema)+` CASCADE`)
	})
	_, err = db.ExecContext(ctx, `SET search_path TO `+pqQuoteIdentifier(schema))
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `CREATE TABLE accounts(id BIGINT PRIMARY KEY,credentials JSONB NOT NULL DEFAULT '{}',platform TEXT NOT NULL DEFAULT 'openai',type TEXT NOT NULL DEFAULT 'apikey',deleted_at TIMESTAMPTZ);
CREATE TABLE usage_logs(id BIGSERIAL PRIMARY KEY,created_at TIMESTAMPTZ NOT NULL,account_id BIGINT,group_id BIGINT,user_id BIGINT,api_key_id BIGINT,requested_model TEXT,model TEXT,request_id TEXT,actual_cost NUMERIC,total_cost NUMERIC,account_stats_cost NUMERIC,account_rate_multiplier NUMERIC,billing_type SMALLINT,input_tokens INT NOT NULL DEFAULT 0,output_tokens INT NOT NULL DEFAULT 0,cache_creation_tokens INT NOT NULL DEFAULT 0,cache_read_tokens INT NOT NULL DEFAULT 0)`)
	require.NoError(t, err)
	for _, name := range []string{"242_upstream_center.sql", "243_upstream_finance.sql", "244_upstream_remote_billing.sql", "247_upstream_finance_usage_totals.sql", "253_upstream_newapi_credentials.sql", "254_upstream_storage_retention.sql", "254_upstream_storage_retention.sql"} {
		migration, err := migrations.FS.ReadFile(name)
		require.NoError(t, err)
		_, err = db.ExecContext(ctx, string(migration))
		require.NoError(t, err, name)
	}
	_, err = db.ExecContext(ctx, `INSERT INTO upstream_suppliers(id,name) VALUES(1,'First'),(2,'Second');
INSERT INTO upstream_targets(id,supplier_id,name,provider,endpoint,api_key_encrypted,api_key_fingerprint) VALUES
(1,1,'Supplier key','openai','https://example.com/v1','cipher','first'),
(2,NULL,'Independent','openai','https://example.com/v1','cipher-two','second'),
(3,2,'Archived','openai','https://other.example.com/v1','cipher-three','third');
UPDATE upstream_targets SET deleted_at=NOW(),enabled=FALSE WHERE id=3`)
	require.NoError(t, err)
	otherDB := func() *sql.DB {
		other, err := sql.Open("postgres", dsn)
		require.NoError(t, err)
		other.SetMaxOpenConns(1)
		t.Cleanup(func() { _ = other.Close() })
		_, err = other.ExecContext(ctx, `SET search_path TO `+pqQuoteIdentifier(schema))
		require.NoError(t, err)
		return other
	}
	return db, ctx, otherDB
}

func TestUpstreamStorageHistoryRollupPreservesFinanceAndBoundedRetention(t *testing.T) {
	db, ctx, _ := upstreamStorageTestDB(t)
	now := time.Date(2026, 9, 26, 13, 30, 0, 0, time.UTC)
	old := now.AddDate(0, 0, -40).Truncate(time.Hour)
	_, err := db.ExecContext(ctx, `INSERT INTO upstream_monitor_history(target_id,supplier_id,target_name,model,status,checked_at,cost,cost_source)
SELECT 1,1,'Supplier key','test','operational',$1::timestamptz+make_interval(secs=>n%3600),0.1,'estimated' FROM generate_series(1,6001) n`, old)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO upstream_monitor_history(target_id,supplier_id,target_name,model,status,checked_at,cost,cost_source) VALUES
(1,1,'Supplier key','test','operational',$1::timestamptz+INTERVAL '10 seconds',2,'reported'),
(2,NULL,'Independent','test','error',$1::timestamptz+INTERVAL '20 seconds',NULL,'unknown'),
(3,2,'Archived','test','operational',$1::timestamptz+INTERVAL '30 seconds',4,'reported'),
(1,1,'Supplier key','test','operational',$2,3,'estimated')`, old, now.AddDate(0, 0, -30).Add(-time.Minute))
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO upstream_finance_ledger(usage_id,created_at,target_id,target_name,supplier_id,account_id,user_id,api_key_id,model,revenue,business_cost,billing_type,total_tokens)
VALUES(99,$1,1,'Supplier key',1,1,1,1,'test',1000,100,0,9000)`, old)
	require.NoError(t, err)
	finance := &upstreamFinanceRepository{db: db}
	supplier, independent := int64(1), int64(2)
	queries := []service.UpstreamFinanceQuery{
		{From: old, To: old.Add(time.Hour)},
		{From: old, To: old.Add(time.Hour), SupplierID: &supplier},
		{From: old, To: old.Add(time.Hour), TargetID: &independent},
		{From: old, To: now.Add(time.Hour)},
	}
	before := []*service.UpstreamFinanceSummary{}
	for _, q := range queries {
		value, err := finance.Summary(ctx, q)
		require.NoError(t, err)
		before = append(before, value)
	}
	repo := &upstreamCenterRepository{db: db}
	first, err := repo.CleanupStorage(ctx, 30, 7, now)
	require.NoError(t, err)
	require.Equal(t, int64(5000), first.HistoryDeleted)
	require.True(t, first.HasMore)
	for i, q := range queries {
		value, err := finance.Summary(ctx, q)
		require.NoError(t, err)
		require.Equal(t, before[i], value, "mixed raw and archived rows must preserve totals")
	}
	second, err := repo.CleanupStorage(ctx, 30, 7, now)
	require.NoError(t, err)
	require.Equal(t, int64(1004), second.HistoryDeleted)
	require.False(t, second.HasMore)
	third, err := repo.CleanupStorage(ctx, 30, 7, now)
	require.NoError(t, err)
	require.Zero(t, third.HistoryDeleted)
	for i, q := range queries {
		value, err := finance.Summary(ctx, q)
		require.NoError(t, err)
		require.Equal(t, before[i], value)
	}
	var raw, samples int64
	require.NoError(t, db.QueryRowContext(ctx, `SELECT COUNT(*) FROM upstream_monitor_history`).Scan(&raw))
	require.Equal(t, int64(1), raw, "retain the full cutoff hour for exact 30-day statistics")
	require.NoError(t, db.QueryRowContext(ctx, `SELECT SUM(sample_count) FROM upstream_monitor_cost_rollups`).Scan(&samples))
	require.Equal(t, int64(6004), samples)
	require.NoError(t, db.QueryRowContext(ctx, `SELECT COUNT(*) FROM upstream_finance_ledger`).Scan(&samples))
	require.Equal(t, int64(1), samples, "cleanup must never delete the business finance ledger")
	for _, q := range []service.UpstreamFinanceQuery{
		{From: old.Add(time.Minute), To: old.Add(time.Hour)},
		{From: old, To: old.Add(30 * time.Minute)},
		{From: old.Add(time.Minute), To: old.Add(30 * time.Minute), SupplierID: &supplier},
	} {
		_, err := finance.Summary(ctx, q)
		require.ErrorIs(t, err, service.ErrUpstreamFinanceArchivedRange)
	}
	missing := int64(999)
	_, err = finance.Summary(ctx, service.UpstreamFinanceQuery{From: old.Add(time.Minute), To: old.Add(30 * time.Minute), TargetID: &missing})
	require.NoError(t, err, "partial boundaries only fail when they intersect matching archived facts")
}

func TestUpstreamStorageRollupSampleBoundsAllowCurrentHourQueries(t *testing.T) {
	db, ctx, _ := upstreamStorageTestDB(t)
	hour := time.Date(2026, 9, 26, 13, 0, 0, 0, time.UTC)
	now := hour.Add(30 * time.Minute)
	finance := &upstreamFinanceRepository{db: db}
	// Separate batches must extend both ends of an existing hourly aggregate.
	for _, minute := range []int{15, 10, 20} {
		_, err := db.ExecContext(ctx, `INSERT INTO upstream_monitor_history(target_id,supplier_id,target_name,model,status,checked_at,cost,cost_source) VALUES(1,1,'Supplier key','test','operational',$1,2,'reported')`, hour.Add(time.Duration(minute)*time.Minute))
		require.NoError(t, err)
		tx, err := db.BeginTx(ctx, nil)
		require.NoError(t, err)
		deleted, err := rollupUpstreamMonitorHistory(ctx, tx, `target_id=$1`, int64(1))
		require.NoError(t, err)
		require.Equal(t, int64(1), deleted)
		require.NoError(t, tx.Commit())
	}
	var first, last time.Time
	require.NoError(t, db.QueryRowContext(ctx, `SELECT first_sample_at,last_sample_at FROM upstream_monitor_cost_rollups`).Scan(&first, &last))
	require.True(t, first.Equal(hour.Add(10*time.Minute)))
	require.True(t, last.Equal(hour.Add(20*time.Minute)))
	for _, q := range []service.UpstreamFinanceQuery{
		{From: hour, To: now},
		{From: first, To: now},
		{From: hour, To: hour.Add(time.Hour)},
		{From: first, To: last.Add(time.Microsecond)},
	} {
		value, err := finance.Summary(ctx, q)
		require.NoError(t, err)
		require.Equal(t, float64(6), *value.MonitorCost)
		require.Equal(t, float64(-6), *value.Profit)
	}
	for _, q := range []service.UpstreamFinanceQuery{
		{From: hour, To: first},
		{From: last.Add(time.Microsecond), To: now},
	} {
		value, err := finance.Summary(ctx, q)
		require.NoError(t, err)
		require.Zero(t, *value.MonitorCost, "same-hour windows wholly outside stored samples must remain exactly queryable")
	}
	for _, q := range []service.UpstreamFinanceQuery{
		{From: first.Add(time.Microsecond), To: now},
		{From: hour, To: last},
		{From: last, To: now},
		{From: hour.Add(12 * time.Minute), To: hour.Add(18 * time.Minute)},
	} {
		_, err := finance.Summary(ctx, q)
		require.ErrorIs(t, err, service.ErrUpstreamFinanceArchivedRange)
	}
}

func TestUpstreamStorageRollupTransactionRollbackAndNullSupplier(t *testing.T) {
	db, ctx, _ := upstreamStorageTestDB(t)
	_, err := db.ExecContext(ctx, `SET TIME ZONE 'Asia/Kathmandu'; INSERT INTO upstream_monitor_history(target_id,supplier_id,target_name,model,status,checked_at,cost,cost_source) VALUES (2,NULL,'Independent','test','operational','2026-01-01T01:20:00Z',2,'reported')`)
	require.NoError(t, err)
	tx, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	count, err := rollupUpstreamMonitorHistory(ctx, tx, `target_id=$1`, int64(2))
	require.NoError(t, err)
	require.Equal(t, int64(1), count)
	require.NoError(t, tx.Rollback())
	var n int
	require.NoError(t, db.QueryRowContext(ctx, `SELECT COUNT(*) FROM upstream_monitor_history`).Scan(&n))
	require.Equal(t, 1, n)
	require.NoError(t, db.QueryRowContext(ctx, `SELECT COUNT(*) FROM upstream_monitor_cost_rollups`).Scan(&n))
	require.Zero(t, n)
	for range 2 {
		tx, err = db.BeginTx(ctx, nil)
		require.NoError(t, err)
		_, err = rollupUpstreamMonitorHistory(ctx, tx, `target_id=$1`, int64(2))
		require.NoError(t, err)
		require.NoError(t, tx.Commit())
		_, err = db.ExecContext(ctx, `INSERT INTO upstream_monitor_history(target_id,supplier_id,target_name,model,status,checked_at,cost,cost_source) VALUES (2,NULL,'Independent','test','operational','2026-01-01T01:30:00Z',2,'reported')`)
		require.NoError(t, err)
	}
	var hour time.Time
	var cost float64
	var samples int64
	require.NoError(t, db.QueryRowContext(ctx, `SELECT hour_start,cost,sample_count FROM upstream_monitor_cost_rollups WHERE supplier_id IS NULL`).Scan(&hour, &cost, &samples))
	require.True(t, hour.Equal(time.Date(2026, 1, 1, 1, 0, 0, 0, time.UTC)))
	require.Equal(t, float64(4), cost)
	require.Equal(t, int64(2), samples)
}

func TestUpstreamStorageSnapshotProtectionCurrentIdentityAndBounds(t *testing.T) {
	db, ctx, _ := upstreamStorageTestDB(t)
	now := time.Date(2026, 9, 26, 13, 30, 0, 0, time.UTC)
	_, err := db.ExecContext(ctx, `UPDATE upstream_targets SET newapi_user_id=42,newapi_access_token_encrypted='console-cipher' WHERE id=1`)
	require.NoError(t, err)
	finance := &upstreamFinanceRepository{db: db}
	target, err := finance.GetTarget(ctx, 1)
	require.NoError(t, err)
	identity := service.UpstreamBalanceIdentity(target)
	for _, id := range []int64{1, 2, 3} {
		var sqlIdentity string
		require.NoError(t, db.QueryRowContext(ctx, `SELECT upstream_storage_identity_hash(provider,endpoint,api_key_encrypted,supplier_id,wallet_ref,newapi_user_id,newapi_access_token_encrypted) FROM upstream_targets WHERE id=$1`, id).Scan(&sqlIdentity))
		var target service.UpstreamFinanceTarget
		require.NoError(t, db.QueryRowContext(ctx, `SELECT id,supplier_id,provider,endpoint,api_key_encrypted,wallet_ref,newapi_user_id,newapi_access_token_encrypted FROM upstream_targets WHERE id=$1`, id).Scan(&target.ID, &target.SupplierID, &target.Provider, &target.Endpoint, &target.APIKeyEncrypted, &target.WalletRef, &target.NewAPIUserID, &target.NewAPIAccessTokenEncrypted))
		require.Equal(t, service.UpstreamBalanceIdentity(&target), sqlIdentity)
	}
	for _, table := range []string{"upstream_balance_snapshots", "upstream_billing_snapshots"} {
		timestamp, columns, values := "synced_at", "kind,status,balance", "'wallet','ok',10"
		if table == "upstream_billing_snapshots" {
			timestamp, columns, values = "attempted_at", "status,source,data", "'ok','newapi_account','{}'::jsonb"
		}
		_, err = db.ExecContext(ctx, `INSERT INTO `+table+`(target_id,identity_hash,`+timestamp+`,`+columns+`) SELECT 1,$1,$2::timestamptz+make_interval(secs=>n),`+values+` FROM generate_series(1,5102) n`, identity, now.AddDate(0, 0, -20))
		require.NoError(t, err)
		_, err = db.ExecContext(ctx, `INSERT INTO `+table+`(target_id,identity_hash,`+timestamp+`,`+columns+`) VALUES (1,'old-key',$1,`+values+`),(1,'old-key-recent',$2,`+values+`),(1,$3,$4,`+values+`)`, now.AddDate(0, 0, -15), now.Add(-time.Hour), identity, now.AddDate(0, 0, -10))
		require.NoError(t, err)
		_, err = db.ExecContext(ctx, `UPDATE `+table+` SET status='error' WHERE identity_hash=$1 AND `+timestamp+`=$2`, identity, now.AddDate(0, 0, -10))
		require.NoError(t, err)
	}
	repo := &upstreamCenterRepository{db: db}
	first, err := repo.CleanupStorage(ctx, 30, 7, now)
	require.NoError(t, err)
	require.Equal(t, int64(5000), first.BalanceDeleted)
	require.Equal(t, int64(5000), first.BillingDeleted)
	require.True(t, first.HasMore)
	second, err := repo.CleanupStorage(ctx, 30, 7, now)
	require.NoError(t, err)
	require.Equal(t, int64(102), second.BalanceDeleted)
	require.Equal(t, int64(102), second.BillingDeleted)
	third, err := repo.CleanupStorage(ctx, 30, 7, now)
	require.NoError(t, err)
	require.Zero(t, third.BalanceDeleted)
	require.Zero(t, third.BillingDeleted)
	for _, table := range []string{"upstream_balance_snapshots", "upstream_billing_snapshots"} {
		var n int
		require.NoError(t, db.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+table).Scan(&n))
		require.Equal(t, 3, n)
		require.NoError(t, db.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+table+` WHERE identity_hash='old-key'`).Scan(&n))
		require.Zero(t, n)
		require.NoError(t, db.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+table+` WHERE identity_hash=$1`, identity).Scan(&n))
		require.Equal(t, 2, n)
	}
	_, err = db.ExecContext(ctx, `UPDATE upstream_targets SET api_key_encrypted='replacement-cipher' WHERE id=1`)
	require.NoError(t, err)
	replaced, err := repo.CleanupStorage(ctx, 30, 7, now)
	require.NoError(t, err)
	require.Equal(t, int64(2), replaced.BalanceDeleted)
	require.Equal(t, int64(2), replaced.BillingDeleted)
	for _, table := range []string{"upstream_balance_snapshots", "upstream_billing_snapshots"} {
		var n int
		require.NoError(t, db.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+table+` WHERE identity_hash=$1`, identity).Scan(&n))
		require.Zero(t, n, "a changed target without new observations cannot retain the old key as a fallback")
	}
}

func TestUpstreamStorageCleanupAdvisoryLockAndInvalidRetention(t *testing.T) {
	db, ctx, otherDB := upstreamStorageTestDB(t)
	repo := &upstreamCenterRepository{db: db}
	for _, days := range []int{0, 29, 366} {
		_, err := repo.CleanupStorage(ctx, days, 7, time.Now())
		require.ErrorIs(t, err, service.ErrUpstreamInvalid)
	}
	for _, days := range []int{0, 91} {
		_, err := repo.CleanupStorage(ctx, 30, days, time.Now())
		require.ErrorIs(t, err, service.ErrUpstreamInvalid)
	}
	other := otherDB()
	tx, err := other.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback() }()
	_, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(254,0)`)
	require.NoError(t, err)
	result, err := repo.CleanupStorage(ctx, 30, 7, time.Now())
	require.ErrorIs(t, err, service.ErrUpstreamStorageCleanupBusy)
	require.Nil(t, result)
}
