package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
)

var _ service.IntelligencePublicDisplayRepository = (*intelligenceMonitorRepository)(nil)

type intelligencePublicSettingsReader interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

func readIntelligencePublicDisplay(ctx context.Context, db intelligencePublicSettingsReader) (*service.IntelligencePublicDisplay, error) {
	cfg := service.DefaultIntelligencePublicDisplay()
	var raw string
	err := db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key=$1`, service.IntelligencePublicDisplaySettingKey).Scan(&raw)
	if errors.Is(err, sql.ErrNoRows) {
		return &cfg, nil
	}
	if err != nil {
		return nil, err
	}
	if err = json.Unmarshal([]byte(raw), &cfg); err != nil {
		return nil, service.ErrPublicPelicanUnavailable
	}
	if err = cfg.Normalize(); err != nil {
		return nil, service.ErrPublicPelicanUnavailable
	}
	return &cfg, nil
}

func (r *intelligenceMonitorRepository) GetPublicDisplay(ctx context.Context) (*service.IntelligencePublicDisplay, error) {
	return readIntelligencePublicDisplay(ctx, r.db)
}

func (r *intelligenceMonitorRepository) SavePublicDisplay(ctx context.Context, cfg service.IntelligencePublicDisplay) error {
	if err := cfg.Normalize(); err != nil {
		return err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if len(cfg.PlanIDs) > 0 {
		rows, e := tx.QueryContext(ctx, `SELECT p.id FROM intelligence_monitor_plans p JOIN groups g ON g.id=p.group_id WHERE p.id=ANY($1) AND p.source_type='local_group' AND p.deleted_at IS NULL AND g.deleted_at IS NULL AND g.status='active' FOR SHARE OF p,g`, pq.Array(cfg.PlanIDs))
		if e != nil {
			return e
		}
		count := 0
		for rows.Next() {
			var id int64
			if e = rows.Scan(&id); e != nil {
				_ = rows.Close()
				return e
			}
			count++
		}
		e = rows.Err()
		_ = rows.Close()
		if e != nil {
			return e
		}
		if count != len(cfg.PlanIDs) {
			return service.ErrIntelligenceInvalid
		}
	}
	raw, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO settings(key,value,updated_at) VALUES($1,$2,NOW()) ON CONFLICT(key) DO UPDATE SET value=EXCLUDED.value,updated_at=EXCLUDED.updated_at`, service.IntelligencePublicDisplaySettingKey, string(raw))
	if err != nil {
		return err
	}
	return tx.Commit()
}

// Current plan/group visibility and the historical group snapshot must both
// match. Rebinding an admin plan must not publish its former group's artwork.
const publicPelicanPlanFilter = `p.source_type='local_group' AND p.deleted_at IS NULL AND g.deleted_at IS NULL AND g.status='active' AND p.id=ANY($1) AND g.id=ANY($2)`
const publicPelicanRunFilter = `r.test_kind='pelican' AND r.source_type='local_group' AND r.source_snapshot->>'group_id'=p.group_id::text AND r.model=p.model`
const publicPelicanRunColumns = `r.id,r.plan_id,r.status,r.model,r.reasoning_effort,r.created_at,r.started_at,r.finished_at,r.duration_ms,CASE WHEN r.status='failed' THEN '检测未完成，请稍后查看下一次结果。' ELSE '' END AS error`

func scanPublicPelicanRun(row upstreamScanner, detail bool) (*service.PublicPelicanRunDetail, error) {
	out := &service.PublicPelicanRunDetail{}
	args := []any{&out.ID, &out.PlanID, &out.Status, &out.Model, &out.ReasoningEffort, &out.CreatedAt, &out.StartedAt, &out.FinishedAt, &out.DurationMS, &out.Error}
	if detail {
		args = append(args, &out.HTML)
	}
	if err := row.Scan(args...); err != nil {
		return nil, intelligenceDBError(err)
	}
	return out, nil
}

func (r *intelligenceMonitorRepository) ListPublicPelican(ctx context.Context, allowedGroupIDs []int64) (*service.PublicPelicanPage, error) {
	// Configuration, current group identity and historical rows share a snapshot.
	// Re-read settings here so an earlier authorization step cannot reuse stale
	// visibility when the admin has just disabled or changed the public selection.
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	cfg, err := readIntelligencePublicDisplay(ctx, tx)
	if err != nil {
		return nil, err
	}
	page := &service.PublicPelicanPage{Config: cfg.PublicPelicanConfig, ServerTime: time.Now().UTC(), Items: []*service.PublicPelicanPlan{}}
	if !cfg.Enabled || len(cfg.PlanIDs) == 0 || len(allowedGroupIDs) == 0 {
		return page, tx.Commit()
	}
	rows, err := tx.QueryContext(ctx, `SELECT p.id,g.name,g.rate_multiplier,p.enabled,p.interval_seconds,p.next_run_at,p.last_run_at,p.model FROM intelligence_monitor_plans p JOIN groups g ON g.id=p.group_id WHERE `+publicPelicanPlanFilter+` ORDER BY p.sort_order ASC NULLS LAST,p.created_at DESC,p.id DESC LIMIT $3`, pq.Array(cfg.PlanIDs), pq.Array(allowedGroupIDs), service.IntelligencePublicDisplayMaxPlans)
	if err != nil {
		return nil, err
	}
	byID := make(map[int64]*service.PublicPelicanPlan)
	ids := make([]int64, 0)
	for rows.Next() {
		plan := &service.PublicPelicanPlan{ReasoningEffort: service.IntelligenceMonitorReasoning, RecentRuns: []*service.PublicPelicanRun{}}
		if err = rows.Scan(&plan.ID, &plan.GroupName, &plan.GroupRateMultiplier, &plan.Enabled, &plan.IntervalSeconds, &plan.NextRunAt, &plan.LastRunAt, &plan.Model); err != nil {
			_ = rows.Close()
			return nil, err
		}
		page.Items = append(page.Items, plan)
		byID[plan.ID] = plan
		ids = append(ids, plan.ID)
	}
	err = rows.Err()
	_ = rows.Close()
	if err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return page, tx.Commit()
	}
	rows, err = tx.QueryContext(ctx, `SELECT recent.* FROM intelligence_monitor_plans p CROSS JOIN LATERAL (
 (SELECT `+publicPelicanRunColumns+` FROM intelligence_monitor_runs r WHERE r.plan_id=p.id AND `+publicPelicanRunFilter+` AND r.status IN ('pending','running') ORDER BY r.created_at DESC,r.id DESC LIMIT 1)
 UNION ALL
 (SELECT `+publicPelicanRunColumns+` FROM intelligence_monitor_runs r WHERE r.plan_id=p.id AND `+publicPelicanRunFilter+` AND r.status IN ('succeeded','failed') AND (NOT $3 OR r.status<>'failed') ORDER BY r.created_at DESC,r.id DESC LIMIT $2)
) recent WHERE p.id=ANY($1) ORDER BY recent.plan_id,(recent.status IN ('pending','running')) DESC,recent.created_at DESC,recent.id DESC`, pq.Array(ids), service.IntelligenceMonitorRetainedRuns, cfg.HideFailed)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		detail, e := scanPublicPelicanRun(rows, false)
		if e != nil {
			_ = rows.Close()
			return nil, e
		}
		run := &detail.PublicPelicanRun
		plan := byID[run.PlanID]
		if plan.LatestRun == nil {
			plan.LatestRun = run
		}
		if run.Status == "succeeded" || run.Status == "failed" {
			plan.RecentRuns = append(plan.RecentRuns, run)
		}
	}
	err = rows.Err()
	_ = rows.Close()
	if err != nil {
		return nil, err
	}
	return page, tx.Commit()
}

func (r *intelligenceMonitorRepository) GetPublicPelicanRun(ctx context.Context, id int64, allowedGroupIDs []int64) (*service.PublicPelicanRunDetail, error) {
	tx, err := r.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	cfg, err := readIntelligencePublicDisplay(ctx, tx)
	if err != nil {
		return nil, err
	}
	if !cfg.Enabled || len(cfg.PlanIDs) == 0 || len(allowedGroupIDs) == 0 || id <= 0 {
		return nil, service.ErrIntelligenceNotFound
	}
	run, err := scanPublicPelicanRun(tx.QueryRowContext(ctx, `SELECT `+publicPelicanRunColumns+`,CASE WHEN r.status='succeeded' THEN r.html ELSE '' END FROM intelligence_monitor_runs r JOIN intelligence_monitor_plans p ON p.id=r.plan_id JOIN groups g ON g.id=p.group_id WHERE `+publicPelicanPlanFilter+` AND `+publicPelicanRunFilter+` AND r.id=$3 AND (NOT $4 OR r.status<>'failed')`, pq.Array(cfg.PlanIDs), pq.Array(allowedGroupIDs), id, cfg.HideFailed), true)
	if err != nil {
		return nil, err
	}
	return run, tx.Commit()
}
