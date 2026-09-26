package repository

import (
	"context"
	"database/sql"
	"errors"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
)

const upstreamStorageArchivesSQL = `
SELECT 'supplier' AS kind,s.id,s.name,s.deleted_at,'' AS source_type,'' AS supplier_name
FROM upstream_suppliers s WHERE s.deleted_at IS NOT NULL
UNION ALL
SELECT 'target',t.id,t.name,COALESCE(t.deleted_at,s.deleted_at),CASE WHEN t.supplier_id IS NULL THEN 'monitor' ELSE 'upstream' END,COALESCE(s.name,'')
FROM upstream_targets t LEFT JOIN upstream_suppliers s ON s.id=t.supplier_id
WHERE t.deleted_at IS NOT NULL OR s.deleted_at IS NOT NULL
UNION ALL
SELECT 'intelligence',p.id,COALESCE(a.name,p.name),p.deleted_at,p.source_type,COALESCE(s.name,'')
FROM intelligence_monitor_plans p
LEFT JOIN upstream_targets t ON t.id=p.upstream_target_id
LEFT JOIN upstream_suppliers s ON s.id=t.supplier_id
LEFT JOIN accounts a ON p.source_type='openai_oauth' AND a.id=p.account_id AND a.deleted_at IS NULL
WHERE p.deleted_at IS NOT NULL`

func (r *upstreamCenterRepository) ListStorageArchives(ctx context.Context) (*service.UpstreamStorageArchivePage, error) {
	out := &service.UpstreamStorageArchivePage{Items: []*service.UpstreamStorageArchiveItem{}}
	rows, err := r.db.QueryContext(ctx, `SELECT kind,id,name,deleted_at,source_type,supplier_name,COUNT(*) OVER() FROM (`+upstreamStorageArchivesSQL+`) archived ORDER BY deleted_at DESC,kind,id DESC LIMIT 200`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		item := new(service.UpstreamStorageArchiveItem)
		if err = rows.Scan(&item.Kind, &item.ID, &item.Name, &item.DeletedAt, &item.SourceType, &item.SupplierName, &out.Total); err != nil {
			return nil, err
		}
		out.Items = append(out.Items, item)
	}
	return out, rows.Err()
}

// Purge holds the same membership lock as inventory changes, then locks targets
// and plans before checking workers. A busy operation is rejected, never killed.
func (r *upstreamCenterRepository) PurgeStorage(ctx context.Context, in service.UpstreamStoragePurgeInput) ([]string, error) {
	if in.ID <= 0 || (in.Kind != "supplier" && in.Kind != "target" && in.Kind != "intelligence") {
		return nil, service.ErrUpstreamStorageInvalid
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	if err = lockManualOrderMembership(ctx, tx); err != nil {
		return nil, err
	}
	// Retention takes this lock before target/plan rows too. Serializing the
	// cleanup transactions prevents raw-history/rollup races and lock inversions.
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(254,0)`); err != nil {
		return nil, err
	}
	if in.Kind == "intelligence" {
		keys, err := purgeIntelligenceStorage(ctx, tx, in)
		if err != nil {
			return nil, err
		}
		if err = tx.Commit(); err != nil {
			return nil, err
		}
		return keys, nil
	}
	targetIDs, err := lockUpstreamPurgeTargets(ctx, tx, in)
	if err != nil {
		return nil, err
	}
	if err = detachUpstreamIntelligencePlans(ctx, tx, targetIDs); err != nil {
		return nil, err
	}
	where, args := "target_id=ANY($1)", []any{pq.Array(targetIDs)}
	if in.Kind == "supplier" {
		where += " OR supplier_id=$2"
		args = append(args, in.ID)
	}
	// Financial aggregates survive, with the supplier identity at check time.
	// The helper atomically deletes raw records while accumulating their costs.
	if _, err = rollupUpstreamMonitorHistory(ctx, tx, where, args...); err != nil {
		return nil, err
	}
	for _, table := range []string{"upstream_account_bindings", "upstream_balance_snapshots"} {
		if _, err = tx.ExecContext(ctx, `DELETE FROM `+table+` WHERE `+where, args...); err != nil {
			return nil, err
		}
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM upstream_billing_snapshots WHERE target_id=ANY($1)`, pq.Array(targetIDs)); err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM upstream_targets WHERE id=ANY($1)`, pq.Array(targetIDs)); err != nil {
		return nil, err
	}
	if in.Kind == "supplier" {
		if _, err = tx.ExecContext(ctx, `DELETE FROM upstream_suppliers WHERE id=$1`, in.ID); err != nil {
			return nil, err
		}
	}
	return nil, tx.Commit()
}

func upstreamStorageQueryError(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return service.ErrUpstreamStorageNotFound
	}
	return err
}

func lockUpstreamPurgeTargets(ctx context.Context, tx *sql.Tx, in service.UpstreamStoragePurgeInput) ([]int64, error) {
	name := ""
	if in.Kind == "supplier" {
		if err := tx.QueryRowContext(ctx, `SELECT name FROM upstream_suppliers WHERE id=$1 FOR UPDATE`, in.ID).Scan(&name); err != nil {
			return nil, upstreamStorageQueryError(err)
		}
		if name != in.ConfirmName {
			return nil, service.ErrUpstreamStorageConfirm
		}
	}
	where := "id=$1"
	if in.Kind == "supplier" {
		where = "supplier_id=$1"
	}
	rows, err := tx.QueryContext(ctx, `SELECT id,name,COALESCE(lease_until>NOW(),FALSE) OR COALESCE(balance_lease_until>NOW(),FALSE) FROM upstream_targets WHERE `+where+` ORDER BY id FOR UPDATE`, in.ID)
	if err != nil {
		return nil, err
	}
	ids := []int64{}
	for rows.Next() {
		var id int64
		var busy bool
		if err = rows.Scan(&id, &name, &busy); err != nil {
			_ = rows.Close()
			return nil, err
		}
		if in.Kind == "target" && name != in.ConfirmName {
			_ = rows.Close()
			return nil, service.ErrUpstreamStorageConfirm
		}
		if busy {
			_ = rows.Close()
			return nil, service.ErrUpstreamStorageBusy
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	_ = rows.Close()
	if err != nil {
		return nil, err
	}
	if in.Kind == "target" && len(ids) == 0 {
		return nil, service.ErrUpstreamStorageNotFound
	}
	return ids, nil
}

func detachUpstreamIntelligencePlans(ctx context.Context, tx *sql.Tx, targetIDs []int64) error {
	if len(targetIDs) == 0 {
		return nil
	}
	rows, err := tx.QueryContext(ctx, `SELECT id FROM intelligence_monitor_plans WHERE upstream_target_id=ANY($1) ORDER BY id FOR UPDATE`, pq.Array(targetIDs))
	if err != nil {
		return err
	}
	planIDs := []int64{}
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			_ = rows.Close()
			return err
		}
		planIDs = append(planIDs, id)
	}
	err = rows.Err()
	_ = rows.Close()
	if err != nil {
		return err
	}
	if len(planIDs) == 0 {
		return nil
	}
	if err = rejectActiveIntelligenceRuns(ctx, tx, planIDs); err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE intelligence_monitor_plans SET enabled=FALSE,next_run_at=NULL,upstream_target_id=NULL,api_key_encrypted='',updated_at=clock_timestamp() WHERE id=ANY($1)`, pq.Array(planIDs))
	return err
}

func rejectActiveIntelligenceRuns(ctx context.Context, tx *sql.Tx, planIDs []int64) error {
	var busy bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM intelligence_monitor_runs WHERE plan_id=ANY($1) AND status IN ('pending','running'))`, pq.Array(planIDs)).Scan(&busy); err != nil {
		return err
	}
	if busy {
		return service.ErrUpstreamStorageBusy
	}
	return nil
}

func purgeIntelligenceStorage(ctx context.Context, tx *sql.Tx, in service.UpstreamStoragePurgeInput) ([]string, error) {
	var name, sourceType string
	var keyID, ownerID, accountID *int64
	if err := tx.QueryRowContext(ctx, `SELECT name,local_api_key_id,local_key_owner_id,source_type,account_id FROM intelligence_monitor_plans WHERE id=$1 FOR UPDATE`, in.ID).Scan(&name, &keyID, &ownerID, &sourceType, &accountID); err != nil {
		return nil, upstreamStorageQueryError(err)
	}
	if sourceType == "openai_oauth" && accountID != nil {
		// OAuth cards use the current account name. Lock it through confirmation
		// and deletion so a concurrent rename cannot change what is confirmed.
		// NOWAIT also avoids reversing an account deletion's FK lock order.
		var currentName string
		err := tx.QueryRowContext(ctx, `SELECT name FROM accounts WHERE id=$1 AND deleted_at IS NULL FOR SHARE NOWAIT`, *accountID).Scan(&currentName)
		if err == nil {
			name = currentName
		} else if !errors.Is(err, sql.ErrNoRows) {
			var pgErr *pq.Error
			if errors.As(err, &pgErr) && pgErr.Code == "55P03" {
				return nil, service.ErrUpstreamStorageBusy
			}
			return nil, err
		}
	}
	if name != in.ConfirmName {
		return nil, service.ErrUpstreamStorageConfirm
	}
	if err := rejectActiveIntelligenceRuns(ctx, tx, []int64{in.ID}); err != nil {
		return nil, err
	}
	keys := []string{}
	if keyID != nil && ownerID != nil {
		var key string
		err := tx.QueryRowContext(ctx, `UPDATE api_keys SET status='disabled',updated_at=NOW() WHERE id=$1 AND user_id=$2 AND deleted_at IS NULL RETURNING key`, *keyID, *ownerID).Scan(&key)
		if err != nil && !errors.Is(err, sql.ErrNoRows) {
			return nil, err
		}
		if err == nil && key != "" {
			keys = append(keys, key)
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM intelligence_monitor_runs WHERE plan_id=$1`, in.ID); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM intelligence_monitor_plans WHERE id=$1`, in.ID); err != nil {
		return nil, err
	}
	return keys, nil
}
