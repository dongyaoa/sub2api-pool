package repository

import (
	"context"
	"database/sql"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
)

var _ service.IntelligenceMonitorListRepository = (*intelligenceMonitorRepository)(nil)

// Gallery polling does not read artwork, raw model output, or execution secrets.
// Candy bars also omit the repeated question and detailed snapshots, available
// through GetRun. Preserve the scanner shape and existing artwork metadata.
const intelligenceRunSummaryColumns = `id,plan_id,plan_name,status,trigger,model,reasoning_effort,CASE WHEN test_kind='candy' THEN ''::text ELSE prompt END AS prompt,source_type,source_name,source_endpoint,CASE WHEN test_kind='candy' THEN '{}'::jsonb ELSE source_snapshot END AS source_snapshot,rate_snapshot,CASE WHEN test_kind='candy' THEN '{}'::jsonb ELSE notes_snapshot END AS notes_snapshot,api_mode,timeout_seconds,''::text AS request_key_encrypted,''::text AS lease_token,started_at,finished_at,duration_ms,http_status,error,created_at,test_kind,correct,answer,fingerprint`

const intelligencePlanSourceNamesSQL = `SELECT p.id,CASE p.source_type
WHEN 'openai_oauth' THEN a.name WHEN 'upstream' THEN t.name WHEN 'local_group' THEN g.name END,p.candy_enabled
FROM intelligence_monitor_plans p
LEFT JOIN accounts a ON p.source_type='openai_oauth' AND a.id=p.account_id AND a.deleted_at IS NULL
LEFT JOIN upstream_targets t ON p.source_type='upstream' AND t.id=p.upstream_target_id AND t.deleted_at IS NULL
LEFT JOIN groups g ON p.source_type='local_group' AND g.id=p.group_id AND g.deleted_at IS NULL
WHERE p.id=ANY($1) AND p.deleted_at IS NULL`

// Separate index-backed active/terminal branches guarantee live status is never
// displaced by old terminal rows or a skewed creation timestamp. The kind's
// bounded terminal history and one active run cross the database boundary,
// without COUNT or a history-wide window.
const intelligencePlanRecentRunsSQL = `SELECT recent.*
FROM unnest($1::bigint[]) AS requested(plan_id)
CROSS JOIN LATERAL (
 (SELECT ` + intelligenceRunSummaryColumns + ` FROM intelligence_monitor_runs
  WHERE plan_id=requested.plan_id AND test_kind=$3 AND status IN ('pending','running')
  ORDER BY created_at DESC,id DESC LIMIT 1)
 UNION ALL
 (SELECT ` + intelligenceRunSummaryColumns + ` FROM intelligence_monitor_runs
  WHERE plan_id=requested.plan_id AND test_kind=$3 AND status IN ('succeeded','failed')
  ORDER BY created_at DESC,id DESC LIMIT $2)
) recent
ORDER BY recent.plan_id,(recent.status IN ('pending','running')) DESC,recent.created_at DESC,recent.id DESC`

func (r *intelligenceMonitorRepository) LoadPlanListData(ctx context.Context, planIDs []int64) (*service.IntelligenceMonitorPlanListData, error) {
	out := &service.IntelligenceMonitorPlanListData{Runs: map[int64][]*service.IntelligenceMonitorRun{}, CandyRuns: map[int64][]*service.IntelligenceMonitorRun{}, SourceNames: map[int64]string{}}
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
	candyIDs := make([]int64, 0, len(ids))
	for rows.Next() {
		var id int64
		var name sql.NullString
		var candyEnabled bool
		if err = rows.Scan(&id, &name, &candyEnabled); err != nil {
			_ = rows.Close()
			return nil, err
		}
		if name.Valid {
			out.SourceNames[id] = name.String
		}
		if candyEnabled {
			candyIDs = append(candyIDs, id)
		}
	}
	err = rows.Err()
	_ = rows.Close()
	if err != nil {
		return nil, err
	}
	if err = r.loadPlanKindRuns(ctx, ids, service.IntelligenceMonitorTestPelican, service.IntelligenceMonitorRetainedRuns, out.Runs); err != nil {
		return nil, err
	}
	if len(candyIDs) > 0 {
		if err = r.loadPlanKindRuns(ctx, candyIDs, service.IntelligenceMonitorTestCandy, service.IntelligenceMonitorCandyRetainedRuns, out.CandyRuns); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (r *intelligenceMonitorRepository) loadPlanKindRuns(ctx context.Context, ids []int64, kind string, limit int, out map[int64][]*service.IntelligenceMonitorRun) error {
	rows, err := r.db.QueryContext(ctx, intelligencePlanRecentRunsSQL, pq.Array(ids), limit, kind)
	if err != nil {
		return err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		run, err := scanIntelligenceRun(rows, false)
		if err != nil {
			return err
		}
		out[run.PlanID] = append(out[run.PlanID], run)
	}
	return rows.Err()
}
