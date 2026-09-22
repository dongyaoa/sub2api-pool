package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

func (r *openAIReauthRepository) SaveBinding(ctx context.Context, account *service.Account, cipher, email string, enabled bool) error {
	if account == nil || account.IsShadow() || account.IsOpenAIPersonalAccessToken() {
		return service.ErrOpenAIReauthInvalidAccount
	}
	credentials, err := json.Marshal(account.Credentials)
	if err != nil {
		return err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var rawExtra []byte
	var existingCipher, proxyStatus string
	var proxyID, fallbackID sql.NullInt64
	var deleted, expires sql.NullTime
	err = tx.QueryRowContext(ctx, `SELECT COALESCE(a.extra, '{}'::jsonb), COALESCE(r.encrypted_secret, ''),
		a.proxy_id, a.proxy_fallback_origin_id, COALESCE(p.status, ''), p.deleted_at, p.expires_at
		FROM accounts a LEFT JOIN proxies p ON p.id = a.proxy_id
		LEFT JOIN openai_auto_reauth r ON r.account_id = a.id
		WHERE a.id = $1 AND a.deleted_at IS NULL AND a.platform = 'openai' AND a.type = 'oauth'
		AND a.parent_account_id IS NULL AND COALESCE(a.credentials, '{}'::jsonb) = $2::jsonb
		AND a.proxy_id IS NOT DISTINCT FROM $3 FOR UPDATE OF a`, account.ID, string(credentials), account.ProxyID).
		Scan(&rawExtra, &existingCipher, &proxyID, &fallbackID, &proxyStatus, &deleted, &expires)
	if errors.Is(err, sql.ErrNoRows) {
		return service.ErrOpenAIReauthInvalidAccount
	}
	if err != nil {
		return err
	}
	var extra map[string]any
	if json.Unmarshal(rawExtra, &extra) != nil {
		return service.ErrOpenAIReauthInvalidAccount
	}
	if cipher == "" {
		cipher = existingCipher
	}
	if cipher == "" {
		return service.ErrOpenAIReauthNotConfigured
	}
	if enabled {
		pool, err := service.ParseAccountProxyPool(extra[service.AccountProxyPoolExtraKey])
		if !proxyID.Valid || fallbackID.Valid || proxyStatus != service.StatusActive || deleted.Valid ||
			(expires.Valid && !expires.Time.After(time.Now())) || err != nil || len(pool) > 1 ||
			(len(pool) == 1 && pool[0].ProxyID != proxyID.Int64) {
			return service.ErrOpenAIReauthInvalidAccount
		}
	}
	// Editing secrets cancels obsolete leases. Existing invalid tokens remain
	// quarantined until the separate Run action or verified manual OAuth succeeds.
	pending := extra[service.OpenAIReauthPendingKey] == true
	_, err = tx.ExecContext(ctx, `INSERT INTO openai_auto_reauth
		(account_id, encrypted_secret, enabled, status, stage, last_error_code, login_email)
		VALUES ($1, $2, $3, CASE WHEN $4 THEN 'blocked' ELSE 'idle' END,
		CASE WHEN $4 THEN 'blocked' ELSE 'idle' END, CASE WHEN $4 THEN 'credentials_updated' ELSE '' END, $5)
		ON CONFLICT (account_id) DO UPDATE SET encrypted_secret = EXCLUDED.encrypted_secret,
		enabled = EXCLUDED.enabled, generation = openai_auto_reauth.generation + 1,
		login_email = CASE WHEN EXCLUDED.login_email <> '' THEN EXCLUDED.login_email ELSE openai_auto_reauth.login_email END,
		status = EXCLUDED.status, stage = EXCLUDED.stage, attempts = 0, next_attempt_at = NULL,
		lease_id = NULL, lease_until = NULL, last_error_code = EXCLUDED.last_error_code,
		trigger_reason = '', expected_credentials_hash = '', expected_proxy_hash = '', updated_at = NOW()`,
		account.ID, cipher, enabled, pending, email)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE accounts SET extra = COALESCE(extra, '{}'::jsonb) ||
		jsonb_build_object('openai_auto_reauth_enabled', $2::boolean), updated_at = NOW() WHERE id = $1`, account.ID, enabled)
	if err != nil {
		return err
	}
	if err = reauthOutbox(ctx, tx, account.ID); err != nil {
		return err
	}
	return tx.Commit()
}

func (r *openAIReauthRepository) SetStage(ctx context.Context, job *service.OpenAIReauthJob, stage string) (bool, error) {
	if job == nil {
		return false, errors.New("missing reauthorization job")
	}
	switch stage {
	case "refreshing", "browser_login", "exchanging":
	default:
		return false, errors.New("invalid reauthorization stage")
	}
	result, err := r.db.ExecContext(ctx, `UPDATE openai_auto_reauth SET stage = $4, updated_at = NOW()
		WHERE account_id = $1 AND generation = $2 AND lease_id = $3
		AND enabled AND status = 'running' AND lease_until > NOW()`, job.AccountID, job.Generation, job.LeaseID, stage)
	if err != nil {
		return false, err
	}
	count, err := result.RowsAffected()
	return count > 0, err
}
