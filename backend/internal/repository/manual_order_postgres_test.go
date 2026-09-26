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

// Every connection is confined to a random private schema; no public rows or
// live plans are modified and none of these tests initiates a model request.
func manualOrderTestDB(t *testing.T) (*sql.DB, context.Context, func() *sql.DB) {
	t.Helper()
	dsn := os.Getenv("UPSTREAM_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("UPSTREAM_TEST_DATABASE_URL is not configured")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	t.Cleanup(cancel)
	schema := "manual_order_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	db, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	_, err = db.ExecContext(ctx, `CREATE SCHEMA `+pqQuoteIdentifier(schema))
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), `DROP SCHEMA `+pqQuoteIdentifier(schema)+` CASCADE`)
	})
	_, err = db.ExecContext(ctx, `SET search_path TO `+pqQuoteIdentifier(schema))
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `CREATE TABLE accounts(id BIGINT PRIMARY KEY,type TEXT NOT NULL DEFAULT 'apikey',credentials JSONB NOT NULL DEFAULT '{}',deleted_at TIMESTAMPTZ); CREATE TABLE api_keys(id BIGINT PRIMARY KEY,status TEXT NOT NULL,updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),deleted_at TIMESTAMPTZ)`)
	require.NoError(t, err)
	for _, name := range []string{"242_upstream_center.sql", "245_intelligence_monitor.sql", "246_intelligence_monitor_oauth.sql", "249_intelligence_monitor_interval_seconds.sql", "250_intelligence_monitor_generation_timeout.sql", "253_upstream_newapi_credentials.sql"} {
		migration, e := migrations.FS.ReadFile(name)
		require.NoError(t, e)
		_, e = db.ExecContext(ctx, string(migration))
		require.NoError(t, e)
	}
	_, err = db.ExecContext(ctx, `ALTER TABLE upstream_targets ADD COLUMN balance_next_sync_at TIMESTAMPTZ;
INSERT INTO upstream_suppliers(id,name) VALUES(1,'Supplier one'),(2,'Supplier two');
INSERT INTO upstream_targets(id,supplier_id,name,provider,endpoint,api_key_encrypted,api_key_fingerprint,enabled,next_check_at) VALUES
(10,1,'Group ten','openai','https://example.com','cipher-ten','ten',FALSE,NULL),
(11,1,'Group eleven','openai','https://example.com','cipher-eleven','eleven',TRUE,'2026-09-25T00:00:00Z'),
(20,2,'Group twenty','openai','https://example.com','cipher-twenty','twenty',FALSE,NULL),
(21,2,'Group twenty-one','openai','https://example.com','cipher-twenty-one','twenty-one',FALSE,NULL),
(30,NULL,'Monitor thirty','openai','https://example.com','cipher-thirty','thirty',FALSE,NULL),
(31,NULL,'Monitor thirty-one','openai','https://example.com','cipher-thirty-one','thirty-one',FALSE,NULL);
INSERT INTO intelligence_monitor_plans(id,name,source_type,created_by,created_at,enabled,next_run_at) VALUES
(100,'IQ hundred','external',1,'2026-09-24T00:00:00Z',FALSE,NULL),
(101,'IQ hundred-one','local_group',1,'2026-09-24T00:00:00Z',TRUE,'2026-09-25T00:00:00Z'),
(200,'OAuth two-hundred','openai_oauth',1,'2026-09-24T00:00:00Z',FALSE,NULL),
(201,'OAuth two-hundred-one','openai_oauth',1,'2026-09-24T00:00:00Z',FALSE,NULL);
INSERT INTO intelligence_monitor_runs(plan_id,plan_name,status,trigger,model,reasoning_effort,prompt,source_type,source_name,source_endpoint,api_mode,timeout_seconds) VALUES(101,'Preserved run','pending','manual','gpt-6-astra','high','preserve','local_group','source','','responses',900);`)
	require.NoError(t, err)
	newDB := func() *sql.DB {
		other, e := sql.Open("postgres", dsn)
		require.NoError(t, e)
		other.SetMaxOpenConns(1)
		t.Cleanup(func() { _ = other.Close() })
		_, e = other.ExecContext(ctx, `SET search_path TO `+pqQuoteIdentifier(schema))
		require.NoError(t, e)
		return other
	}
	return db, ctx, newDB
}

func applyManualOrderMigration(t *testing.T, db *sql.DB, ctx context.Context) {
	t.Helper()
	migration, err := migrations.FS.ReadFile("251_upstream_manual_order.sql")
	require.NoError(t, err)
	for range 2 {
		_, err = db.ExecContext(ctx, string(migration))
		require.NoError(t, err, "migration is idempotent")
	}
}

func manualOrderSnapshot(t *testing.T, db *sql.DB, ctx context.Context) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, table := range []string{"upstream_suppliers", "upstream_targets", "upstream_account_bindings", "intelligence_monitor_plans", "intelligence_monitor_runs"} {
		var value string
		require.NoError(t, db.QueryRowContext(ctx, `SELECT COALESCE(jsonb_agg(to_jsonb(item)-'sort_order' ORDER BY id),'[]')::text FROM `+table+` item`).Scan(&value))
		out[table] = value
	}
	return out
}

func upstreamOrderIDs(t *testing.T, db *sql.DB, ctx context.Context, scope string, supplier int64) []int64 {
	t.Helper()
	r := &upstreamCenterRepository{db: db}
	ids := []int64{}
	if scope == "suppliers" {
		rows, err := r.ListSuppliers(ctx)
		require.NoError(t, err)
		for _, row := range rows {
			ids = append(ids, row.ID)
		}
		return ids
	}
	rows, err := r.ListTargets(ctx)
	require.NoError(t, err)
	for _, row := range rows {
		if (scope == "monitors" && row.SupplierID == nil) || (scope == "groups" && row.SupplierID != nil && *row.SupplierID == supplier) {
			ids = append(ids, row.ID)
		}
	}
	return ids
}

func intelligenceOrderIDs(t *testing.T, db *sql.DB, ctx context.Context, oauth bool) []int64 {
	t.Helper()
	rows, err := (&intelligenceMonitorRepository{db: db}).ListPlans(ctx)
	require.NoError(t, err)
	ids := []int64{}
	for _, row := range rows {
		if (row.SourceType == "openai_oauth") == oauth {
			ids = append(ids, row.ID)
		}
	}
	return ids
}

func TestManualOrderPostgresPersistenceIsolationAndMigration(t *testing.T) {
	db, ctx, newDB := manualOrderTestDB(t)
	before := manualOrderSnapshot(t, db, ctx)
	applyManualOrderMigration(t, db, ctx)
	require.Equal(t, before, manualOrderSnapshot(t, db, ctx), "migration preserves metadata, scheduling, credentials and run snapshots")
	require.Equal(t, []int64{1, 2}, upstreamOrderIDs(t, db, ctx, "suppliers", 0))
	require.Equal(t, []int64{10, 11}, upstreamOrderIDs(t, db, ctx, "groups", 1))
	require.Equal(t, []int64{30, 31}, upstreamOrderIDs(t, db, ctx, "monitors", 0))
	require.Equal(t, []int64{101, 100}, intelligenceOrderIDs(t, db, ctx, false))
	require.Equal(t, []int64{201, 200}, intelligenceOrderIDs(t, db, ctx, true))
	u, i := &upstreamCenterRepository{db: db}, &intelligenceMonitorRepository{db: db}
	one := int64(1)
	require.NoError(t, u.SaveOrder(ctx, service.UpstreamOrderInput{Scope: "suppliers", IDs: []int64{2, 1}}))
	require.NoError(t, u.SaveOrder(ctx, service.UpstreamOrderInput{Scope: "groups", SupplierID: &one, IDs: []int64{11, 10}}))
	require.NoError(t, u.SaveOrder(ctx, service.UpstreamOrderInput{Scope: "monitors", IDs: []int64{31, 30}}))
	require.NoError(t, i.SaveOrder(ctx, service.IntelligenceOrderInput{Scope: "intelligence", IDs: []int64{100, 101}}))
	require.NoError(t, i.SaveOrder(ctx, service.IntelligenceOrderInput{Scope: "oauth", IDs: []int64{200, 201}}))
	require.Equal(t, before, manualOrderSnapshot(t, db, ctx), "only sort_order may change, including for an actively queued plan")
	// A new connection/repository sees persistent order. Applying the migration
	// again does not discard an existing administrator preference.
	applyManualOrderMigration(t, db, ctx)
	other := newDB()
	require.Equal(t, []int64{2, 1}, upstreamOrderIDs(t, other, ctx, "suppliers", 0))
	require.Equal(t, []int64{11, 10}, upstreamOrderIDs(t, other, ctx, "groups", 1))
	require.Equal(t, []int64{20, 21}, upstreamOrderIDs(t, other, ctx, "groups", 2))
	require.Equal(t, []int64{31, 30}, upstreamOrderIDs(t, other, ctx, "monitors", 0))
	require.Equal(t, []int64{100, 101}, intelligenceOrderIDs(t, other, ctx, false))
	require.Equal(t, []int64{200, 201}, intelligenceOrderIDs(t, other, ctx, true))
	_, err := db.ExecContext(ctx, `SELECT setval('upstream_suppliers_id_seq',2);
INSERT INTO upstream_targets(id,supplier_id,name,provider,endpoint,api_key_encrypted,api_key_fingerprint) VALUES(12,1,'New group','openai','https://example.com','cipher','new-group'),(32,NULL,'New monitor','openai','https://example.com','cipher','new-monitor');
INSERT INTO intelligence_monitor_plans(id,name,source_type,created_by) VALUES(102,'New IQ','external',1),(202,'New OAuth','openai_oauth',1);`)
	require.NoError(t, err)
	newSupplier := &service.UpstreamSupplier{Name: "New supplier"}
	require.NoError(t, u.SaveSupplier(ctx, newSupplier))
	require.Equal(t, []int64{newSupplier.ID, 2, 1}, upstreamOrderIDs(t, other, ctx, "suppliers", 0), "new suppliers precede the saved manual order")
	nextSupplier := &service.UpstreamSupplier{Name: "Newest supplier"}
	require.NoError(t, u.SaveSupplier(ctx, nextSupplier))
	newSupplier.Name = "Renamed supplier"
	require.NoError(t, u.SaveSupplier(ctx, newSupplier))
	require.Equal(t, []int64{nextSupplier.ID, newSupplier.ID, 2, 1}, upstreamOrderIDs(t, other, ctx, "suppliers", 0), "repeated additions prepend; editing does not reorder")
	require.Equal(t, []int64{11, 10, 12}, upstreamOrderIDs(t, db, ctx, "groups", 1))
	require.Equal(t, []int64{31, 30, 32}, upstreamOrderIDs(t, db, ctx, "monitors", 0))
	require.Equal(t, []int64{100, 101, 102}, intelligenceOrderIDs(t, db, ctx, false))
	require.Equal(t, []int64{200, 201, 202}, intelligenceOrderIDs(t, db, ctx, true))
}

func TestManualOrderPostgresRejectsStaleOrCrossScopeAndAllowsEmpty(t *testing.T) {
	db, ctx, _ := manualOrderTestDB(t)
	applyManualOrderMigration(t, db, ctx)
	u, i := &upstreamCenterRepository{db: db}, &intelligenceMonitorRepository{db: db}
	one, missing := int64(1), int64(999)
	for _, input := range []service.UpstreamOrderInput{
		{Scope: "suppliers", IDs: []int64{1}}, {Scope: "suppliers", IDs: []int64{1, 999}},
		{Scope: "suppliers", IDs: []int64{}}, {Scope: "monitors", IDs: []int64{10, 11}},
		{Scope: "groups", SupplierID: &one, IDs: []int64{20, 21}},
		{Scope: "groups", SupplierID: &missing, IDs: []int64{}},
	} {
		require.ErrorIs(t, u.SaveOrder(ctx, input), service.ErrManualOrderConflict)
	}
	for _, input := range []service.IntelligenceOrderInput{
		{Scope: "oauth", IDs: []int64{100, 101}}, {Scope: "intelligence", IDs: []int64{200, 201}},
		{Scope: "intelligence", IDs: []int64{100}}, {Scope: "oauth", IDs: []int64{}},
	} {
		require.ErrorIs(t, i.SaveOrder(ctx, input), service.ErrManualOrderConflict)
	}
	require.Equal(t, []int64{1, 2}, upstreamOrderIDs(t, db, ctx, "suppliers", 0), "conflicts leave all sort fields untouched")
	require.NoError(t, u.ArchiveTarget(ctx, 31))
	require.ErrorIs(t, u.SaveOrder(ctx, service.UpstreamOrderInput{Scope: "monitors", IDs: []int64{31, 30}}), service.ErrManualOrderConflict)
	require.NoError(t, u.ArchiveTarget(ctx, 30))
	require.NoError(t, u.SaveOrder(ctx, service.UpstreamOrderInput{Scope: "monitors", IDs: []int64{}}))
	require.NoError(t, i.ArchivePlan(ctx, 200))
	require.ErrorIs(t, i.SaveOrder(ctx, service.IntelligenceOrderInput{Scope: "oauth", IDs: []int64{201, 200}}), service.ErrManualOrderConflict)
	require.NoError(t, i.ArchivePlan(ctx, 201))
	require.NoError(t, i.SaveOrder(ctx, service.IntelligenceOrderInput{Scope: "oauth", IDs: []int64{}}))
	_, err := db.ExecContext(ctx, `INSERT INTO upstream_suppliers(id,name) VALUES(3,'Empty supplier')`)
	require.NoError(t, err)
	empty := int64(3)
	require.NoError(t, u.SaveOrder(ctx, service.UpstreamOrderInput{Scope: "groups", SupplierID: &empty, IDs: []int64{}}))
}

func TestManualOrderPostgresScopeChangesResetPosition(t *testing.T) {
	db, ctx, _ := manualOrderTestDB(t)
	applyManualOrderMigration(t, db, ctx)
	u, i := &upstreamCenterRepository{db: db}, &intelligenceMonitorRepository{db: db}
	one, two := int64(1), int64(2)
	require.NoError(t, u.SaveOrder(ctx, service.UpstreamOrderInput{Scope: "groups", SupplierID: &one, IDs: []int64{10, 11}}))
	require.NoError(t, u.SaveOrder(ctx, service.UpstreamOrderInput{Scope: "groups", SupplierID: &two, IDs: []int64{21, 20}}))
	target, err := u.GetTarget(ctx, 10)
	require.NoError(t, err)
	target.SupplierID = &two
	require.NoError(t, u.SaveTarget(ctx, target))
	require.Equal(t, []int64{21, 20, 10}, upstreamOrderIDs(t, db, ctx, "groups", 2))
	require.ErrorIs(t, u.SaveOrder(ctx, service.UpstreamOrderInput{Scope: "groups", SupplierID: &one, IDs: []int64{10, 11}}), service.ErrManualOrderConflict)
	require.NoError(t, i.SaveOrder(ctx, service.IntelligenceOrderInput{Scope: "intelligence", IDs: []int64{100, 101}}))
	require.NoError(t, i.SaveOrder(ctx, service.IntelligenceOrderInput{Scope: "oauth", IDs: []int64{201, 200}}))
	plan, err := i.GetPlan(ctx, 100)
	require.NoError(t, err)
	plan.SourceType = "upstream"
	require.NoError(t, i.SavePlan(ctx, plan))
	require.Equal(t, []int64{100, 101}, intelligenceOrderIDs(t, db, ctx, false), "non-OAuth source edits keep position in the same list")
	plan.SourceType = "openai_oauth"
	require.NoError(t, i.SavePlan(ctx, plan))
	require.Equal(t, []int64{201, 200, 100}, intelligenceOrderIDs(t, db, ctx, true))
}

func TestManualOrderPostgresSerializesMembershipAndSameScope(t *testing.T) {
	db, ctx, newDB := manualOrderTestDB(t)
	applyManualOrderMigration(t, db, ctx)
	for _, lock := range []string{`SELECT pg_advisory_xact_lock(251,0)`, `SELECT pg_advisory_xact_lock(2511,hashtext('suppliers'))`} {
		other := newDB()
		tx, err := db.BeginTx(ctx, nil)
		require.NoError(t, err)
		_, err = tx.ExecContext(ctx, lock)
		require.NoError(t, err)
		short, cancel := context.WithTimeout(ctx, 150*time.Millisecond)
		err = (&upstreamCenterRepository{db: other}).SaveOrder(short, service.UpstreamOrderInput{Scope: "suppliers", IDs: []int64{2, 1}})
		cancel()
		require.Error(t, err, "must wait for membership changes and concurrent same-scope reorder")
		require.NoError(t, tx.Rollback())
	}
	// Refresh the connection after a cancelled lib/pq request. SET search_path is
	// deliberately per-connection in this private-schema test helper.
	other := newDB()
	tx, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock_shared(251,0)`)
	require.NoError(t, err)
	short, cancel := context.WithTimeout(ctx, 150*time.Millisecond)
	err = (&upstreamCenterRepository{db: other}).ArchiveSupplier(short, 1)
	cancel()
	require.Error(t, err, "membership writes must wait until reorder membership is committed")
	require.NoError(t, tx.Rollback())
	_, err = db.ExecContext(ctx, `INSERT INTO upstream_suppliers(id,name) VALUES(3,'Concurrent insertion')`)
	require.NoError(t, err)
	require.ErrorIs(t, (&upstreamCenterRepository{db: db}).SaveOrder(ctx, service.UpstreamOrderInput{Scope: "suppliers", IDs: []int64{2, 1}}), service.ErrManualOrderConflict)
	require.Equal(t, []int64{1, 2, 3}, upstreamOrderIDs(t, db, ctx, "suppliers", 0))
}
