package repository

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestIntelligenceLocalPlanMetadataPostgresMasksAndKeepsZeroRate(t *testing.T) {
	db, ctx := intelligenceMonitorTestDB(t)
	_, err := db.ExecContext(ctx, `CREATE TABLE groups(id BIGINT PRIMARY KEY,name TEXT,rate_multiplier NUMERIC,status TEXT,deleted_at TIMESTAMPTZ);
ALTER TABLE api_keys ADD COLUMN user_id BIGINT,ADD COLUMN group_id BIGINT,ADD COLUMN name TEXT,ADD COLUMN key TEXT;
INSERT INTO groups VALUES(8,'Zero cost',0,'active',NULL),(9,'Other',2,'active',NULL);
INSERT INTO api_keys(id,status,user_id,group_id,name,key) VALUES(41,'active',7,8,'Personal key','sk-private-sensitive-secret');
INSERT INTO intelligence_monitor_plans(id,name,source_type,group_id,local_api_key_id,local_key_owner_id,local_api_key_borrowed,created_by)
VALUES(501,'Monitor','local_group',8,41,7,TRUE,7),(502,'Wrong binding','local_group',9,41,7,TRUE,7)`)
	require.NoError(t, err)
	repo := &intelligenceMonitorRepository{db: db}
	meta, err := repo.LoadLocalPlanMetadata(ctx, []int64{501, 502})
	require.NoError(t, err)
	require.Equal(t, "Personal key", meta[501].KeyName)
	require.Equal(t, "sk-p••••cret", meta[501].KeyMasked)
	require.Equal(t, "Zero cost", meta[501].GroupName)
	require.NotNil(t, meta[501].GroupRateMultiplier)
	require.Zero(t, *meta[501].GroupRateMultiplier)
	require.Empty(t, meta[502].KeyMasked, "changed group must not show a key as still bound")
	managed, err := repo.IsManagedIntelligenceKey(ctx, 41, 0)
	require.NoError(t, err)
	require.False(t, managed)
	_, err = db.ExecContext(ctx, `UPDATE intelligence_monitor_plans SET local_api_key_borrowed=FALSE WHERE id=501`)
	require.NoError(t, err)
	managed, err = repo.IsManagedIntelligenceKey(ctx, 41, 0)
	require.NoError(t, err)
	require.True(t, managed)
	managed, err = repo.IsManagedIntelligenceKey(ctx, 41, 501)
	require.NoError(t, err)
	require.False(t, managed)
}

func TestIntelligenceExecutionBindingPostgresUsesHistoricalPeriod(t *testing.T) {
	db, ctx := intelligenceMonitorTestDB(t)
	_, err := db.ExecContext(ctx, `ALTER TABLE upstream_targets ADD COLUMN name TEXT;
CREATE TABLE upstream_suppliers(id BIGINT PRIMARY KEY,name TEXT);
CREATE TABLE upstream_account_bindings(id BIGSERIAL PRIMARY KEY,account_id BIGINT,target_id BIGINT,target_name TEXT,supplier_id BIGINT,supplier_name TEXT,valid_from TIMESTAMPTZ,valid_until TIMESTAMPTZ);
INSERT INTO upstream_account_bindings(account_id,target_id,target_name,supplier_id,supplier_name,valid_from,valid_until) VALUES
(42,1,'Old key',10,'Old supplier','2026-09-27 09:00:00Z','2026-09-27 10:00:00Z'),
(42,2,'New key',20,'New supplier','2026-09-27 10:00:00Z',NULL)`)
	require.NoError(t, err)
	repo := &intelligenceMonitorRepository{db: db}
	at := time.Date(2026, 9, 27, 9, 59, 59, 0, time.UTC)
	old, err := repo.IntelligenceExecutionBinding(ctx, 42, at)
	require.NoError(t, err)
	require.Equal(t, int64(1), old["execution_upstream_target_id"])
	require.Equal(t, "Old key", old["execution_upstream_target_name"])
	require.Equal(t, int64(10), old["execution_supplier_id"])
	require.Equal(t, "Old supplier", old["execution_supplier_name"])
	current, err := repo.IntelligenceExecutionBinding(ctx, 42, at.Add(time.Second))
	require.NoError(t, err)
	require.Equal(t, "New supplier", current["execution_supplier_name"])
	missing, err := repo.IntelligenceExecutionBinding(ctx, 99, at)
	require.NoError(t, err)
	require.Equal(t, map[string]any{"execution_binding_status": "unbound"}, missing)
	_, err = db.ExecContext(ctx, `INSERT INTO upstream_account_bindings(account_id,target_id,target_name,supplier_name,valid_from) VALUES(42,3,'ambiguous','','2026-09-27 09:30:00Z')`)
	require.NoError(t, err)
	ambiguous, err := repo.IntelligenceExecutionBinding(ctx, 42, at)
	require.NoError(t, err)
	require.Equal(t, map[string]any{"execution_binding_status": "ambiguous"}, ambiguous)
}

func TestIntelligenceExecutionBindingPostgresRefreshesNamesWithoutChangingHistoricalIdentity(t *testing.T) {
	db, ctx := intelligenceMonitorTestDB(t)
	_, err := db.ExecContext(ctx, `ALTER TABLE upstream_targets ADD COLUMN name TEXT,ADD COLUMN supplier_id BIGINT,ADD COLUMN deleted_at TIMESTAMPTZ;
CREATE TABLE upstream_suppliers(id BIGINT PRIMARY KEY,name TEXT,deleted_at TIMESTAMPTZ);
CREATE TABLE upstream_account_bindings(id BIGSERIAL PRIMARY KEY,account_id BIGINT,target_id BIGINT,target_name TEXT,supplier_id BIGINT,supplier_name TEXT,valid_from TIMESTAMPTZ,valid_until TIMESTAMPTZ);
INSERT INTO upstream_suppliers(id,name) VALUES(10,'Original supplier'),(20,'Current supplier');
INSERT INTO upstream_targets(id,name,supplier_id) VALUES(1,'Original key',10),(2,'Current key',20);
INSERT INTO upstream_account_bindings(account_id,target_id,target_name,supplier_id,supplier_name,valid_from,valid_until) VALUES
(42,1,'Original key',10,'Original supplier','2026-09-27 09:00:00Z','2026-09-27 10:00:00Z'),
(42,2,'Current key',20,'Current supplier','2026-09-27 10:00:00Z',NULL);
UPDATE upstream_targets SET name='Renamed old key',supplier_id=20,deleted_at=NOW() WHERE id=1;
UPDATE upstream_suppliers SET name='Renamed old supplier',deleted_at=NOW() WHERE id=10`)
	require.NoError(t, err)
	repo := &intelligenceMonitorRepository{db: db}
	at := time.Date(2026, 9, 27, 9, 59, 59, 0, time.UTC)
	old, err := repo.IntelligenceExecutionBinding(ctx, 42, at)
	require.NoError(t, err)
	require.Equal(t, map[string]any{
		"execution_binding_status":       "matched",
		"execution_upstream_target_id":   int64(1),
		"execution_upstream_target_name": "Renamed old key",
		"execution_supplier_id":          int64(10),
		"execution_supplier_name":        "Renamed old supplier",
	}, old, "resolve names through the historical IDs, including archived identities")
	current, err := repo.IntelligenceExecutionBinding(ctx, 42, at.Add(time.Second))
	require.NoError(t, err)
	require.Equal(t, int64(2), current["execution_upstream_target_id"])
	require.Equal(t, "Current key", current["execution_upstream_target_name"])
	require.Equal(t, int64(20), current["execution_supplier_id"])
	require.Equal(t, "Current supplier", current["execution_supplier_name"])
	// Removing the old display records must not resolve through the account's
	// new binding or the old target's newly assigned supplier.
	_, err = db.ExecContext(ctx, `DELETE FROM upstream_targets WHERE id=1; DELETE FROM upstream_suppliers WHERE id=10`)
	require.NoError(t, err)
	fallback, err := repo.IntelligenceExecutionBinding(ctx, 42, at)
	require.NoError(t, err)
	require.Equal(t, int64(1), fallback["execution_upstream_target_id"])
	require.Equal(t, "Original key", fallback["execution_upstream_target_name"])
	require.Equal(t, int64(10), fallback["execution_supplier_id"])
	require.Equal(t, "Original supplier", fallback["execution_supplier_name"])
}
