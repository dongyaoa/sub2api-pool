package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

var _ service.UpstreamAccountMonitorRepository = (*upstreamCenterRepository)(nil)

type upstreamAccountMonitorQuerier interface {
	QueryContext(context.Context, string, ...any) (*sql.Rows, error)
}

// A binding must still match all credential dimensions. Fallback resolution
// prefers supplier inventory, then independent monitors, without moving either.
func findAccountMonitorID(ctx context.Context, q upstreamAccountMonitorQuerier, identity service.UpstreamAccountMonitorIdentity) (int64, error) {
	rows, err := q.QueryContext(ctx, `SELECT t.id,
 CASE WHEN EXISTS(SELECT 1 FROM upstream_account_bindings b WHERE b.target_id=t.id AND b.account_id=$1 AND b.valid_until IS NULL) THEN 0
 WHEN t.supplier_id IS NOT NULL THEN 1 ELSE 2 END AS priority
 FROM upstream_targets t
 WHERE t.deleted_at IS NULL AND t.provider=$2 AND t.endpoint=$3 AND t.api_key_fingerprint=$4
 AND (t.supplier_id IS NULL OR EXISTS(SELECT 1 FROM upstream_suppliers s WHERE s.id=t.supplier_id AND s.deleted_at IS NULL))
 ORDER BY priority,t.id LIMIT 2`, identity.AccountID, identity.Provider, identity.Endpoint, identity.Fingerprint)
	if err != nil {
		return 0, err
	}
	defer func() { _ = rows.Close() }()
	var id int64
	var priority int
	for rows.Next() {
		var candidateID int64
		var candidatePriority int
		if err := rows.Scan(&candidateID, &candidatePriority); err != nil {
			return 0, err
		}
		if id == 0 {
			id, priority = candidateID, candidatePriority
		} else if candidatePriority == priority {
			return 0, service.ErrUpstreamAccountMonitorAmbiguous
		}
	}
	return id, rows.Err()
}

func (r *upstreamCenterRepository) FindAccountMonitor(ctx context.Context, identity service.UpstreamAccountMonitorIdentity) (*service.UpstreamTarget, error) {
	id, err := findAccountMonitorID(ctx, r.db, identity)
	if err != nil || id == 0 {
		return nil, err
	}
	return r.GetTarget(ctx, id)
}

func (r *upstreamCenterRepository) EnsureAccountMonitor(ctx context.Context, identity service.UpstreamAccountMonitorIdentity, candidate *service.UpstreamTarget) (*service.UpstreamTarget, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	// Shared with inventory writes/deletes and ordering. The account row is then
	// held against edits/deletion so retries and concurrent instances converge.
	if err = lockManualOrderMembership(ctx, tx); err != nil {
		return nil, err
	}
	var accountID int64
	err = tx.QueryRowContext(ctx, `SELECT id FROM accounts WHERE id=$1 AND deleted_at IS NULL AND type='apikey'
 AND platform=$2 AND COALESCE(credentials->>'api_key','')=$3 AND COALESCE(credentials->>'base_url','')=$4
 AND parent_account_id IS NULL AND COALESCE(extra->'synthetic_ui_test','false'::jsonb) != 'true'::jsonb FOR SHARE`,
		identity.AccountID, identity.Provider, identity.Credentials.APIKey, identity.Credentials.BaseURL).Scan(&accountID)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrUpstreamBindingConflict
	}
	if err != nil {
		return nil, err
	}
	id, err := findAccountMonitorID(ctx, tx, identity)
	if err != nil {
		return nil, err
	}
	if id == 0 {
		// A matched target may have been deleted between the read and write. Do
		// not synthesize unvalidated credentials when the caller supplied none.
		if candidate == nil {
			return nil, service.ErrUpstreamBindingConflict
		}
		var supplierID int64
		err = tx.QueryRowContext(ctx, `INSERT INTO upstream_suppliers(name,website,sort_order)
 SELECT $1,$2,COALESCE(MIN(sort_order),0)-1 FROM upstream_suppliers WHERE deleted_at IS NULL RETURNING id`, candidate.Name, candidate.Endpoint).Scan(&supplierID)
		if err != nil {
			return nil, err
		}
		models, err := json.Marshal(candidate.Models)
		if err != nil {
			return nil, err
		}
		err = tx.QueryRowContext(ctx, `INSERT INTO upstream_targets(supplier_id,name,provider,api_mode,endpoint,api_key_encrypted,api_key_fingerprint,
 models,enabled,interval_seconds,timeout_seconds,degraded_threshold_ms,wallet_ref,next_check_at)
 VALUES($1,$2,$3,$4,$5,$6,$7,$8::jsonb,FALSE,$9,$10,$11,$12,NULL) RETURNING id`, supplierID, candidate.Name, candidate.Provider,
			candidate.APIMode, candidate.Endpoint, candidate.APIKeyEncrypted, candidate.APIKeyFingerprint, string(models), candidate.IntervalSeconds,
			candidate.TimeoutSeconds, candidate.DegradedThresholdMs, candidate.WalletRef).Scan(&id)
		if err != nil {
			return nil, upstreamPersistenceError(err)
		}
	}
	// Invalidated historical bindings remain for billing attribution. Independent
	// monitors retain their intentionally separate identity and schedules.
	_, err = tx.ExecContext(ctx, `UPDATE upstream_account_bindings SET valid_until=GREATEST(clock_timestamp(),valid_from)
 WHERE account_id=$1 AND valid_until IS NULL AND target_id<>$2`, identity.AccountID, id)
	if err != nil {
		return nil, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO upstream_account_bindings(target_id,account_id,supplier_id,target_name,supplier_name)
 SELECT t.id,$2,t.supplier_id,t.name,s.name FROM upstream_targets t JOIN upstream_suppliers s ON s.id=t.supplier_id
 WHERE t.id=$1 AND NOT EXISTS(SELECT 1 FROM upstream_account_bindings b WHERE b.account_id=$2 AND b.valid_until IS NULL)`, id, identity.AccountID)
	if err != nil {
		return nil, upstreamPersistenceError(err)
	}
	if err = tx.Commit(); err != nil {
		return nil, upstreamPersistenceError(err)
	}
	return r.GetTarget(ctx, id)
}
