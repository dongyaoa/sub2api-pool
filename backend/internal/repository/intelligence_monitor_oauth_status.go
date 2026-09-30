package repository

import (
	"context"
	"encoding/json"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
)

var _ service.IntelligenceOAuthMonitorRepository = (*intelligenceMonitorRepository)(nil)

func (r *intelligenceMonitorRepository) LoadOAuthMonitorAccounts(ctx context.Context, ids []int64) (map[int64]*service.Account, error) {
	out := map[int64]*service.Account{}
	if len(ids) == 0 {
		return out, nil
	}
	// Identity fields are sufficient to reject a usage snapshot from a previous
	// OAuth identity. Never load access/refresh tokens for gallery polling.
	// Group names and membership come from this same current snapshot; do not
	// reuse the historical run's group or omit an inactive but still-bound group.
	rows, err := r.db.QueryContext(ctx, `SELECT a.id,a.name,a.platform,a.type,a.status,a.schedulable,a.expires_at,a.auto_pause_on_expired,a.parent_account_id,a.rate_limit_reset_at,a.temp_unschedulable_until,COALESCE(a.temp_unschedulable_reason,''),a.extra,
jsonb_build_object('email',a.credentials->'email','chatgpt_account_id',a.credentials->'chatgpt_account_id','workspace_id',a.credentials->'workspace_id','chatgpt_workspace_id',a.credentials->'chatgpt_workspace_id','organization_id',a.credentials->'organization_id','org_id',a.credentials->'org_id'),
COALESCE(membership.groups,'[]'::jsonb)
FROM accounts a
LEFT JOIN (
 SELECT ag.account_id,jsonb_agg(jsonb_build_object('id',g.id,'name',g.name) ORDER BY g.id) AS groups
 FROM account_groups ag JOIN groups g ON g.id=ag.group_id AND g.deleted_at IS NULL
 WHERE ag.account_id=ANY($1) GROUP BY ag.account_id
) membership ON membership.account_id=a.id
WHERE a.id=ANY($1) AND a.deleted_at IS NULL`, pq.Array(ids))
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		account := new(service.Account)
		var extra, identity, groups []byte
		if err := rows.Scan(&account.ID, &account.Name, &account.Platform, &account.Type, &account.Status, &account.Schedulable, &account.ExpiresAt, &account.AutoPauseOnExpired, &account.ParentAccountID, &account.RateLimitResetAt, &account.TempUnschedulableUntil, &account.TempUnschedulableReason, &extra, &identity, &groups); err != nil {
			return nil, err
		}
		if len(extra) > 0 {
			if err := json.Unmarshal(extra, &account.Extra); err != nil {
				return nil, err
			}
		}
		if err := json.Unmarshal(identity, &account.Credentials); err != nil {
			return nil, err
		}
		if err := json.Unmarshal(groups, &account.Groups); err != nil {
			return nil, err
		}
		out[account.ID] = account
	}
	return out, rows.Err()
}

func (r *intelligenceMonitorRepository) DeferOAuthMonitor(ctx context.Context, planID, accountID int64, until time.Time) error {
	// Preserve the user's switch and interval. A short recheck also detects an
	// administrator clearing the quota before the provider's original reset.
	_, err := r.db.ExecContext(ctx, `UPDATE intelligence_monitor_plans SET
next_run_at=CASE WHEN enabled AND next_run_at IS NOT NULL THEN GREATEST(next_run_at,$3) ELSE next_run_at END,
candy_next_run_at=CASE WHEN enabled AND candy_enabled AND candy_next_run_at IS NOT NULL THEN GREATEST(candy_next_run_at,$3) ELSE candy_next_run_at END
WHERE id=$1 AND (account_id=$2 OR ($2=0 AND account_id IS NULL)) AND source_type='openai_oauth' AND deleted_at IS NULL`, planID, accountID, until)
	return err
}
