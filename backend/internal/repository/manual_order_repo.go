package repository

import (
	"context"
	"database/sql"
	"errors"
	"strconv"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
)

func (r *upstreamCenterRepository) SaveOrder(ctx context.Context, in service.UpstreamOrderInput) error {
	if err := in.Validate(); err != nil {
		return err
	}
	table, where, scope := "upstream_targets", "deleted_at IS NULL AND supplier_id IS NULL", "monitors"
	var args []any
	switch in.Scope {
	case "suppliers":
		table, where, scope = "upstream_suppliers", "deleted_at IS NULL", "suppliers"
	case "groups":
		where, scope = "deleted_at IS NULL AND supplier_id=$1", "groups:"+strconv.FormatInt(*in.SupplierID, 10)
		args = []any{*in.SupplierID}
	}
	return saveManualOrder(ctx, r.db, table, where, scope, args, in.SupplierID, in.IDs)
}

func (r *intelligenceMonitorRepository) SaveOrder(ctx context.Context, in service.IntelligenceOrderInput) error {
	if err := in.Validate(); err != nil {
		return err
	}
	where := "deleted_at IS NULL AND source_type <> 'openai_oauth'"
	if in.Scope == "oauth" {
		where = "deleted_at IS NULL AND source_type = 'openai_oauth'"
	}
	return saveManualOrder(ctx, r.db, "intelligence_monitor_plans", where, in.Scope, nil, nil, in.IDs)
}

// table and where are fixed internal constants, never request-controlled SQL.
// The shared membership lock lets different lists reorder concurrently while
// excluding create/archive/move operations that could invalidate membership.
// A per-list lock serializes reorders; row locks preserve membership through
// the atomic update without blocking normal worker scheduling globally.
func saveManualOrder(ctx context.Context, db *sql.DB, table, where, scope string, args []any, supplierID *int64, ids []int64) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock_shared(251,0)`); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(2511,hashtext($1))`, scope); err != nil {
		return err
	}
	if supplierID != nil {
		var found int64
		// Match archive's supplier -> targets lock order, and reject a supplier
		// that disappeared even when its now-empty group list was submitted.
		err = tx.QueryRowContext(ctx, `SELECT id FROM upstream_suppliers WHERE id=$1 AND deleted_at IS NULL FOR SHARE`, *supplierID).Scan(&found)
		if errors.Is(err, sql.ErrNoRows) {
			return service.ErrManualOrderConflict
		}
		if err != nil {
			return err
		}
	}
	rows, err := tx.QueryContext(ctx, `SELECT id FROM `+table+` WHERE `+where+` ORDER BY id FOR UPDATE`, args...)
	if err != nil {
		return err
	}
	current := make(map[int64]struct{}, len(ids))
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			_ = rows.Close()
			return err
		}
		current[id] = struct{}{}
	}
	err = rows.Err()
	_ = rows.Close()
	if err != nil {
		return err
	}
	if len(current) != len(ids) {
		return service.ErrManualOrderConflict
	}
	for _, id := range ids {
		if _, exists := current[id]; !exists {
			return service.ErrManualOrderConflict
		}
	}
	if len(ids) > 0 {
		// Do not touch updated_at: in-flight runs and configuration writes use it
		// as an identity/version guard, unrelated to administrator display order.
		_, err = tx.ExecContext(ctx, `UPDATE `+table+` AS item SET sort_order=ordered.position FROM unnest($1::bigint[]) WITH ORDINALITY AS ordered(id,position) WHERE item.id=ordered.id`, pq.Array(ids))
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}

// Always acquire this before supplier/target/plan row locks. Reorder transactions
// hold the shared form until commit, so their complete-set validation cannot
// miss a concurrent insertion, archival or cross-list move.
func lockManualOrderMembership(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(251,0)`)
	return err
}
