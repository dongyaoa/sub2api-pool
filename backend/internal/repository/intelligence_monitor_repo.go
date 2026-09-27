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

const intelligencePlanColumns = `id,name,source_type,endpoint,api_key_encrypted,upstream_target_id,group_id,local_api_key_id,local_key_owner_id,supplier_note,group_note,rate_note,notes,api_mode,enabled,interval_seconds,timeout_seconds,created_by,last_run_at,next_run_at,created_at,updated_at,account_id,candy_enabled,candy_interval_seconds,candy_last_run_at,candy_next_run_at,local_api_key_borrowed`

func scanIntelligencePlan(row upstreamScanner) (*service.IntelligenceMonitorPlan, error) {
	p := new(service.IntelligenceMonitorPlan)
	err := row.Scan(&p.ID, &p.Name, &p.SourceType, &p.Endpoint, &p.APIKeyEncrypted, &p.UpstreamTargetID, &p.GroupID, &p.LocalAPIKeyID, &p.LocalKeyOwnerID, &p.SupplierNote, &p.GroupNote, &p.RateNote, &p.Notes, &p.APIMode, &p.Enabled, &p.IntervalSeconds, &p.TimeoutSeconds, &p.CreatedBy, &p.LastRunAt, &p.NextRunAt, &p.CreatedAt, &p.UpdatedAt, &p.AccountID, &p.CandyEnabled, &p.CandyIntervalSeconds, &p.CandyLastRunAt, &p.CandyNextRunAt, &p.LocalAPIKeyBorrowed)
	return p, intelligenceDBError(err)
}
func (r *intelligenceMonitorRepository) ListPlans(ctx context.Context) ([]*service.IntelligenceMonitorPlan, error) {
	return r.listPlans(ctx, "deleted_at IS NULL")
}

func (r *intelligenceMonitorRepository) ListPlansForUpstream(ctx context.Context, targetID int64) ([]*service.IntelligenceMonitorPlan, error) {
	if targetID <= 0 {
		return nil, service.ErrIntelligenceInvalid
	}
	return r.listPlans(ctx, "deleted_at IS NULL AND source_type='upstream' AND upstream_target_id=$1", targetID)
}

// where is an internal constant; source filtering precedes all summary reads.
func (r *intelligenceMonitorRepository) listPlans(ctx context.Context, where string, args ...any) ([]*service.IntelligenceMonitorPlan, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT `+intelligencePlanColumns+` FROM intelligence_monitor_plans WHERE `+where+` ORDER BY sort_order ASC NULLS LAST,created_at DESC,id DESC`, args...)
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
	if p.CandyIntervalSeconds == 0 {
		p.CandyIntervalSeconds = service.IntelligenceMonitorCandyDefaultIntervalSeconds
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err = lockManualOrderMembership(ctx, tx); err != nil {
		return err
	}
	var oldSource string
	var oldTargetID *int64
	if p.ID > 0 {
		var busy bool
		var oldKeyID *int64
		var oldBorrowed bool
		err = tx.QueryRowContext(ctx, `SELECT local_api_key_id,source_type,upstream_target_id,local_api_key_borrowed FROM intelligence_monitor_plans WHERE id=$1 AND deleted_at IS NULL FOR UPDATE`, p.ID).Scan(&oldKeyID, &oldSource, &oldTargetID, &oldBorrowed)
		if err != nil {
			return intelligenceDBError(err)
		}
		if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM intelligence_monitor_runs WHERE plan_id=$1 AND status IN ('pending','running'))`, p.ID).Scan(&busy); err != nil {
			return err
		}
		if busy && !p.AllowWhileBusy {
			return service.ErrIntelligenceBusy
		}
		if !oldBorrowed && oldKeyID != nil && (p.LocalAPIKeyID == nil || *oldKeyID != *p.LocalAPIKeyID) {
			if _, err = tx.ExecContext(ctx, `UPDATE api_keys SET status='disabled',updated_at=NOW() WHERE id=$1 AND deleted_at IS NULL`, *oldKeyID); err != nil {
				return err
			}
		}
	}
	if p.SourceType == "upstream" && p.UpstreamTargetID != nil && (p.ID == 0 || oldSource != "upstream" || oldTargetID == nil || *oldTargetID != *p.UpstreamTargetID) {
		// The membership lock serializes create/move/archive across all writers.
		// Legacy duplicates remain editable in place; only entering a new target
		// is rejected when it already has a live (including paused) plan.
		var exists bool
		err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM intelligence_monitor_plans WHERE source_type='upstream' AND upstream_target_id=$1 AND deleted_at IS NULL AND id<>$2)`, *p.UpstreamTargetID, p.ID).Scan(&exists)
		if err != nil {
			return err
		}
		if exists {
			return service.ErrIntelligenceUpstreamPlanExists
		}
	}
	args := []any{p.Name, p.SourceType, p.Endpoint, p.APIKeyEncrypted, p.UpstreamTargetID, p.GroupID, p.LocalAPIKeyID, p.LocalKeyOwnerID, p.SupplierNote, p.GroupNote, p.RateNote, p.Notes, p.APIMode, p.Enabled, p.IntervalSeconds, p.TimeoutSeconds, p.CreatedBy}
	if p.ID == 0 {
		args = append(args, p.AccountID, p.CandyEnabled, p.CandyIntervalSeconds, p.LocalAPIKeyBorrowed)
		err = tx.QueryRowContext(ctx, `INSERT INTO intelligence_monitor_plans(name,source_type,endpoint,api_key_encrypted,upstream_target_id,group_id,local_api_key_id,local_key_owner_id,supplier_note,group_note,rate_note,notes,api_mode,enabled,interval_seconds,timeout_seconds,created_by,account_id,candy_enabled,candy_interval_seconds,local_api_key_borrowed,next_run_at,candy_next_run_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,CASE WHEN $14 THEN NOW() ELSE NULL END,CASE WHEN $14 AND $19 THEN NOW() ELSE NULL END) RETURNING id,created_at,updated_at,next_run_at,candy_next_run_at`, args...).Scan(&p.ID, &p.CreatedAt, &p.UpdatedAt, &p.NextRunAt, &p.CandyNextRunAt)
	} else {
		// created_by is immutable and therefore is not an UPDATE argument. Keep
		// placeholders contiguous: PostgreSQL cannot infer an unused $17 type.
		args = append(args[:16], p.AccountID, p.CandyEnabled, p.CandyIntervalSeconds, p.LocalAPIKeyBorrowed, p.ID, p.UpdatedAt)
		err = tx.QueryRowContext(ctx, `UPDATE intelligence_monitor_plans p SET name=$1,source_type=$2,endpoint=$3,api_key_encrypted=$4,upstream_target_id=$5,group_id=$6,local_api_key_id=$7,local_key_owner_id=$8,supplier_note=$9,group_note=$10,rate_note=$11,notes=$12,api_mode=$13,enabled=$14,interval_seconds=$15,timeout_seconds=$16,account_id=$17,candy_enabled=$18,candy_interval_seconds=$19,local_api_key_borrowed=$20,
next_run_at=CASE WHEN NOT $14 OR EXISTS(SELECT 1 FROM intelligence_monitor_runs r WHERE r.plan_id=p.id AND r.test_kind='pelican' AND r.status IN ('pending','running')) THEN NULL WHEN NOT enabled THEN NOW() WHEN interval_seconds<>$15 THEN NOW()+make_interval(secs=>$15) ELSE COALESCE(next_run_at,NOW()) END,
candy_next_run_at=CASE WHEN NOT $14 OR NOT $18 OR EXISTS(SELECT 1 FROM intelligence_monitor_runs r WHERE r.plan_id=p.id AND r.test_kind='candy' AND r.status IN ('pending','running')) THEN NULL WHEN NOT enabled OR NOT candy_enabled THEN NOW() WHEN candy_interval_seconds<>$19 THEN NOW()+make_interval(secs=>$19) ELSE COALESCE(candy_next_run_at,NOW()) END,
sort_order=CASE WHEN (source_type='openai_oauth') IS DISTINCT FROM ($2::varchar='openai_oauth') THEN NULL ELSE sort_order END,updated_at=clock_timestamp() WHERE id=$21 AND updated_at=$22 AND deleted_at IS NULL RETURNING updated_at,next_run_at,candy_next_run_at`, args...).Scan(&p.UpdatedAt, &p.NextRunAt, &p.CandyNextRunAt)
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
	if err = lockManualOrderMembership(ctx, tx); err != nil {
		return err
	}
	var keyID *int64
	var borrowed bool
	if err = tx.QueryRowContext(ctx, `SELECT local_api_key_id,local_api_key_borrowed FROM intelligence_monitor_plans WHERE id=$1 AND deleted_at IS NULL FOR UPDATE`, id).Scan(&keyID, &borrowed); err != nil {
		return intelligenceDBError(err)
	}
	var busy bool
	if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM intelligence_monitor_runs WHERE plan_id=$1 AND status IN ('pending','running'))`, id).Scan(&busy); err != nil {
		return err
	}
	if busy {
		return service.ErrIntelligenceBusy
	}
	if keyID != nil && !borrowed {
		if _, err = tx.ExecContext(ctx, `UPDATE api_keys SET status='disabled',updated_at=NOW() WHERE id=$1 AND deleted_at IS NULL`, *keyID); err != nil {
			return err
		}
	}
	if _, err = tx.ExecContext(ctx, `UPDATE intelligence_monitor_plans SET deleted_at=NOW(),enabled=FALSE,next_run_at=NULL,candy_next_run_at=NULL,api_key_encrypted='',updated_at=NOW() WHERE id=$1`, id); err != nil {
		return err
	}
	return tx.Commit()
}

const intelligenceRunColumns = `id,plan_id,plan_name,status,trigger,model,reasoning_effort,prompt,source_type,source_name,source_endpoint,source_snapshot,rate_snapshot,notes_snapshot,api_mode,timeout_seconds,request_key_encrypted,lease_token,started_at,finished_at,duration_ms,http_status,error,created_at,test_kind,correct,answer`

func scanIntelligenceRun(row upstreamScanner, detail bool) (*service.IntelligenceMonitorRun, error) {
	run := new(service.IntelligenceMonitorRun)
	var source, rate, notes []byte
	args := []any{&run.ID, &run.PlanID, &run.PlanName, &run.Status, &run.Trigger, &run.Model, &run.ReasoningEffort, &run.Prompt, &run.SourceType, &run.SourceName, &run.SourceEndpoint, &source, &rate, &notes, &run.APIMode, &run.TimeoutSeconds, &run.RequestKeyEncrypted, &run.LeaseToken, &run.StartedAt, &run.FinishedAt, &run.DurationMs, &run.HTTPStatus, &run.Error, &run.CreatedAt, &run.TestKind, &run.Correct, &run.Answer}
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
	kind, err := intelligenceTestKind(q.TestKind)
	if err != nil {
		return nil, err
	}
	args := []any{kind}
	where := "test_kind=$1"
	if q.PlanID != nil {
		args = append(args, *q.PlanID)
		where += " AND plan_id=$2"
	}
	out := &service.IntelligenceMonitorRunPage{Items: []*service.IntelligenceMonitorRun{}, Page: q.Page, PageSize: q.PageSize}
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM intelligence_monitor_runs WHERE `+where, args...).Scan(&out.Total); err != nil {
		return nil, err
	}
	args = append(args, q.PageSize, (q.Page-1)*q.PageSize)
	rows, err := r.db.QueryContext(ctx, `SELECT `+intelligenceRunSummaryColumns+` FROM intelligence_monitor_runs WHERE `+where+fmt.Sprintf(" ORDER BY created_at DESC,id DESC LIMIT $%d OFFSET $%d", len(args)-1, len(args)), args...)
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
	kind, err := intelligenceTestKind(run.TestKind)
	if err != nil {
		return service.ErrIntelligenceInvalid
	}
	run.TestKind = kind
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var found int64
	var candyEnabled bool
	if err = tx.QueryRowContext(ctx, `SELECT id,candy_enabled FROM intelligence_monitor_plans WHERE id=$1 AND updated_at=$3 AND deleted_at IS NULL AND (NOT $2 OR (enabled AND CASE WHEN $4='candy' THEN candy_enabled AND candy_next_run_at<=NOW() ELSE next_run_at<=NOW() END)) FOR UPDATE`, run.PlanID, scheduled, run.PlanUpdatedAt, kind).Scan(&found, &candyEnabled); err != nil {
		return intelligenceDBError(err)
	}
	if kind == service.IntelligenceMonitorTestCandy && !candyEnabled {
		return service.ErrIntelligenceInvalid
	}
	if scheduled {
		var busy bool
		if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM intelligence_monitor_runs WHERE plan_id=$1 AND test_kind=$2 AND status IN ('pending','running'))`, run.PlanID, kind).Scan(&busy); err != nil {
			return err
		}
		if busy {
			return service.ErrIntelligenceBusy
		}
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
	err = tx.QueryRowContext(ctx, `INSERT INTO intelligence_monitor_runs(plan_id,plan_name,status,trigger,model,reasoning_effort,prompt,source_type,source_name,source_endpoint,source_snapshot,rate_snapshot,notes_snapshot,api_mode,timeout_seconds,request_key_encrypted,test_kind) VALUES($1,$2,'pending',$3,$4,$5,$6,$7,$8,$9,$10::jsonb,$11::jsonb,$12::jsonb,$13,$14,$15,$16) RETURNING id,created_at`, run.PlanID, run.PlanName, run.Trigger, run.Model, run.ReasoningEffort, run.Prompt, run.SourceType, run.SourceName, run.SourceEndpoint, string(source), string(rate), string(notes), run.APIMode, run.TimeoutSeconds, run.RequestKeyEncrypted, kind).Scan(&run.ID, &run.CreatedAt)
	if err != nil {
		return intelligenceDBError(err)
	}
	if _, err = tx.ExecContext(ctx, `UPDATE intelligence_monitor_plans SET next_run_at=CASE WHEN $2='pelican' THEN NULL ELSE next_run_at END,candy_next_run_at=CASE WHEN $2='candy' THEN NULL ELSE candy_next_run_at END WHERE id=$1`, run.PlanID, kind); err != nil {
		return err
	}
	run.Status = "pending"
	return tx.Commit()
}
func (r *intelligenceMonitorRepository) DuePlanIDs(ctx context.Context, limit int) ([]int64, error) {
	return r.duePlanIDs(ctx, limit, service.IntelligenceMonitorTestPelican)
}
func (r *intelligenceMonitorRepository) DueCandyPlanIDs(ctx context.Context, limit int) ([]int64, error) {
	return r.duePlanIDs(ctx, limit, service.IntelligenceMonitorTestCandy)
}
func (r *intelligenceMonitorRepository) duePlanIDs(ctx context.Context, limit int, kind string) ([]int64, error) {
	schedule := "next_run_at"
	where := ""
	if kind == service.IntelligenceMonitorTestCandy {
		schedule, where = "candy_next_run_at", " AND candy_enabled"
	}
	rows, err := r.db.QueryContext(ctx, `SELECT id FROM intelligence_monitor_plans p WHERE enabled AND deleted_at IS NULL`+where+` AND `+schedule+`<=NOW() AND NOT EXISTS(SELECT 1 FROM intelligence_monitor_runs r WHERE r.plan_id=p.id AND r.test_kind=$2 AND r.status IN ('pending','running')) ORDER BY `+schedule+`,id LIMIT $1`, limit, kind)
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
	// Retain the shared two-worker contract for legacy repository decorators.
	return r.claimNext(ctx, token, "", 2)
}

func (r *intelligenceMonitorRepository) ClaimNextForKind(ctx context.Context, token, kind string, limit int) (*service.IntelligenceMonitorRun, error) {
	maxLimit := service.IntelligenceMonitorMaxConcurrency
	if kind == service.IntelligenceMonitorTestCandy {
		maxLimit = service.IntelligenceMonitorCandyMaxConcurrency
	}
	if (kind != service.IntelligenceMonitorTestPelican && kind != service.IntelligenceMonitorTestCandy) || limit < 1 || limit > maxLimit {
		return nil, service.ErrIntelligenceInvalid
	}
	return r.claimNext(ctx, token, kind, limit)
}

func (r *intelligenceMonitorRepository) claimNext(ctx context.Context, token, kind string, limit int) (*service.IntelligenceMonitorRun, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	// Serialize only the short claim transaction. The same lock spans both pools
	// and legacy callers, so concurrent app instances cannot exceed a pool's
	// global capacity or claim the same run simultaneously.
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(245,1)`); err != nil {
		return nil, err
	}
	if kind != "" {
		limit, err = intelligenceConcurrencyForClaim(ctx, tx, kind, limit)
		if err != nil {
			return nil, err
		}
	}
	var active int
	activeQuery := `SELECT COUNT(*) FROM intelligence_monitor_runs WHERE status='running' AND lease_until>NOW()`
	var activeArgs []any
	pendingFilter := ""
	runningFilter := ""
	claimArgs := []any{token, service.IntelligenceMonitorLeaseGraceSeconds}
	if kind != "" {
		activeQuery += ` AND test_kind=$1`
		activeArgs = append(activeArgs, kind)
		pendingFilter = ` AND pending.test_kind=$3`
		runningFilter = ` AND running.test_kind=pending.test_kind`
		claimArgs = append(claimArgs, kind)
	}
	if err = tx.QueryRowContext(ctx, activeQuery, activeArgs...).Scan(&active); err != nil {
		return nil, err
	}
	if active >= limit {
		return nil, nil
	}
	// The execution budget includes source preparation and persistence. Base the
	// lease on this run's immutable timeout, not the plan's current configuration.
	// The two tests for one plan use independent pools. A long drawing must not
	// hold a ready candy test (or vice versa) while that pool has free capacity.
	// Same-kind duplicates are also excluded by the active plan/kind index.
	// Legacy ClaimNext retains its original shared, serial queue contract.
	run, err := scanIntelligenceRun(tx.QueryRowContext(ctx, `UPDATE intelligence_monitor_runs SET status='running',started_at=NOW(),lease_token=$1,lease_until=NOW()+make_interval(secs=>timeout_seconds+$2) WHERE id=(SELECT pending.id FROM intelligence_monitor_runs pending WHERE pending.status='pending'`+pendingFilter+` AND NOT EXISTS(SELECT 1 FROM intelligence_monitor_runs running WHERE running.plan_id=pending.plan_id`+runningFilter+` AND running.status='running') ORDER BY pending.created_at,pending.id FOR UPDATE OF pending SKIP LOCKED LIMIT 1) RETURNING `+intelligenceRunColumns, claimArgs...), false)
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
	var kind string
	// Keep the same plan -> run lock order as Enqueue to avoid a completion
	// waiting on a plan whose enqueuer is waiting on the active-run unique index.
	if err = tx.QueryRowContext(ctx, `SELECT id FROM intelligence_monitor_plans WHERE id=$1 FOR UPDATE`, run.PlanID).Scan(&planID); err != nil {
		return intelligenceDBError(err)
	}
	err = tx.QueryRowContext(ctx, `UPDATE intelligence_monitor_runs SET status=$3,finished_at=NOW(),duration_ms=EXTRACT(EPOCH FROM (NOW()-started_at))*1000,http_status=$4,error=$5,html=$6,raw_text=$7,rate_snapshot=$8::jsonb,source_snapshot=$9::jsonb,correct=$10,answer=$11,candy_grade_version=CASE WHEN test_kind='candy' AND $3::varchar='succeeded' THEN $12 ELSE 0 END,request_key_encrypted='',lease_token='',lease_until=NULL WHERE id=$1 AND lease_token=$2 AND status='running' RETURNING plan_id,test_kind`, run.ID, run.LeaseToken, run.Status, run.HTTPStatus, run.Error, run.HTML, run.RawText, string(rate), string(source), run.Correct, run.Answer, service.IntelligenceMonitorCandyGradeVersion).Scan(&planID, &kind)
	if err != nil {
		return intelligenceDBError(err)
	}
	if err = scheduleCompletedIntelligencePlans(ctx, tx, []int64{planID}, kind); err != nil {
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
		rows, err = tx.QueryContext(ctx, `UPDATE intelligence_monitor_runs SET status='failed',finished_at=NOW(),duration_ms=EXTRACT(EPOCH FROM (NOW()-started_at))*1000,error='Execution interrupted or lease expired; this request was not automatically retried to avoid duplicate charges',correct=NULL,answer='',request_key_encrypted='',lease_token='',lease_until=NULL WHERE plan_id=ANY($1) AND status='running' AND lease_until<NOW() RETURNING plan_id,test_kind`, pq.Array(ids))
		if err != nil {
			return err
		}
		expired := make(map[int64]struct{}, len(ids))
		expiredByKind := make(map[string][]int64, 2)
		for rows.Next() {
			var id int64
			var kind string
			if err = rows.Scan(&id, &kind); err != nil {
				_ = rows.Close()
				return err
			}
			expired[id] = struct{}{}
			expiredByKind[kind] = append(expiredByKind[kind], id)
		}
		err = rows.Err()
		_ = rows.Close()
		if err != nil {
			return err
		}
		// A normal completion may have won while we waited for the plan lock.
		// Only actually expired plans may change their countdown a second time.
		expiredIDs := make([]int64, 0, len(expired))
		for _, id := range ids {
			if _, found := expired[id]; found {
				expiredIDs = append(expiredIDs, id)
			}
		}
		// A separate statement sees the newly expired rows. A data-modifying
		// CTE would still expose their old active state to an ordinary subquery.
		for _, kind := range []string{service.IntelligenceMonitorTestPelican, service.IntelligenceMonitorTestCandy} {
			if err = scheduleCompletedIntelligencePlans(ctx, tx, expiredByKind[kind], kind); err != nil {
				return err
			}
		}
		for _, id := range expiredIDs {
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
	_, err := tx.ExecContext(ctx, `DELETE FROM intelligence_monitor_runs WHERE id IN (
 (SELECT id FROM intelligence_monitor_runs WHERE plan_id=$1 AND test_kind='pelican' AND status IN ('succeeded','failed') ORDER BY created_at DESC,id DESC OFFSET $2)
 UNION ALL
 (SELECT id FROM intelligence_monitor_runs WHERE plan_id=$1 AND test_kind='candy' AND status IN ('succeeded','failed') ORDER BY created_at DESC,id DESC OFFSET $3)
)`, planID, service.IntelligenceMonitorRetainedRuns, service.IntelligenceMonitorCandyRetainedRuns)
	return err
}

func (r *intelligenceMonitorRepository) PruneRuns(ctx context.Context) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	// Include archived plans so retention applies to all stored artifacts.
	rows, err := tx.QueryContext(ctx, `SELECT id FROM intelligence_monitor_plans p WHERE
 EXISTS(SELECT 1 FROM intelligence_monitor_runs r WHERE r.plan_id=p.id AND r.test_kind='pelican' AND r.status IN ('succeeded','failed') OFFSET $1)
 OR EXISTS(SELECT 1 FROM intelligence_monitor_runs r WHERE r.plan_id=p.id AND r.test_kind='candy' AND r.status IN ('succeeded','failed') OFFSET $2)
 ORDER BY id FOR UPDATE`, service.IntelligenceMonitorRetainedRuns, service.IntelligenceMonitorCandyRetainedRuns)
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
