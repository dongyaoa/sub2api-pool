package repository

import (
	"context"
	"database/sql"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
)

var _ service.IntelligenceLocalPlanRepository = (*intelligenceMonitorRepository)(nil)

// Mask in SQL so ordinary plan polling never reads plaintext local credentials.
func (r *intelligenceMonitorRepository) LoadLocalPlanMetadata(ctx context.Context, ids []int64) (map[int64]service.IntelligenceLocalPlanMetadata, error) {
	out := make(map[int64]service.IntelligenceLocalPlanMetadata)
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := r.db.QueryContext(ctx, `SELECT p.id,COALESCE(k.name,''),
CASE WHEN k.id IS NULL THEN '' WHEN length(k.key)>8 THEN left(k.key,4)||'••••'||right(k.key,4) ELSE '***' END,
COALESCE(g.name,''),g.rate_multiplier,COALESCE(g.status,'')
FROM intelligence_monitor_plans p
LEFT JOIN api_keys k ON k.id=p.local_api_key_id AND k.user_id=p.local_key_owner_id AND k.group_id=p.group_id AND k.deleted_at IS NULL
LEFT JOIN groups g ON g.id=p.group_id AND g.deleted_at IS NULL
WHERE p.id=ANY($1) AND p.source_type='local_group' AND p.deleted_at IS NULL`, pq.Array(ids))
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var id int64
		var value service.IntelligenceLocalPlanMetadata
		var rate sql.NullFloat64
		if err = rows.Scan(&id, &value.KeyName, &value.KeyMasked, &value.GroupName, &rate, &value.GroupStatus); err != nil {
			return nil, err
		}
		if rate.Valid {
			value.GroupRateMultiplier = &rate.Float64
		}
		out[id] = value
	}
	return out, rows.Err()
}

func (r *intelligenceMonitorRepository) IsManagedIntelligenceKey(ctx context.Context, keyID, excludingPlan int64) (bool, error) {
	var managed bool
	err := r.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM intelligence_monitor_plans WHERE local_api_key_id=$1 AND NOT local_api_key_borrowed AND id<>$2)`, keyID, excludingPlan).Scan(&managed)
	return managed, err
}
