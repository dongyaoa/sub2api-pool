package repository

import (
	"context"
	"encoding/json"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

const storagePolicyColumns = `enabled,history_retention_days,snapshot_retention_days,last_cleanup_at,last_result`

func scanStoragePolicy(row interface{ Scan(...any) error }) (*service.UpstreamStoragePolicy, error) {
	p := &service.UpstreamStoragePolicy{}
	var data []byte
	if err := row.Scan(&p.Enabled, &p.HistoryRetentionDays, &p.SnapshotRetentionDays, &p.LastCleanupAt, &data); err != nil {
		return nil, err
	}
	if len(data) > 0 {
		if err := json.Unmarshal(data, &p.LastResult); err != nil {
			return nil, err
		}
	}
	return p, nil
}

func (r *upstreamCenterRepository) GetStoragePolicy(ctx context.Context) (*service.UpstreamStoragePolicy, error) {
	return scanStoragePolicy(r.db.QueryRowContext(ctx, `SELECT `+storagePolicyColumns+` FROM upstream_storage_policy WHERE id=1`))
}

func (r *upstreamCenterRepository) SaveStoragePolicy(ctx context.Context, in service.UpstreamStoragePolicyInput) (*service.UpstreamStoragePolicy, error) {
	return scanStoragePolicy(r.db.QueryRowContext(ctx, `UPDATE upstream_storage_policy SET enabled=$1,history_retention_days=$2,snapshot_retention_days=$3,updated_at=NOW() WHERE id=1 RETURNING `+storagePolicyColumns, in.Enabled, in.HistoryRetentionDays, in.SnapshotRetentionDays))
}

func (r *upstreamCenterRepository) RecordStorageCleanup(ctx context.Context, at time.Time, result *service.UpstreamStorageCleanupResult) error {
	data, err := json.Marshal(result)
	if err != nil {
		return err
	}
	// Do not let a slower earlier worker overwrite a later result.
	_, err = r.db.ExecContext(ctx, `UPDATE upstream_storage_policy SET last_cleanup_at=$1,last_result=$2::jsonb WHERE id=1 AND (last_cleanup_at IS NULL OR last_cleanup_at <= $1)`, at, string(data))
	return err
}
