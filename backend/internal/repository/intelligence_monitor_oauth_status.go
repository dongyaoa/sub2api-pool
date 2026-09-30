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
	rows, err := r.db.QueryContext(ctx, `SELECT id,name,platform,type,status,schedulable,expires_at,auto_pause_on_expired,parent_account_id,rate_limit_reset_at,temp_unschedulable_until,COALESCE(temp_unschedulable_reason,''),extra,
jsonb_build_object('email',credentials->'email','chatgpt_account_id',credentials->'chatgpt_account_id','workspace_id',credentials->'workspace_id','chatgpt_workspace_id',credentials->'chatgpt_workspace_id','organization_id',credentials->'organization_id','org_id',credentials->'org_id')
FROM accounts WHERE id=ANY($1) AND deleted_at IS NULL`, pq.Array(ids))
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		account := new(service.Account)
		var extra, identity []byte
		if err := rows.Scan(&account.ID, &account.Name, &account.Platform, &account.Type, &account.Status, &account.Schedulable, &account.ExpiresAt, &account.AutoPauseOnExpired, &account.ParentAccountID, &account.RateLimitResetAt, &account.TempUnschedulableUntil, &account.TempUnschedulableReason, &extra, &identity); err != nil {
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
