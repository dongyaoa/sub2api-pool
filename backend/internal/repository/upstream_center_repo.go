package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
)

type upstreamCenterRepository struct{ db *sql.DB }

func NewUpstreamCenterRepository(db *sql.DB) service.UpstreamCenterRepository {
	return &upstreamCenterRepository{db: db}
}

func upstreamPersistenceError(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return service.ErrUpstreamNotFound
	}
	var pg *pq.Error
	if errors.As(err, &pg) && pg.Code == "23505" {
		if pg.Constraint == "idx_upstream_targets_unique_key" {
			return service.ErrUpstreamDuplicateKey
		}
		return service.ErrUpstreamBindingConflict
	}
	return err
}

func (r *upstreamCenterRepository) ListSuppliers(ctx context.Context) ([]*service.UpstreamSupplier, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id,name,website,notes,created_at,updated_at FROM upstream_suppliers WHERE deleted_at IS NULL ORDER BY sort_order ASC NULLS LAST,id`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := make([]*service.UpstreamSupplier, 0)
	for rows.Next() {
		s := new(service.UpstreamSupplier)
		if err = rows.Scan(&s.ID, &s.Name, &s.Website, &s.Notes, &s.CreatedAt, &s.UpdatedAt); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}
func (r *upstreamCenterRepository) GetSupplier(ctx context.Context, id int64) (*service.UpstreamSupplier, error) {
	s := new(service.UpstreamSupplier)
	err := r.db.QueryRowContext(ctx, `SELECT id,name,website,notes,created_at,updated_at FROM upstream_suppliers WHERE id=$1 AND deleted_at IS NULL`, id).Scan(&s.ID, &s.Name, &s.Website, &s.Notes, &s.CreatedAt, &s.UpdatedAt)
	return s, upstreamPersistenceError(err)
}
func (r *upstreamCenterRepository) SaveSupplier(ctx context.Context, s *service.UpstreamSupplier) error {
	if s.ID == 0 {
		tx, err := r.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer func() { _ = tx.Rollback() }()
		if err = lockManualOrderMembership(ctx, tx); err != nil {
			return err
		}
		if err = tx.QueryRowContext(ctx, `INSERT INTO upstream_suppliers(name,website,notes) VALUES($1,$2,$3) RETURNING id,created_at,updated_at`, s.Name, s.Website, s.Notes).Scan(&s.ID, &s.CreatedAt, &s.UpdatedAt); err != nil {
			return err
		}
		return tx.Commit()
	}
	return upstreamPersistenceError(r.db.QueryRowContext(ctx, `UPDATE upstream_suppliers SET name=$2,website=$3,notes=$4,updated_at=NOW() WHERE id=$1 AND deleted_at IS NULL RETURNING updated_at`, s.ID, s.Name, s.Website, s.Notes).Scan(&s.UpdatedAt))
}
func (r *upstreamCenterRepository) ArchiveSupplier(ctx context.Context, id int64) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err = lockManualOrderMembership(ctx, tx); err != nil {
		return err
	}
	// Match SaveTarget's supplier -> target lock order. The supplier lock also
	// prevents a new target from appearing after the busy-check snapshot.
	var found int64
	if err = tx.QueryRowContext(ctx, `SELECT id FROM upstream_suppliers WHERE id=$1 AND deleted_at IS NULL FOR UPDATE`, id).Scan(&found); err != nil {
		return upstreamPersistenceError(err)
	}
	locked, err := tx.QueryContext(ctx, `SELECT lease_until > NOW() FROM upstream_targets WHERE supplier_id=$1 AND deleted_at IS NULL FOR UPDATE`, id)
	if err != nil {
		return err
	}
	for locked.Next() {
		var busy sql.NullBool
		if err = locked.Scan(&busy); err != nil {
			_ = locked.Close()
			return err
		}
		if busy.Valid && busy.Bool {
			_ = locked.Close()
			return service.ErrUpstreamBusy
		}
	}
	err = locked.Err()
	_ = locked.Close()
	if err != nil {
		return err
	}
	if err = tx.QueryRowContext(ctx, `UPDATE upstream_suppliers SET deleted_at=NOW(),updated_at=NOW() WHERE id=$1 AND deleted_at IS NULL RETURNING id`, id).Scan(&found); err != nil {
		return upstreamPersistenceError(err)
	}
	if _, err = tx.ExecContext(ctx, `UPDATE upstream_targets SET deleted_at=NOW(),enabled=FALSE,next_check_at=NULL,check_token='',lease_until=NULL,updated_at=NOW() WHERE supplier_id=$1 AND deleted_at IS NULL`, id); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE upstream_account_bindings SET valid_until=NOW() WHERE target_id IN (SELECT id FROM upstream_targets WHERE supplier_id=$1) AND valid_until IS NULL`, id); err != nil {
		return err
	}
	return tx.Commit()
}

const upstreamTargetColumns = `id,supplier_id,name,provider,api_mode,endpoint,api_key_encrypted,models,enabled,interval_seconds,timeout_seconds,degraded_threshold_ms,wallet_ref,notes,last_checked_at,next_check_at,created_at,updated_at`

type upstreamScanner interface{ Scan(...any) error }

func scanUpstreamTarget(row upstreamScanner) (*service.UpstreamTarget, error) {
	t := new(service.UpstreamTarget)
	var models []byte
	err := row.Scan(&t.ID, &t.SupplierID, &t.Name, &t.Provider, &t.APIMode, &t.Endpoint, &t.APIKeyEncrypted, &models, &t.Enabled, &t.IntervalSeconds, &t.TimeoutSeconds, &t.DegradedThresholdMs, &t.WalletRef, &t.Notes, &t.LastCheckedAt, &t.NextCheckAt, &t.CreatedAt, &t.UpdatedAt)
	if err != nil {
		return nil, upstreamPersistenceError(err)
	}
	if err = json.Unmarshal(models, &t.Models); err != nil {
		return nil, fmt.Errorf("decode upstream models: %w", err)
	}
	t.AccountIDs = []int64{}
	t.Statistics = []*service.UpstreamModelStatistics{}
	return t, nil
}
func (r *upstreamCenterRepository) ListTargets(ctx context.Context) ([]*service.UpstreamTarget, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT `+upstreamTargetColumns+` FROM upstream_targets WHERE deleted_at IS NULL ORDER BY sort_order ASC NULLS LAST,id`)
	if err != nil {
		return nil, err
	}
	out := make([]*service.UpstreamTarget, 0)
	for rows.Next() {
		t, e := scanUpstreamTarget(rows)
		if e != nil {
			_ = rows.Close()
			return nil, e
		}
		out = append(out, t)
	}
	err = rows.Err()
	_ = rows.Close()
	if err != nil {
		return nil, err
	}
	if err = r.loadBindings(ctx, out); err != nil {
		return nil, err
	}
	return out, nil
}
func (r *upstreamCenterRepository) GetTarget(ctx context.Context, id int64) (*service.UpstreamTarget, error) {
	t, err := scanUpstreamTarget(r.db.QueryRowContext(ctx, `SELECT `+upstreamTargetColumns+` FROM upstream_targets WHERE id=$1 AND deleted_at IS NULL`, id))
	if err != nil {
		return nil, err
	}
	if err = r.loadBindings(ctx, []*service.UpstreamTarget{t}); err != nil {
		return nil, err
	}
	return t, nil
}
func (r *upstreamCenterRepository) loadBindings(ctx context.Context, targets []*service.UpstreamTarget) error {
	ids := make([]int64, 0, len(targets))
	byID := map[int64]*service.UpstreamTarget{}
	for _, t := range targets {
		ids = append(ids, t.ID)
		byID[t.ID] = t
	}
	if len(ids) == 0 {
		return nil
	}
	rows, err := r.db.QueryContext(ctx, `SELECT target_id,account_id FROM upstream_account_bindings WHERE target_id=ANY($1) AND valid_until IS NULL ORDER BY account_id`, pq.Array(ids))
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var tid, aid int64
		if err = rows.Scan(&tid, &aid); err != nil {
			return err
		}
		byID[tid].AccountIDs = append(byID[tid].AccountIDs, aid)
	}
	return rows.Err()
}

func (r *upstreamCenterRepository) SaveTarget(ctx context.Context, t *service.UpstreamTarget) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err = lockManualOrderMembership(ctx, tx); err != nil {
		return err
	}
	supplierName := ""
	if t.SupplierID != nil {
		if err = tx.QueryRowContext(ctx, `SELECT name FROM upstream_suppliers WHERE id=$1 AND deleted_at IS NULL FOR SHARE`, *t.SupplierID).Scan(&supplierName); err != nil {
			return upstreamPersistenceError(err)
		}
	}
	// Hold the account row while checking the exact credential snapshot that the
	// service validated. An account edit cannot race a new attribution binding.
	for _, id := range t.AccountIDs {
		expected, ok := t.BindingCredentials[id]
		if !ok {
			return service.ErrUpstreamBindingConflict
		}
		var kind, key, base string
		err = tx.QueryRowContext(ctx, `SELECT type,COALESCE(credentials->>'api_key',''),COALESCE(credentials->>'base_url','') FROM accounts WHERE id=$1 AND deleted_at IS NULL FOR SHARE`, id).Scan(&kind, &key, &base)
		if err != nil || kind != "apikey" || key != expected.APIKey || base != expected.BaseURL {
			return service.ErrUpstreamBindingConflict
		}
	}
	models, err := json.Marshal(t.Models)
	if err != nil {
		return err
	}
	args := []any{t.SupplierID, t.Name, t.Provider, t.APIMode, t.Endpoint, t.APIKeyEncrypted, string(models), t.Enabled, t.IntervalSeconds, t.TimeoutSeconds, t.DegradedThresholdMs, t.WalletRef, t.Notes, t.APIKeyFingerprint}
	if t.ID == 0 {
		err = tx.QueryRowContext(ctx, `INSERT INTO upstream_targets(supplier_id,name,provider,api_mode,endpoint,api_key_encrypted,models,enabled,interval_seconds,timeout_seconds,degraded_threshold_ms,wallet_ref,notes,api_key_fingerprint,next_check_at) VALUES($1,$2,$3,$4,$5,$6,$7::jsonb,$8,$9,$10,$11,$12,$13,$14,CASE WHEN $8 THEN NOW() ELSE NULL END) RETURNING id,created_at,updated_at,next_check_at`, args...).Scan(&t.ID, &t.CreatedAt, &t.UpdatedAt, &t.NextCheckAt)
	} else {
		args = append(args, t.ID, t.UpdatedAt)
		// A new financial identity has no cached balance or billing observation.
		// Queue its first sync immediately, retaining the cadence for cosmetic edits.
		err = tx.QueryRowContext(ctx, `UPDATE upstream_targets SET supplier_id=$1,name=$2,provider=$3,api_mode=$4,endpoint=$5,api_key_encrypted=$6,models=$7::jsonb,enabled=$8,interval_seconds=$9,timeout_seconds=$10,degraded_threshold_ms=$11,wallet_ref=$12,notes=$13,api_key_fingerprint=$14,next_check_at=CASE WHEN $8 THEN NOW() ELSE NULL END,balance_next_sync_at=CASE WHEN supplier_id IS DISTINCT FROM $1::bigint OR provider IS DISTINCT FROM $3::varchar OR endpoint IS DISTINCT FROM $5::varchar OR api_key_encrypted IS DISTINCT FROM $6::text OR wallet_ref IS DISTINCT FROM $12::varchar THEN NOW() ELSE balance_next_sync_at END,sort_order=CASE WHEN supplier_id IS DISTINCT FROM $1::bigint THEN NULL ELSE sort_order END,lease_until=NULL,check_token='',updated_at=clock_timestamp() WHERE id=$15 AND updated_at=$16 AND deleted_at IS NULL AND (lease_until IS NULL OR lease_until < NOW()) RETURNING updated_at,next_check_at`, args...).Scan(&t.UpdatedAt, &t.NextCheckAt)
		if errors.Is(err, sql.ErrNoRows) {
			return service.ErrUpstreamBusy
		}
	}
	if err != nil {
		return upstreamPersistenceError(err)
	}
	if t.ResetBindings {
		_, err = tx.ExecContext(ctx, `UPDATE upstream_account_bindings SET valid_until=NOW() WHERE target_id=$1 AND valid_until IS NULL`, t.ID)
	} else {
		_, err = tx.ExecContext(ctx, `UPDATE upstream_account_bindings SET valid_until=NOW() WHERE target_id=$1 AND valid_until IS NULL AND NOT(account_id=ANY($2))`, t.ID, pq.Array(t.AccountIDs))
	}
	if err != nil {
		return err
	}
	for _, id := range t.AccountIDs {
		_, err = tx.ExecContext(ctx, `INSERT INTO upstream_account_bindings(target_id,account_id,supplier_id,target_name,supplier_name) SELECT $1,$2,$3,$4,$5 WHERE NOT EXISTS(SELECT 1 FROM upstream_account_bindings WHERE target_id=$1 AND account_id=$2 AND valid_until IS NULL)`, t.ID, id, t.SupplierID, t.Name, supplierName)
		if err != nil {
			return upstreamPersistenceError(err)
		}
	}
	return upstreamPersistenceError(tx.Commit())
}

func (r *upstreamCenterRepository) ArchiveTarget(ctx context.Context, id int64) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err = lockManualOrderMembership(ctx, tx); err != nil {
		return err
	}
	var found int64
	err = tx.QueryRowContext(ctx, `UPDATE upstream_targets SET deleted_at=NOW(),enabled=FALSE,next_check_at=NULL,lease_until=NULL,check_token='',updated_at=NOW() WHERE id=$1 AND deleted_at IS NULL AND (lease_until IS NULL OR lease_until < NOW()) RETURNING id`, id).Scan(&found)
	if errors.Is(err, sql.ErrNoRows) {
		var exists bool
		if e := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM upstream_targets WHERE id=$1 AND deleted_at IS NULL)`, id).Scan(&exists); e != nil {
			return e
		}
		if exists {
			return service.ErrUpstreamBusy
		}
	}
	if err != nil {
		return upstreamPersistenceError(err)
	}
	if _, err = tx.ExecContext(ctx, `UPDATE upstream_account_bindings SET valid_until=NOW() WHERE target_id=$1 AND valid_until IS NULL`, id); err != nil {
		return err
	}
	return tx.Commit()
}

const upstreamHistoryColumns = `id,target_id,model,status,latency_ms,ping_latency_ms,http_status,message,checked_at,cost,cost_source`

func scanUpstreamHistory(row upstreamScanner) (*service.UpstreamHistoryRecord, error) {
	h := new(service.UpstreamHistoryRecord)
	err := row.Scan(&h.ID, &h.TargetID, &h.Model, &h.Status, &h.LatencyMs, &h.PingLatencyMs, &h.HTTPStatus, &h.Message, &h.CheckedAt, &h.Cost, &h.CostSource)
	return h, err
}

func (r *upstreamCenterRepository) PopulateStatistics(ctx context.Context, targets []*service.UpstreamTarget, from time.Time) error {
	ids := make([]int64, 0, len(targets))
	stats := map[int64]map[string]*service.UpstreamModelStatistics{}
	for _, t := range targets {
		ids = append(ids, t.ID)
		stats[t.ID] = map[string]*service.UpstreamModelStatistics{}
		t.Statistics = []*service.UpstreamModelStatistics{}
		for _, model := range t.Models {
			s := &service.UpstreamModelStatistics{Model: model, Status: "unknown", Timeline: []*service.UpstreamHistoryRecord{}}
			stats[t.ID][model] = s
			t.Statistics = append(t.Statistics, s)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	rows, err := r.db.QueryContext(ctx, `SELECT target_id,model,
 COUNT(*) FILTER(WHERE checked_at >= $2),
 COUNT(*) FILTER(WHERE checked_at >= $2 AND status IN ('operational','degraded')),
 AVG(latency_ms) FILTER(WHERE checked_at >= $2 AND status IN ('operational','degraded')),
 percentile_cont(0.95) WITHIN GROUP(ORDER BY latency_ms) FILTER(WHERE checked_at >= $2 AND status IN ('operational','degraded')),
 COUNT(*) FILTER(WHERE checked_at >= NOW()-INTERVAL '7 days'),
 COUNT(*) FILTER(WHERE checked_at >= NOW()-INTERVAL '7 days' AND status IN ('operational','degraded'))
 FROM upstream_monitor_history WHERE target_id=ANY($1) AND checked_at >= LEAST($2,NOW()-INTERVAL '7 days') AND checked_at <= NOW()
 GROUP BY target_id,model`, pq.Array(ids), from)
	if err != nil {
		return err
	}
	for rows.Next() {
		var tid, n, success, samples7d, success7d int64
		var model string
		var avg, p95 *float64
		if err = rows.Scan(&tid, &model, &n, &success, &avg, &p95, &samples7d, &success7d); err != nil {
			_ = rows.Close()
			return err
		}
		if s := stats[tid][model]; s != nil {
			s.SampleCount = n
			s.SuccessCount = success
			s.AvgLatencyMs = avg
			s.P95LatencyMs = p95
			if n > 0 {
				v := float64(success) * 100 / float64(n)
				s.Availability = &v
			}
			if samples7d > 0 {
				v := float64(success7d) * 100 / float64(samples7d)
				s.Availability7d = &v
			}
		}
	}
	err = rows.Err()
	_ = rows.Close()
	if err != nil {
		return err
	}
	// Lateral index scans bound work to 60 entries per configured model instead
	// of ranking the entire retained history on every dashboard refresh.
	prefixed := strings.ReplaceAll(upstreamHistoryColumns, ",", ",h.")
	rows, err = r.db.QueryContext(ctx, `SELECT h.`+prefixed+` FROM upstream_targets t CROSS JOIN LATERAL jsonb_array_elements_text(t.models) m(model) CROSS JOIN LATERAL (SELECT `+upstreamHistoryColumns+` FROM upstream_monitor_history WHERE target_id=t.id AND model=m.model ORDER BY checked_at DESC,id DESC LIMIT 60) h WHERE t.id=ANY($1) ORDER BY h.checked_at,h.id`, pq.Array(ids))
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		h, e := scanUpstreamHistory(rows)
		if e != nil {
			return e
		}
		if s := stats[h.TargetID][h.Model]; s != nil {
			s.Timeline = append(s.Timeline, h)
			s.Status = h.Status
			s.LatestLatencyMs = h.LatencyMs
			at := h.CheckedAt
			s.LastCheckedAt = &at
		}
	}
	return rows.Err()
}

func (r *upstreamCenterRepository) History(ctx context.Context, q service.UpstreamHistoryQuery) (*service.UpstreamHistoryPage, error) {
	where := `target_id=$1`
	args := []any{q.TargetID}
	if q.Model != "" {
		args = append(args, q.Model)
		where += fmt.Sprintf(" AND model=$%d", len(args))
	}
	if q.From != nil {
		args = append(args, *q.From)
		where += fmt.Sprintf(" AND checked_at >= $%d", len(args))
	}
	if q.To != nil {
		args = append(args, *q.To)
		where += fmt.Sprintf(" AND checked_at < $%d", len(args))
	}
	page := &service.UpstreamHistoryPage{Items: []*service.UpstreamHistoryRecord{}, Page: q.Page, PageSize: q.PageSize}
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM upstream_monitor_history WHERE `+where, args...).Scan(&page.Total); err != nil {
		return nil, err
	}
	args = append(args, q.PageSize, (q.Page-1)*q.PageSize)
	rows, err := r.db.QueryContext(ctx, `SELECT `+upstreamHistoryColumns+` FROM upstream_monitor_history WHERE `+where+fmt.Sprintf(" ORDER BY checked_at DESC,id DESC LIMIT $%d OFFSET $%d", len(args)-1, len(args)), args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		h, e := scanUpstreamHistory(rows)
		if e != nil {
			return nil, e
		}
		page.Items = append(page.Items, h)
	}
	return page, rows.Err()
}

func (r *upstreamCenterRepository) DueTargetIDs(ctx context.Context, limit int) ([]int64, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id FROM upstream_targets WHERE deleted_at IS NULL AND enabled AND next_check_at <= NOW() AND (lease_until IS NULL OR lease_until < NOW()) ORDER BY next_check_at,id LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := []int64{}
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}
func (r *upstreamCenterRepository) ClaimCheck(ctx context.Context, id int64, token string, manual bool) (bool, error) {
	result, err := r.db.ExecContext(ctx, `UPDATE upstream_targets SET lease_until=NOW()+INTERVAL '10 minutes',check_token=$2 WHERE id=$1 AND deleted_at IS NULL AND (lease_until IS NULL OR lease_until < NOW()) AND ($3 OR (enabled AND next_check_at <= NOW()))`, id, token, manual)
	if err != nil {
		return false, err
	}
	n, err := result.RowsAffected()
	return n > 0, err
}
func (r *upstreamCenterRepository) CompleteCheck(ctx context.Context, id int64, token string, history []*service.UpstreamHistoryRecord) (bool, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()
	var supplierID *int64
	var name, supplierName string
	err = tx.QueryRowContext(ctx, `UPDATE upstream_targets SET last_checked_at=NOW(),next_check_at=CASE WHEN enabled THEN NOW()+make_interval(secs => interval_seconds) ELSE NULL END,lease_until=NULL,check_token='' WHERE id=$1 AND check_token=$2 AND deleted_at IS NULL RETURNING supplier_id,name,COALESCE((SELECT name FROM upstream_suppliers WHERE id=supplier_id),'')`, id, token).Scan(&supplierID, &name, &supplierName)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	for _, h := range history {
		err = tx.QueryRowContext(ctx, `INSERT INTO upstream_monitor_history(target_id,supplier_id,target_name,supplier_name,model,status,latency_ms,ping_latency_ms,http_status,message,checked_at,cost,cost_source) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13) RETURNING id`, id, supplierID, name, supplierName, h.Model, h.Status, h.LatencyMs, h.PingLatencyMs, h.HTTPStatus, h.Message, h.CheckedAt, h.Cost, h.CostSource).Scan(&h.ID)
		if err != nil {
			return false, err
		}
	}
	if err = tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}
func (r *upstreamCenterRepository) ReleaseCheck(ctx context.Context, id int64, token string) error {
	_, err := r.db.ExecContext(ctx, `UPDATE upstream_targets SET lease_until=NULL,check_token='',next_check_at=CASE WHEN enabled THEN NOW()+make_interval(secs => interval_seconds) ELSE NULL END WHERE id=$1 AND check_token=$2`, id, token)
	return err
}
