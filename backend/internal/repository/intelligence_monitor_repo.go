package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
)

type intelligenceMonitorRepository struct{ db *sql.DB }

func NewIntelligenceMonitorRepository(db *sql.DB) service.IntelligenceMonitorRepository {
	return &intelligenceMonitorRepository{db: db}
}
func intelligenceDBError(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return service.ErrIntelligenceNotFound
	}
	var pg *pq.Error
	if errors.As(err, &pg) && pg.Code == "23505" {
		return service.ErrIntelligenceBusy
	}
	return err
}

const intelligencePlanColumns = `id,name,source_type,endpoint,api_key_encrypted,upstream_target_id,group_id,local_api_key_id,local_key_owner_id,supplier_note,group_note,rate_note,notes,api_mode,enabled,interval_seconds,timeout_seconds,created_by,last_run_at,next_run_at,created_at,updated_at,account_id`

func scanIntelligencePlan(row upstreamScanner) (*service.IntelligenceMonitorPlan, error) {
	p := new(service.IntelligenceMonitorPlan)
	err := row.Scan(&p.ID, &p.Name, &p.SourceType, &p.Endpoint, &p.APIKeyEncrypted, &p.UpstreamTargetID, &p.GroupID, &p.LocalAPIKeyID, &p.LocalKeyOwnerID, &p.SupplierNote, &p.GroupNote, &p.RateNote, &p.Notes, &p.APIMode, &p.Enabled, &p.IntervalSeconds, &p.TimeoutSeconds, &p.CreatedBy, &p.LastRunAt, &p.NextRunAt, &p.CreatedAt, &p.UpdatedAt, &p.AccountID)
	return p, intelligenceDBError(err)
}
func (r *intelligenceMonitorRepository) ListPlans(ctx context.Context) ([]*service.IntelligenceMonitorPlan, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT `+intelligencePlanColumns+` FROM intelligence_monitor_plans WHERE deleted_at IS NULL ORDER BY created_at DESC,id DESC`)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	out := []*service.IntelligenceMonitorPlan{}
	for rows.Next() {
		p, e := scanIntelligencePlan(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
func (r *intelligenceMonitorRepository) GetPlan(ctx context.Context, id int64) (*service.IntelligenceMonitorPlan, error) {
	return scanIntelligencePlan(r.db.QueryRowContext(ctx, `SELECT `+intelligencePlanColumns+` FROM intelligence_monitor_plans WHERE id=$1 AND deleted_at IS NULL`, id))
}

func (r *intelligenceMonitorRepository) SavePlan(ctx context.Context, p *service.IntelligenceMonitorPlan) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if p.ID > 0 {
		var busy bool
		var oldKeyID *int64
		err = tx.QueryRowContext(ctx, `SELECT local_api_key_id FROM intelligence_monitor_plans WHERE id=$1 AND deleted_at IS NULL FOR UPDATE`, p.ID).Scan(&oldKeyID)
		if err != nil {
			return intelligenceDBError(err)
		}
		if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM intelligence_monitor_runs WHERE plan_id=$1 AND status IN ('pending','running'))`, p.ID).Scan(&busy); err != nil {
			return err
		}
		if busy && !p.AllowWhileBusy {
			return service.ErrIntelligenceBusy
		}
		if oldKeyID != nil && (p.LocalAPIKeyID == nil || *oldKeyID != *p.LocalAPIKeyID) {
			if _, err = tx.ExecContext(ctx, `UPDATE api_keys SET status='disabled',updated_at=NOW() WHERE id=$1 AND deleted_at IS NULL`, *oldKeyID); err != nil {
				return err
			}
		}
	}
	args := []any{p.Name, p.SourceType, p.Endpoint, p.APIKeyEncrypted, p.UpstreamTargetID, p.GroupID, p.LocalAPIKeyID, p.LocalKeyOwnerID, p.SupplierNote, p.GroupNote, p.RateNote, p.Notes, p.APIMode, p.Enabled, p.IntervalSeconds, p.TimeoutSeconds, p.CreatedBy}
	if p.ID == 0 {
		args = append(args, p.AccountID)
		err = tx.QueryRowContext(ctx, `INSERT INTO intelligence_monitor_plans(name,source_type,endpoint,api_key_encrypted,upstream_target_id,group_id,local_api_key_id,local_key_owner_id,supplier_note,group_note,rate_note,notes,api_mode,enabled,interval_seconds,timeout_seconds,created_by,account_id,next_run_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,CASE WHEN $14 THEN NOW() ELSE NULL END) RETURNING id,created_at,updated_at,next_run_at`, args...).Scan(&p.ID, &p.CreatedAt, &p.UpdatedAt, &p.NextRunAt)
	} else {
		// created_by is immutable and therefore is not an UPDATE argument. Keep
		// placeholders contiguous: PostgreSQL cannot infer an unused $17 type.
		args = append(args[:16], p.AccountID, p.ID, p.UpdatedAt)
		err = tx.QueryRowContext(ctx, `UPDATE intelligence_monitor_plans SET name=$1,source_type=$2,endpoint=$3,api_key_encrypted=$4,upstream_target_id=$5,group_id=$6,local_api_key_id=$7,local_key_owner_id=$8,supplier_note=$9,group_note=$10,rate_note=$11,notes=$12,api_mode=$13,enabled=$14,interval_seconds=$15,timeout_seconds=$16,account_id=$17,next_run_at=CASE WHEN $14 THEN NOW() ELSE NULL END,updated_at=clock_timestamp() WHERE id=$18 AND updated_at=$19 AND deleted_at IS NULL RETURNING updated_at,next_run_at`, args...).Scan(&p.UpdatedAt, &p.NextRunAt)
	}
	if err != nil {
		return intelligenceDBError(err)
	}
	return tx.Commit()
}
func (r *intelligenceMonitorRepository) ArchivePlan(ctx context.Context, id int64) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var keyID *int64
	if err = tx.QueryRowContext(ctx, `SELECT local_api_key_id FROM intelligence_monitor_plans WHERE id=$1 AND deleted_at IS NULL FOR UPDATE`, id).Scan(&keyID); err != nil {
		return intelligenceDBError(err)
	}
	var busy bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM intelligence_monitor_runs WHERE plan_id=$1 AND status IN ('pending','running'))`, id).Scan(&busy); err != nil {
		return err
	}
	if busy {
		return service.ErrIntelligenceBusy
	}
	if keyID != nil {
		if _, err = tx.ExecContext(ctx, `UPDATE api_keys SET status='disabled',updated_at=NOW() WHERE id=$1 AND deleted_at IS NULL`, *keyID); err != nil {
			return err
		}
	}
	if _, err = tx.ExecContext(ctx, `UPDATE intelligence_monitor_plans SET deleted_at=NOW(),enabled=FALSE,next_run_at=NULL,api_key_encrypted='',updated_at=NOW() WHERE id=$1`, id); err != nil {
		return err
	}
	return tx.Commit()
}

const intelligenceRunColumns = `id,plan_id,plan_name,status,trigger,model,reasoning_effort,prompt,source_type,source_name,source_endpoint,source_snapshot,rate_snapshot,notes_snapshot,api_mode,timeout_seconds,request_key_encrypted,lease_token,started_at,finished_at,duration_ms,http_status,error,created_at`

func scanIntelligenceRun(row upstreamScanner, detail bool) (*service.IntelligenceMonitorRun, error) {
	run := new(service.IntelligenceMonitorRun)
	var source, rate, notes []byte
	args := []any{&run.ID, &run.PlanID, &run.PlanName, &run.Status, &run.Trigger, &run.Model, &run.ReasoningEffort, &run.Prompt, &run.SourceType, &run.SourceName, &run.SourceEndpoint, &source, &rate, &notes, &run.APIMode, &run.TimeoutSeconds, &run.RequestKeyEncrypted, &run.LeaseToken, &run.StartedAt, &run.FinishedAt, &run.DurationMs, &run.HTTPStatus, &run.Error, &run.CreatedAt}
	if detail {
		args = append(args, &run.HTML, &run.RawText)
	}
	if err := row.Scan(args...); err != nil {
		return nil, intelligenceDBError(err)
	}
	run.OAuth = run.SourceType == "openai_oauth"
	if err := json.Unmarshal(source, &run.SourceSnapshot); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(notes, &run.NotesSnapshot); err != nil {
		return nil, err
	}
	if len(rate) > 0 {
		if err := json.Unmarshal(rate, &run.RateSnapshot); err != nil {
			return nil, err
		}
	}
	return run, nil
}
func (r *intelligenceMonitorRepository) GetRun(ctx context.Context, id int64) (*service.IntelligenceMonitorRun, error) {
	return scanIntelligenceRun(r.db.QueryRowContext(ctx, `SELECT `+intelligenceRunColumns+`,html,raw_text FROM intelligence_monitor_runs WHERE id=$1`, id), true)
}
func (r *intelligenceMonitorRepository) ListRuns(ctx context.Context, q service.IntelligenceMonitorRunQuery) (*service.IntelligenceMonitorRunPage, error) {
	args := []any{}
	where := "TRUE"
	if q.PlanID != nil {
		args = append(args, *q.PlanID)
		where = "plan_id=$1"
	}
	out := &service.IntelligenceMonitorRunPage{Items: []*service.IntelligenceMonitorRun{}, Page: q.Page, PageSize: q.PageSize}
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM intelligence_monitor_runs WHERE `+where, args...).Scan(&out.Total); err != nil {
		return nil, err
	}
	args = append(args, q.PageSize, (q.Page-1)*q.PageSize)
	rows, err := r.db.QueryContext(ctx, `SELECT `+intelligenceRunColumns+` FROM intelligence_monitor_runs WHERE `+where+fmt.Sprintf(" ORDER BY created_at DESC,id DESC LIMIT $%d OFFSET $%d", len(args)-1, len(args)), args...)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		run, e := scanIntelligenceRun(rows, false)
		if e != nil {
			return nil, e
		}
		out.Items = append(out.Items, run)
	}
	return out, rows.Err()
}
func (r *intelligenceMonitorRepository) Enqueue(ctx context.Context, run *service.IntelligenceMonitorRun, scheduled bool) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var found int64
	if err = tx.QueryRowContext(ctx, `SELECT id FROM intelligence_monitor_plans WHERE id=$1 AND updated_at=$3 AND deleted_at IS NULL AND (NOT $2 OR (enabled AND next_run_at<=NOW())) FOR UPDATE`, run.PlanID, scheduled, run.PlanUpdatedAt).Scan(&found); err != nil {
		return intelligenceDBError(err)
	}
	source, err := json.Marshal(run.SourceSnapshot)
	if err != nil {
		return err
	}
	notes, err := json.Marshal(run.NotesSnapshot)
	if err != nil {
		return err
	}
	rate, err := json.Marshal(run.RateSnapshot)
	if err != nil {
		return err
	}
	err = tx.QueryRowContext(ctx, `INSERT INTO intelligence_monitor_runs(plan_id,plan_name,status,trigger,model,reasoning_effort,prompt,source_type,source_name,source_endpoint,source_snapshot,rate_snapshot,notes_snapshot,api_mode,timeout_seconds,request_key_encrypted) VALUES($1,$2,'pending',$3,$4,$5,$6,$7,$8,$9,$10::jsonb,$11::jsonb,$12::jsonb,$13,$14,$15) RETURNING id,created_at`, run.PlanID, run.PlanName, run.Trigger, run.Model, run.ReasoningEffort, run.Prompt, run.SourceType, run.SourceName, run.SourceEndpoint, string(source), string(rate), string(notes), run.APIMode, run.TimeoutSeconds, run.RequestKeyEncrypted).Scan(&run.ID, &run.CreatedAt)
	if err != nil {
		return intelligenceDBError(err)
	}
	if _, err = tx.ExecContext(ctx, `UPDATE intelligence_monitor_plans SET next_run_at=CASE WHEN enabled THEN NOW()+make_interval(secs=>interval_seconds) ELSE NULL END WHERE id=$1`, run.PlanID); err != nil {
		return err
	}
	run.Status = "pending"
	return tx.Commit()
}
func (r *intelligenceMonitorRepository) DuePlanIDs(ctx context.Context, limit int) ([]int64, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT id FROM intelligence_monitor_plans p WHERE enabled AND deleted_at IS NULL AND next_run_at<=NOW() AND NOT EXISTS(SELECT 1 FROM intelligence_monitor_runs r WHERE r.plan_id=p.id AND r.status IN ('pending','running')) ORDER BY next_run_at,id LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	ids := []int64{}
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
func (r *intelligenceMonitorRepository) ClaimNext(ctx context.Context, token string) (*service.IntelligenceMonitorRun, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	// Serialize only the short claim transaction to enforce TWO workers globally,
	// including when multiple app instances share this database.
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(245,1)`); err != nil {
		return nil, err
	}
	var active int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM intelligence_monitor_runs WHERE status='running' AND lease_until>NOW()`).Scan(&active); err != nil {
		return nil, err
	}
	if active >= 2 {
		return nil, nil
	}
	// The execution budget includes source preparation and persistence. Base the
	// lease on this run's immutable timeout, not the plan's current configuration.
	run, err := scanIntelligenceRun(tx.QueryRowContext(ctx, `UPDATE intelligence_monitor_runs SET status='running',started_at=NOW(),lease_token=$1,lease_until=NOW()+make_interval(secs=>timeout_seconds+$2) WHERE id=(SELECT id FROM intelligence_monitor_runs WHERE status='pending' ORDER BY created_at,id FOR UPDATE SKIP LOCKED LIMIT 1) RETURNING `+intelligenceRunColumns, token, service.IntelligenceMonitorLeaseGraceSeconds), false)
	if errors.Is(err, service.ErrIntelligenceNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return run, nil
}
func (r *intelligenceMonitorRepository) CompleteRun(ctx context.Context, run *service.IntelligenceMonitorRun) error {
	rate, err := json.Marshal(run.RateSnapshot)
	if err != nil {
		return err
	}
	source, err := json.Marshal(run.SourceSnapshot)
	if err != nil {
		return err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var planID int64
	// Keep the same plan -> run lock order as Enqueue to avoid a completion
	// waiting on a plan whose enqueuer is waiting on the active-run unique index.
	if err = tx.QueryRowContext(ctx, `SELECT id FROM intelligence_monitor_plans WHERE id=$1 FOR UPDATE`, run.PlanID).Scan(&planID); err != nil {
		return intelligenceDBError(err)
	}
	err = tx.QueryRowContext(ctx, `UPDATE intelligence_monitor_runs SET status=$3,finished_at=NOW(),duration_ms=EXTRACT(EPOCH FROM (NOW()-started_at))*1000,http_status=$4,error=$5,html=$6,raw_text=$7,rate_snapshot=$8::jsonb,source_snapshot=$9::jsonb,request_key_encrypted='',lease_token='',lease_until=NULL WHERE id=$1 AND lease_token=$2 AND status='running' RETURNING plan_id`, run.ID, run.LeaseToken, run.Status, run.HTTPStatus, run.Error, run.HTML, run.RawText, string(rate), string(source)).Scan(&planID)
	if err != nil {
		return intelligenceDBError(err)
	}
	if _, err = tx.ExecContext(ctx, `UPDATE intelligence_monitor_plans SET last_run_at=NOW(),next_run_at=CASE WHEN enabled AND deleted_at IS NULL THEN NOW()+make_interval(secs=>interval_seconds) ELSE NULL END WHERE id=$1`, planID); err != nil {
		return err
	}
	if err = pruneIntelligencePlanRuns(ctx, tx, planID); err != nil {
		return err
	}
	return tx.Commit()
}
func (r *intelligenceMonitorRepository) ExpireRuns(ctx context.Context) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	rows, err := tx.QueryContext(ctx, `SELECT id FROM intelligence_monitor_plans p WHERE EXISTS(SELECT 1 FROM intelligence_monitor_runs r WHERE r.plan_id=p.id AND r.status='running' AND r.lease_until<NOW()) ORDER BY id FOR UPDATE`)
	if err != nil {
		return err
	}
	ids := []int64{}
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			_ = rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	_ = rows.Close()
	if err != nil {
		return err
	}
	if len(ids) > 0 {
		if _, err = tx.ExecContext(ctx, `WITH expired AS (UPDATE intelligence_monitor_runs SET status='failed',finished_at=NOW(),duration_ms=EXTRACT(EPOCH FROM (NOW()-started_at))*1000,error='Execution interrupted or lease expired; this request was not automatically retried to avoid duplicate charges',request_key_encrypted='',lease_token='',lease_until=NULL WHERE plan_id=ANY($1) AND status='running' AND lease_until<NOW() RETURNING plan_id) UPDATE intelligence_monitor_plans SET last_run_at=NOW(),next_run_at=CASE WHEN enabled AND deleted_at IS NULL THEN NOW()+make_interval(secs=>interval_seconds) ELSE NULL END WHERE id IN (SELECT plan_id FROM expired)`, pq.Array(ids)); err != nil {
			return err
		}
		for _, id := range ids {
			if err = pruneIntelligencePlanRuns(ctx, tx, id); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}

// Caller holds the plan lock, matching completion/expiry/enqueue lock order.
// Active requests never enter the retention set, including an old queued run.
func pruneIntelligencePlanRuns(ctx context.Context, tx *sql.Tx, planID int64) error {
	_, err := tx.ExecContext(ctx, `DELETE FROM intelligence_monitor_runs WHERE id IN (SELECT id FROM intelligence_monitor_runs WHERE plan_id=$1 AND status IN ('succeeded','failed') ORDER BY created_at DESC,id DESC OFFSET $2)`, planID, service.IntelligenceMonitorRetainedRuns)
	return err
}

func (r *intelligenceMonitorRepository) PruneRuns(ctx context.Context) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	// Include archived plans so retention applies to all stored artifacts.
	rows, err := tx.QueryContext(ctx, `SELECT id FROM intelligence_monitor_plans p WHERE EXISTS(SELECT 1 FROM intelligence_monitor_runs r WHERE r.plan_id=p.id AND r.status IN ('succeeded','failed') OFFSET $1) ORDER BY id FOR UPDATE`, service.IntelligenceMonitorRetainedRuns)
	if err != nil {
		return err
	}
	ids := []int64{}
	for rows.Next() {
		var id int64
		if err = rows.Scan(&id); err != nil {
			_ = rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	_ = rows.Close()
	if err != nil {
		return err
	}
	for _, id := range ids {
		if err = pruneIntelligencePlanRuns(ctx, tx, id); err != nil {
			return err
		}
	}
	return tx.Commit()
}
