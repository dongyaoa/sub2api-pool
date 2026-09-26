package repository

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
)

const upstreamStorageBatchSize = 5000

// where must be a repository-owned SQL fragment, never user input. Deleting
// and aggregating the exact returned rows in one statement is replay-safe.
func rollupUpstreamMonitorHistory(ctx context.Context, tx *sql.Tx, where string, args ...any) (int64, error) {
	query := `WITH removed AS (
 DELETE FROM upstream_monitor_history WHERE ` + where + `
 RETURNING checked_at,target_id,supplier_id,cost,cost_source
), grouped AS (
 SELECT date_trunc('hour',checked_at AT TIME ZONE 'UTC') AT TIME ZONE 'UTC' AS hour_start,
 target_id,supplier_id,MIN(checked_at) AS first_sample_at,MAX(checked_at) AS last_sample_at,COALESCE(SUM(cost),0) AS cost,
 COUNT(*) FILTER(WHERE cost IS NULL OR cost_source='unknown') AS unpriced_count,
 COUNT(*) FILTER(WHERE cost IS NOT NULL AND cost_source='reported') AS reported_count,
 COUNT(*) FILTER(WHERE cost IS NOT NULL AND cost_source='estimated') AS estimated_count,
 COUNT(*) AS sample_count
 FROM removed GROUP BY 1,target_id,supplier_id
), saved AS (
 INSERT INTO upstream_monitor_cost_rollups(hour_start,target_id,supplier_id,first_sample_at,last_sample_at,cost,unpriced_count,reported_count,estimated_count,sample_count)
 SELECT hour_start,target_id,supplier_id,first_sample_at,last_sample_at,cost,unpriced_count,reported_count,estimated_count,sample_count FROM grouped
 ON CONFLICT (hour_start,target_id,(COALESCE(supplier_id,0))) DO UPDATE SET
 cost=upstream_monitor_cost_rollups.cost+EXCLUDED.cost,
 unpriced_count=upstream_monitor_cost_rollups.unpriced_count+EXCLUDED.unpriced_count,
 reported_count=upstream_monitor_cost_rollups.reported_count+EXCLUDED.reported_count,
 estimated_count=upstream_monitor_cost_rollups.estimated_count+EXCLUDED.estimated_count,
 sample_count=upstream_monitor_cost_rollups.sample_count+EXCLUDED.sample_count,
 first_sample_at=LEAST(upstream_monitor_cost_rollups.first_sample_at,EXCLUDED.first_sample_at),
 last_sample_at=GREATEST(upstream_monitor_cost_rollups.last_sample_at,EXCLUDED.last_sample_at),
 updated_at=clock_timestamp() RETURNING 1
) SELECT COUNT(*) FROM removed`
	var count int64
	if err := tx.QueryRowContext(ctx, query, args...).Scan(&count); err != nil {
		return 0, fmt.Errorf("archive upstream monitor costs: %w", err)
	}
	return count, nil
}

func (r *upstreamCenterRepository) CleanupStorage(ctx context.Context, historyDays, snapshotDays int, now time.Time) (*service.UpstreamStorageCleanupResult, error) {
	if historyDays < 30 || historyDays > 365 || snapshotDays < 1 || snapshotDays > 90 {
		return nil, service.ErrUpstreamInvalid
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err = tx.ExecContext(ctx, `SET LOCAL statement_timeout='30s'`); err != nil {
		return nil, err
	}
	var locked bool
	if err = tx.QueryRowContext(ctx, `SELECT pg_try_advisory_xact_lock(254,0)`).Scan(&locked); err != nil {
		return nil, err
	}
	result := &service.UpstreamStorageCleanupResult{}
	if !locked {
		return nil, service.ErrUpstreamStorageCleanupBusy
	}
	// Keep the entire cutoff hour raw; any partially retained hour must remain
	// exactly queryable while it still contributes to the last 30 days.
	historyCutoff := now.UTC().AddDate(0, 0, -historyDays).Truncate(time.Hour)
	result.HistoryDeleted, err = rollupUpstreamMonitorHistory(ctx, tx,
		`id IN (SELECT id FROM upstream_monitor_history WHERE checked_at < $1 ORDER BY checked_at,id LIMIT $2 FOR UPDATE)`, historyCutoff, upstreamStorageBatchSize)
	if err != nil {
		return nil, err
	}
	snapshotCutoff := now.UTC().AddDate(0, 0, -snapshotDays)
	result.BalanceDeleted, err = cleanupUpstreamSnapshots(ctx, tx, "upstream_balance_snapshots", "synced_at", snapshotCutoff)
	if err != nil {
		return nil, err
	}
	result.BillingDeleted, err = cleanupUpstreamSnapshots(ctx, tx, "upstream_billing_snapshots", "attempted_at", snapshotCutoff)
	if err != nil {
		return nil, err
	}
	// A full batch might have exhausted the table. One cheap extra worker pass
	// is preferable to another unbounded candidate scan under this transaction.
	result.HasMore = result.HistoryDeleted >= upstreamStorageBatchSize || result.BalanceDeleted >= upstreamStorageBatchSize || result.BillingDeleted >= upstreamStorageBatchSize
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return result, nil
}

// Candidates exclude the two protected rows using the SQL identity mirror so
// ancient retained snapshots cannot starve cleanup of newer expired records.
// Before deletion, Go identity is authoritative and target rows are locked.
func cleanupUpstreamSnapshots(ctx context.Context, tx *sql.Tx, table, timestamp string, cutoff time.Time) (int64, error) {
	query := `SELECT s.id,s.target_id FROM ` + table + ` s
 LEFT JOIN upstream_targets t ON t.id=s.target_id
 LEFT JOIN LATERAL (SELECT upstream_storage_identity_hash(t.provider,t.endpoint,t.api_key_encrypted,t.supplier_id,t.wallet_ref,t.newapi_user_id,t.newapi_access_token_encrypted) AS value) identity ON t.id IS NOT NULL
 WHERE s.` + timestamp + ` < $1 AND (
 t.id IS NULL OR s.identity_hash IS DISTINCT FROM identity.value OR (
 s.id IS DISTINCT FROM (SELECT p.id FROM ` + table + ` p WHERE p.target_id=s.target_id AND p.identity_hash=identity.value ORDER BY p.` + timestamp + ` DESC,p.id DESC LIMIT 1)
 AND s.id IS DISTINCT FROM (SELECT p.id FROM ` + table + ` p WHERE p.target_id=s.target_id AND p.identity_hash=identity.value AND p.status='ok' ORDER BY p.` + timestamp + ` DESC,p.id DESC LIMIT 1)))
 ORDER BY s.` + timestamp + `,s.id LIMIT $2`
	rows, err := tx.QueryContext(ctx, query, cutoff, upstreamStorageBatchSize)
	if err != nil {
		return 0, fmt.Errorf("select expired upstream snapshots: %w", err)
	}
	ids := []int64{}
	targetIDs := []int64{}
	seen := map[int64]bool{}
	for rows.Next() {
		var id, targetID int64
		if err = rows.Scan(&id, &targetID); err != nil {
			_ = rows.Close()
			return 0, err
		}
		ids = append(ids, id)
		if !seen[targetID] {
			seen[targetID] = true
			targetIDs = append(targetIDs, targetID)
		}
	}
	err = rows.Err()
	_ = rows.Close()
	if err != nil || len(ids) == 0 {
		return 0, err
	}
	rows, err = tx.QueryContext(ctx, `SELECT id,supplier_id,provider,endpoint,api_key_encrypted,wallet_ref,newapi_user_id,newapi_access_token_encrypted FROM upstream_targets WHERE id=ANY($1) ORDER BY id FOR SHARE`, pq.Array(targetIDs))
	if err != nil {
		return 0, err
	}
	currentIDs := []int64{}
	identities := []string{}
	for rows.Next() {
		t := &service.UpstreamFinanceTarget{}
		if err = rows.Scan(&t.ID, &t.SupplierID, &t.Provider, &t.Endpoint, &t.APIKeyEncrypted, &t.WalletRef, &t.NewAPIUserID, &t.NewAPIAccessTokenEncrypted); err != nil {
			_ = rows.Close()
			return 0, err
		}
		currentIDs = append(currentIDs, t.ID)
		identities = append(identities, service.UpstreamBalanceIdentity(t))
	}
	err = rows.Err()
	_ = rows.Close()
	if err != nil {
		return 0, err
	}
	query = `WITH identities AS (SELECT * FROM unnest($2::bigint[],$3::text[]) AS value(target_id,identity_hash)), protected AS (
 SELECT p.id FROM identities i CROSS JOIN LATERAL (SELECT id FROM ` + table + ` WHERE target_id=i.target_id AND identity_hash=i.identity_hash ORDER BY ` + timestamp + ` DESC,id DESC LIMIT 1) p
 UNION SELECT p.id FROM identities i CROSS JOIN LATERAL (SELECT id FROM ` + table + ` WHERE target_id=i.target_id AND identity_hash=i.identity_hash AND status='ok' ORDER BY ` + timestamp + ` DESC,id DESC LIMIT 1) p
) DELETE FROM ` + table + ` WHERE id=ANY($1) AND ` + timestamp + ` < $4 AND id NOT IN(SELECT id FROM protected)`
	deleted, err := tx.ExecContext(ctx, query, pq.Array(ids), pq.Array(currentIDs), pq.Array(identities), cutoff)
	if err != nil {
		return 0, fmt.Errorf("delete expired upstream snapshots: %w", err)
	}
	return deleted.RowsAffected()
}
