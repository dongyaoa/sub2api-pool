package repository

import (
	"context"
	"database/sql"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
)

var _ service.IntelligenceMonitorLifecycleRepository = (*intelligenceMonitorRepository)(nil)

func (r *intelligenceMonitorRepository) DeletePelicanRun(ctx context.Context, id int64) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var planID int64
	// Match enqueue/completion's plan -> run lock order.
	err = tx.QueryRowContext(ctx, `SELECT p.id FROM intelligence_monitor_plans p JOIN intelligence_monitor_runs r ON r.plan_id=p.id WHERE r.id=$1 AND r.test_kind='pelican' FOR UPDATE OF p`, id).Scan(&planID)
	if err != nil {
		return intelligenceDBError(err)
	}
	var status string
	if err = tx.QueryRowContext(ctx, `SELECT status FROM intelligence_monitor_runs WHERE id=$1 FOR UPDATE`, id).Scan(&status); err != nil {
		return intelligenceDBError(err)
	}
	if status == "pending" || status == "running" {
		return service.ErrIntelligenceBusy
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM intelligence_monitor_runs WHERE id=$1`, id); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *intelligenceMonitorRepository) DeleteOAuthPlanPermanently(ctx context.Context, id int64) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err = lockManualOrderMembership(ctx, tx); err != nil {
		return err
	}
	var source string
	if err = tx.QueryRowContext(ctx, `SELECT source_type FROM intelligence_monitor_plans WHERE id=$1 FOR UPDATE`, id).Scan(&source); err != nil {
		return intelligenceDBError(err)
	}
	if source != "openai_oauth" {
		return service.ErrIntelligenceInvalid
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM intelligence_monitor_runs WHERE plan_id=$1`, id); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM intelligence_monitor_plans WHERE id=$1`, id); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *intelligenceMonitorRepository) ScheduleStatus(ctx context.Context) (*service.IntelligenceScheduleStatus, error) {
	status := &service.IntelligenceScheduleStatus{}
	err := r.db.QueryRowContext(ctx, `SELECT COUNT(*),COUNT(*) FILTER(WHERE enabled) FROM intelligence_monitor_plans WHERE deleted_at IS NULL`).Scan(&status.Total, &status.Enabled)
	return status, err
}

func (r *intelligenceMonitorRepository) SetPlansEnabled(ctx context.Context, enabled bool) (*service.IntelligenceScheduleUpdate, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	if err = lockManualOrderMembership(ctx, tx); err != nil {
		return nil, err
	}
	// Running requests finish normally. The existing enqueue version check
	// serializes schedule changes with source preparation. On resume the normal
	// scheduler rechecks source validity and weekly quota before enqueue.
	result, err := tx.ExecContext(ctx, `WITH locked AS MATERIALIZED (SELECT id FROM intelligence_monitor_plans WHERE deleted_at IS NULL ORDER BY id FOR UPDATE)
UPDATE intelligence_monitor_plans p SET enabled=$1,
next_run_at=CASE WHEN NOT $1 OR EXISTS(SELECT 1 FROM intelligence_monitor_runs r WHERE r.plan_id=p.id AND r.test_kind='pelican' AND r.status IN ('pending','running')) THEN NULL ELSE COALESCE(p.next_run_at,NOW()) END,
candy_next_run_at=CASE WHEN NOT $1 OR NOT p.candy_enabled OR EXISTS(SELECT 1 FROM intelligence_monitor_runs r WHERE r.plan_id=p.id AND r.test_kind='candy' AND r.status IN ('pending','running')) THEN NULL ELSE COALESCE(p.candy_next_run_at,NOW()) END,
updated_at=clock_timestamp() WHERE p.id IN (SELECT id FROM locked) AND p.enabled IS DISTINCT FROM $1`, enabled)
	if err != nil {
		return nil, err
	}
	status := &service.IntelligenceScheduleUpdate{}
	status.Updated, err = result.RowsAffected()
	if err != nil {
		return nil, err
	}
	if !enabled {
		// Withdraw scheduled work that has not started. Explicit manual requests
		// retain their intent, and a concurrently claimed run finishes normally.
		if _, err = tx.ExecContext(ctx, `DELETE FROM intelligence_monitor_runs r USING intelligence_monitor_plans p WHERE r.plan_id=p.id AND p.deleted_at IS NULL AND r.status='pending' AND r.trigger='scheduled'`); err != nil {
			return nil, err
		}
	}
	err = tx.QueryRowContext(ctx, `SELECT COUNT(*),COUNT(*) FILTER(WHERE enabled) FROM intelligence_monitor_plans WHERE deleted_at IS NULL`).Scan(&status.Total, &status.Enabled)
	if err != nil {
		return nil, err
	}
	return status, tx.Commit()
}

func (r *intelligenceMonitorRepository) LiveOAuthExecutionIDs(ctx context.Context, ids []int64) ([]int64, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT r.id FROM intelligence_monitor_runs r JOIN intelligence_monitor_plans p ON p.id=r.plan_id JOIN accounts a ON a.id=p.account_id WHERE r.id=ANY($1) AND r.status='running' AND p.source_type='openai_oauth' AND p.deleted_at IS NULL AND a.deleted_at IS NULL`, pq.Array(ids))
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	result := []int64{}
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			return nil, err
		}
		result = append(result, id)
	}
	return result, rows.Err()
}

// Caller holds the membership lock, also acquired by account deletion. This
// closes the service validation/create race without reading any credentials.
func validateIntelligenceOAuthAccountExists(ctx context.Context, tx *sql.Tx, accountID *int64) error {
	if accountID == nil {
		return service.ErrIntelligenceOAuthUnavailable
	}
	var exists bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM accounts a WHERE a.id=$1 AND a.deleted_at IS NULL)`, *accountID).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return service.ErrIntelligenceOAuthUnavailable
	}
	return nil
}

type intelligenceLifecycleExecutor interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

// Used inside AccountRepository.Delete's existing transaction, including the
// account management bulk-delete path. Deleting whole rows also removes HTML,
// raw responses, candy diagnostics and encrypted credentials.
func purgeAccountOAuthMonitors(ctx context.Context, tx intelligenceLifecycleExecutor, accountID int64) error {
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(251,0)`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `WITH locked AS MATERIALIZED (SELECT id FROM intelligence_monitor_plans WHERE source_type='openai_oauth' AND account_id=$1 ORDER BY id FOR UPDATE)
DELETE FROM intelligence_monitor_runs WHERE plan_id IN (SELECT id FROM locked)`, accountID); err != nil {
		return err
	}
	_, err := tx.ExecContext(ctx, `DELETE FROM intelligence_monitor_plans WHERE source_type='openai_oauth' AND account_id=$1`, accountID)
	return err
}
