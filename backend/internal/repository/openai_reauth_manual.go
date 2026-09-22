package repository

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

// CompleteManual is the recovery exit for CAPTCHA/device challenges. The
// service supplies only tokens obtained by exchanging a live OAuth session.
func (r *openAIReauthRepository) CompleteManual(ctx context.Context, account *service.Account, generation int64, patch map[string]any) (bool, error) {
	if account == nil || account.ProxyID == nil || account.Proxy == nil || generation <= 0 {
		return false, errors.New("invalid manual reauthorization snapshot")
	}
	patchJSON, err := reauthTokenPatch(patch)
	if err != nil {
		return false, err
	}
	credentialsJSON, err := json.Marshal(account.Credentials)
	if err != nil {
		return false, err
	}
	poolJSON, err := json.Marshal(account.Extra[service.AccountProxyPoolExtraKey])
	if err != nil {
		return false, err
	}
	p := account.Proxy
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()
	// Account first, configuration second, matching Save/Enqueue/Complete lock
	// order. Preserve status, schedulable, all limits and ordinary settings.
	result, err := tx.ExecContext(ctx, `UPDATE accounts a SET
		credentials = COALESCE(a.credentials, '{}'::jsonb) || $3::jsonb,
		extra = COALESCE(a.extra, '{}'::jsonb) - 'openai_auto_reauth_pending', updated_at = NOW()
		FROM openai_auto_reauth r, proxies p
		WHERE a.id = $1 AND r.account_id = a.id AND r.generation = $2
		AND a.platform = 'openai' AND a.type = 'oauth' AND a.parent_account_id IS NULL
		AND a.deleted_at IS NULL AND a.proxy_fallback_origin_id IS NULL
		AND a.extra->'openai_auto_reauth_pending' = 'true'::jsonb
		AND a.credentials = $4::jsonb AND a.proxy_id = $5 AND p.id = a.proxy_id
		AND COALESCE(a.extra->'proxy_pool', 'null'::jsonb) = $6::jsonb
		AND p.protocol = $7 AND p.host = $8 AND p.port = $9
		AND COALESCE(p.username, '') = $10 AND COALESCE(p.password, '') = $11
		AND p.status = 'active' AND p.deleted_at IS NULL
		AND (p.expires_at IS NULL OR p.expires_at > NOW())`,
		account.ID, generation, string(patchJSON), string(credentialsJSON), *account.ProxyID,
		string(poolJSON), p.Protocol, p.Host, p.Port, p.Username, p.Password)
	if err != nil {
		return false, err
	}
	count, err := result.RowsAffected()
	if err != nil || count == 0 {
		return false, err
	}
	result, err = tx.ExecContext(ctx, `UPDATE openai_auto_reauth SET
		status = 'succeeded', stage = 'succeeded', generation = generation + 1,
		lease_id = NULL, lease_until = NULL, next_attempt_at = NULL,
		last_error_code = '', last_success_at = NOW(), trigger_reason = 'manual_oauth', updated_at = NOW()
		WHERE account_id = $1 AND generation = $2`, account.ID, generation)
	if err != nil {
		return false, err
	}
	count, err = result.RowsAffected()
	if err != nil || count == 0 {
		return false, err
	}
	if err = reauthOutbox(ctx, tx, account.ID); err != nil {
		return false, err
	}
	if err = tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}
