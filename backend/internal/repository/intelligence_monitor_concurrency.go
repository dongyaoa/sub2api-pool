package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

const intelligenceConcurrencySettingKey = service.IntelligenceMonitorConcurrencySettingKey

func decodeIntelligenceConcurrency(raw string) (service.IntelligenceMonitorConcurrency, error) {
	var limits service.IntelligenceMonitorConcurrency
	if err := json.Unmarshal([]byte(raw), &limits); err != nil {
		return limits, fmt.Errorf("decode intelligence concurrency setting: %w", err)
	}
	if limits.Validate() != nil {
		return limits, fmt.Errorf("invalid stored intelligence concurrency setting")
	}
	return limits, nil
}

func (r *intelligenceMonitorRepository) GetConcurrency(ctx context.Context) (*service.IntelligenceMonitorConcurrencySettings, error) {
	out := &service.IntelligenceMonitorConcurrencySettings{Source: "deployment"}
	var raw sql.NullString
	// One snapshot keeps the displayed limits and queue totals coherent. Finished
	// and expired runs do not occupy capacity in the claim transaction either.
	err := r.db.QueryRowContext(ctx, `SELECT
 (SELECT value FROM settings WHERE key=$1),
 COUNT(*) FILTER (WHERE test_kind='pelican' AND status='running' AND lease_until>NOW()),
 COUNT(*) FILTER (WHERE test_kind='pelican' AND status='pending'),
 COUNT(*) FILTER (WHERE test_kind='candy' AND status='running' AND lease_until>NOW()),
 COUNT(*) FILTER (WHERE test_kind='candy' AND status='pending')
 FROM intelligence_monitor_runs WHERE status IN ('pending','running')`, intelligenceConcurrencySettingKey).
		Scan(&raw, &out.PelicanRunning, &out.PelicanPending, &out.CandyRunning, &out.CandyPending)
	if err != nil {
		return nil, err
	}
	if raw.Valid {
		out.IntelligenceMonitorConcurrency, err = decodeIntelligenceConcurrency(raw.String)
		if err != nil {
			return nil, err
		}
		out.Source = "database"
	}
	return out, nil
}

func (r *intelligenceMonitorRepository) SaveConcurrency(ctx context.Context, limits service.IntelligenceMonitorConcurrency) error {
	if err := limits.Validate(); err != nil {
		return err
	}
	raw, err := json.Marshal(limits)
	if err != nil {
		return err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	// Share the claim lock, so lowering a limit cannot race an old-limit claim.
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(245,1)`); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO settings(key,value,updated_at) VALUES($1,$2,NOW()) ON CONFLICT(key) DO UPDATE SET value=EXCLUDED.value,updated_at=EXCLUDED.updated_at`, intelligenceConcurrencySettingKey, string(raw)); err != nil {
		return err
	}
	return tx.Commit()
}

func intelligenceConcurrencyForClaim(ctx context.Context, tx *sql.Tx, kind string, fallback int) (int, error) {
	var raw string
	err := tx.QueryRowContext(ctx, `SELECT value FROM settings WHERE key=$1`, intelligenceConcurrencySettingKey).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return fallback, nil
	}
	if err != nil {
		return 0, err
	}
	limits, err := decodeIntelligenceConcurrency(raw)
	if err != nil {
		return 0, err
	}
	if kind == service.IntelligenceMonitorTestCandy {
		return limits.CandyMaxConcurrency, nil
	}
	return limits.MaxConcurrency, nil
}
