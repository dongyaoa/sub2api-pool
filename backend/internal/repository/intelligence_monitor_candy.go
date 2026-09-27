package repository

import (
	"context"
	"database/sql"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
)

func intelligenceTestKind(kind string) (string, error) {
	if kind == "" {
		return service.IntelligenceMonitorTestPelican, nil
	}
	if kind != service.IntelligenceMonitorTestPelican && kind != service.IntelligenceMonitorTestCandy {
		return "", service.ErrIntelligenceInvalid
	}
	return kind, nil
}

// Plan rows are already locked by CompleteRun/ExpireRuns. Each kind starts its
// own next interval at completion without changing the other kind's countdown.
func scheduleCompletedIntelligencePlans(ctx context.Context, tx *sql.Tx, ids []int64, kind string) error {
	if len(ids) == 0 {
		return nil
	}
	last, next, interval, enabled := "last_run_at", "next_run_at", "interval_seconds", "enabled"
	if kind == service.IntelligenceMonitorTestCandy {
		last, next, interval, enabled = "candy_last_run_at", "candy_next_run_at", "candy_interval_seconds", "enabled AND candy_enabled"
	}
	_, err := tx.ExecContext(ctx, `UPDATE intelligence_monitor_plans p SET `+last+`=NOW(),`+next+`=CASE WHEN `+enabled+` AND deleted_at IS NULL AND NOT EXISTS(
 SELECT 1 FROM intelligence_monitor_runs r WHERE r.plan_id=p.id AND r.test_kind=$2 AND r.status IN ('pending','running')
) THEN NOW()+make_interval(secs=>`+interval+`) ELSE NULL END WHERE id=ANY($1)`, pq.Array(ids), kind)
	return err
}
