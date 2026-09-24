package repository

import (
	"context"
	"database/sql"
	"fmt"
)

// ensureRetiredBatchImageJobsDrained prevents removing the only workers that can
// finish pending jobs or release their balances. Terminal jobs can still have
// frozen funds after a failed release, so balances must be checked independently.
// Run after migrations have created the legacy tables, before starting services.
func ensureRetiredBatchImageJobsDrained(ctx context.Context, db *sql.DB) error {
	var frozenUsers, pendingJobs int64
	if err := db.QueryRowContext(ctx, `
		SELECT
			(SELECT COUNT(*) FROM users WHERE COALESCE(frozen_balance, 0) > 0),
			(SELECT COUNT(*) FROM batch_image_jobs
			 WHERE status NOT IN ('completed', 'failed', 'cancelled', 'output_deleted'))
	`).Scan(&frozenUsers, &pendingJobs); err != nil {
		return fmt.Errorf("check retired batch image activity: %w", err)
	}
	if frozenUsers > 0 || pendingJobs > 0 {
		return fmt.Errorf(
			"cannot start with batch image generation removed: %d users have frozen balances and %d batch image jobs are pending; first use the previous version to complete or cancel these jobs and settle or release all frozen balances, then restart this version",
			frozenUsers, pendingJobs,
		)
	}
	return nil
}
