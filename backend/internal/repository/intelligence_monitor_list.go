package repository

import (
	"context"
	"database/sql"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
)

var _ service.IntelligenceMonitorListRepository = (*intelligenceMonitorRepository)(nil)

// Gallery polling does not read artwork, raw model output, or execution secrets.
// Preserve the metadata scanner shape with empty credential/lease literals.
const intelligenceRunSummaryColumns = `id,plan_id,plan_name,status,trigger,model,reasoning_effort,prompt,source_type,source_name,source_endpoint,source_snapshot,rate_snapshot,notes_snapshot,api_mode,timeout_seconds,''::text AS request_key_encrypted,''::text AS lease_token,started_at,finished_at,duration_ms,http_status,error,created_at`

const intelligencePlanSourceNamesSQL = `SELECT p.id,CASE p.source_type
WHEN 'openai_oauth' THEN a.name WHEN 'upstream' THEN t.name WHEN 'local_group' THEN g.name END
FROM intelligence_monitor_plans p
LEFT JOIN accounts a ON p.source_type='openai_oauth' AND a.id=p.account_id AND a.deleted_at IS NULL
LEFT JOIN upstream_targets t ON p.source_type='upstream' AND t.id=p.upstream_target_id AND t.deleted_at IS NULL
LEFT JOIN groups g ON p.source_type='local_group' AND g.id=p.group_id AND g.deleted_at IS NULL
WHERE p.id=ANY($1) AND p.deleted_at IS NULL`

// Separate index-backed active/terminal branches guarantee live status is never
// displaced by old terminal rows or a skewed creation timestamp. At most 21 rows
// per plan cross the database boundary, without COUNT or a history-wide window.
const intelligencePlanRecentRunsSQL = `SELECT recent.*
FROM unnest($1::bigint[]) AS requested(plan_id)
CROSS JOIN LATERAL (
 (SELECT ` + intelligenceRunSummaryColumns + ` FROM intelligence_monitor_runs
  WHERE plan_id=requested.plan_id AND status IN ('pending','running')
  ORDER BY created_at DESC,id DESC LIMIT 1)
 UNION ALL
 (SELECT ` + intelligenceRunSummaryColumns + ` FROM intelligence_monitor_runs
  WHERE plan_id=requested.plan_id AND status IN ('succeeded','failed')
  ORDER BY created_at DESC,id DESC LIMIT $2)
) recent
ORDER BY recent.plan_id,(recent.status IN ('pending','running')) DESC,recent.created_at DESC,recent.id DESC`

func (r *intelligenceMonitorRepository) LoadPlanListData(ctx context.Context, planIDs []int64) (*service.IntelligenceMonitorPlanListData, error) {
	out := &service.IntelligenceMonitorPlanListData{Runs: map[int64][]*service.IntelligenceMonitorRun{}, SourceNames: map[int64]string{}}
	if len(planIDs) == 0 {
		return out, nil
	}
	ids := make([]int64, 0, len(planIDs))
	seen := make(map[int64]struct{}, len(planIDs))
	for _, id := range planIDs {
		if _, found := seen[id]; id <= 0 || found {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := r.db.QueryContext(ctx, intelligencePlanSourceNamesSQL, pq.Array(ids))
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id int64
		var name sql.NullString
		if err = rows.Scan(&id, &name); err != nil {
			_ = rows.Close()
			return nil, err
		}
		if name.Valid {
			out.SourceNames[id] = name.String
		}
	}
	err = rows.Err()
	_ = rows.Close()
	if err != nil {
		return nil, err
	}
	rows, err = r.db.QueryContext(ctx, intelligencePlanRecentRunsSQL, pq.Array(ids), service.IntelligenceMonitorRetainedRuns)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		run, err := scanIntelligenceRun(rows, false)
		if err != nil {
			return nil, err
		}
		out.Runs[run.PlanID] = append(out.Runs[run.PlanID], run)
	}
	return out, rows.Err()
}
