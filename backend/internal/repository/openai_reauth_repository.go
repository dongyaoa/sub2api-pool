package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

type openAIReauthRepository struct{ db *sql.DB }

func stripOpenAIReauthExtraUpdate(extra map[string]any) map[string]any {
	if len(extra) == 0 {
		return extra
	}
	filtered := make(map[string]any, len(extra))
	for key, value := range extra {
		if key != service.OpenAIReauthEnabledKey && key != service.OpenAIReauthPendingKey {
			filtered[key] = value
		}
	}
	return filtered
}

func preserveOpenAIReauthExtra(extra, current map[string]any) map[string]any {
	extra = stripOpenAIReauthExtraUpdate(extra)
	if extra == nil {
		extra = make(map[string]any)
	}
	for _, key := range []string{service.OpenAIReauthEnabledKey, service.OpenAIReauthPendingKey} {
		if value, ok := current[key]; ok {
			extra[key] = value
		}
	}
	return extra
}

func NewOpenAIReauthRepository(db *sql.DB) service.OpenAIReauthRepository {
	return &openAIReauthRepository{db: db}
}

// Hash the proxy endpoint as well as its account association. Changing an
// endpoint/password in place must invalidate an in-flight authorization too.
const reauthProxyHashSQL = `md5(jsonb_build_array(a.proxy_id,
	COALESCE(a.extra->'proxy_pool', 'null'::jsonb), p.protocol, p.host,
	p.port, p.username, p.password, p.status, p.expires_at, p.deleted_at)::text)`

const reauthColumns = `account_id, enabled, status, generation, attempts,
	next_attempt_at, last_error_code, last_success_at, trigger_reason, updated_at, stage`

func (r *openAIReauthRepository) Save(ctx context.Context, id int64, cipher string, enabled bool) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var platform, accountType, proxyStatus, existingCipher, credentialsHash, proxyHash string
	var parentID, proxyID sql.NullInt64
	var proxyDeleted, proxyExpires sql.NullTime
	var rawExtra []byte
	err = tx.QueryRowContext(ctx, `SELECT a.platform, a.type, a.parent_account_id,
		a.proxy_id, COALESCE(a.extra, '{}'::jsonb), COALESCE(p.status, ''),
		p.deleted_at, p.expires_at, COALESCE(r.encrypted_secret, ''),
		md5(COALESCE(a.credentials, '{}'::jsonb)::text), `+reauthProxyHashSQL+`
		FROM accounts a LEFT JOIN proxies p ON p.id = a.proxy_id
		LEFT JOIN openai_auto_reauth r ON r.account_id = a.id
		WHERE a.id = $1 AND a.deleted_at IS NULL FOR UPDATE OF a`, id).Scan(
		&platform, &accountType, &parentID, &proxyID, &rawExtra, &proxyStatus,
		&proxyDeleted, &proxyExpires, &existingCipher, &credentialsHash, &proxyHash)
	if errors.Is(err, sql.ErrNoRows) {
		return service.ErrAccountNotFound
	}
	if err != nil {
		return err
	}
	if cipher == "" {
		cipher = existingCipher
	}
	if enabled {
		if cipher == "" {
			return service.ErrOpenAIReauthNotConfigured
		}
		var extra map[string]any
		if err := json.Unmarshal(rawExtra, &extra); err != nil {
			return service.ErrOpenAIReauthInvalidAccount
		}
		pool, poolErr := service.ParseAccountProxyPool(extra[service.AccountProxyPoolExtraKey])
		if platform != service.PlatformOpenAI || accountType != service.AccountTypeOAuth ||
			parentID.Valid || !proxyID.Valid || proxyStatus != service.StatusActive ||
			proxyDeleted.Valid || (proxyExpires.Valid && !proxyExpires.Time.After(time.Now())) ||
			poolErr != nil || len(pool) > 1 || (len(pool) == 1 && pool[0].ProxyID != proxyID.Int64) {
			return service.ErrOpenAIReauthInvalidAccount
		}
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO openai_auto_reauth
		(account_id, encrypted_secret, enabled, status, stage, next_attempt_at,
		expected_credentials_hash, expected_proxy_hash)
		VALUES ($1, $2, $3, CASE WHEN $3 THEN 'pending' ELSE 'idle' END,
		CASE WHEN $3 THEN 'queued' ELSE 'idle' END,
		CASE WHEN $3 THEN NOW() ELSE NULL END, $4, $5)
		ON CONFLICT (account_id) DO UPDATE SET encrypted_secret = EXCLUDED.encrypted_secret,
		enabled = EXCLUDED.enabled, generation = openai_auto_reauth.generation + 1,
		status = EXCLUDED.status, stage = EXCLUDED.stage, attempts = 0, next_attempt_at = EXCLUDED.next_attempt_at,
		lease_id = NULL, lease_until = NULL, last_error_code = '', trigger_reason = '',
		expected_credentials_hash = EXCLUDED.expected_credentials_hash,
		expected_proxy_hash = EXCLUDED.expected_proxy_hash, updated_at = NOW()`, id, cipher, enabled, credentialsHash, proxyHash)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `UPDATE accounts SET
		extra = COALESCE(extra, '{}'::jsonb) ||
		jsonb_build_object('openai_auto_reauth_enabled', $2::boolean) ||
		CASE WHEN $2 THEN '{"openai_auto_reauth_pending":true}'::jsonb ELSE '{}'::jsonb END,
		updated_at = NOW()
		WHERE id = $1`, id, enabled)
	if err != nil {
		return err
	}
	if err = reauthOutbox(ctx, tx, id); err != nil {
		return err
	}
	return tx.Commit()
}

type reauthScanner interface{ Scan(...any) error }

func reauthScanStatus(row reauthScanner, status *service.OpenAIReauthStatus, tail ...any) error {
	args := []any{&status.AccountID, &status.Enabled, &status.Status, &status.Generation,
		&status.Attempts, &status.NextAttemptAt, &status.LastErrorCode,
		&status.LastSuccessAt, &status.TriggerReason, &status.UpdatedAt, &status.Stage}
	return row.Scan(append(args, tail...)...)
}

func (r *openAIReauthRepository) Get(ctx context.Context, id int64) (*service.OpenAIReauthConfig, error) {
	result := &service.OpenAIReauthConfig{}
	err := reauthScanStatus(r.db.QueryRowContext(ctx, `SELECT `+reauthColumns+`, encrypted_secret
		FROM openai_auto_reauth WHERE account_id = $1`, id), &result.OpenAIReauthStatus, &result.EncryptedSecret)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrOpenAIReauthNotConfigured
	}
	return result, err
}

func (r *openAIReauthRepository) ListStatuses(ctx context.Context, limit int) ([]service.OpenAIReauthStatus, error) {
	if limit <= 0 || limit > 5000 {
		limit = 1000
	}
	rows, err := r.db.QueryContext(ctx, `SELECT r.account_id, r.enabled, r.status, r.generation, r.attempts,
		r.next_attempt_at, r.last_error_code, r.last_success_at, r.trigger_reason, r.updated_at, r.stage,
		COALESCE(NULLIF(r.login_email, ''), NULLIF(a.credentials->>'email', ''), a.name)
		FROM openai_auto_reauth r JOIN accounts a ON a.id = r.account_id AND a.deleted_at IS NULL
		ORDER BY r.updated_at DESC, r.account_id LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]service.OpenAIReauthStatus, 0)
	for rows.Next() {
		var status service.OpenAIReauthStatus
		if err := reauthScanStatus(rows, &status, &status.Email); err != nil {
			return nil, err
		}
		result = append(result, status)
	}
	return result, rows.Err()
}

var reauthCodePattern = regexp.MustCompile(`^[a-z][a-z0-9_]{0,79}$`)

func reauthSafeCode(value string) string {
	if !reauthCodePattern.MatchString(value) {
		return "authorization_failed"
	}
	return value
}

func (r *openAIReauthRepository) Enqueue(ctx context.Context, id int64, reason string) (bool, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	// Account first, config second: the same lock order used by Save/Complete.
	var credentialsHash, proxyHash string
	err = tx.QueryRowContext(ctx, `SELECT md5(COALESCE(a.credentials, '{}'::jsonb)::text), `+
		reauthProxyHashSQL+` FROM accounts a LEFT JOIN proxies p ON p.id = a.proxy_id
		WHERE a.id = $1 AND a.deleted_at IS NULL AND a.platform = 'openai'
		AND a.type = 'oauth' AND a.parent_account_id IS NULL
		AND a.status = 'active' AND a.schedulable IS TRUE
		FOR UPDATE OF a`, id).Scan(&credentialsHash, &proxyHash)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	var status string
	err = tx.QueryRowContext(ctx, `SELECT status FROM openai_auto_reauth
		WHERE account_id = $1 AND enabled FOR UPDATE`, id).Scan(&status)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if status == "idle" || status == "succeeded" {
		_, err = tx.ExecContext(ctx, `UPDATE openai_auto_reauth SET status = 'pending', stage = 'queued',
			attempts = 0, next_attempt_at = NOW(), lease_id = NULL, lease_until = NULL,
			last_error_code = '', trigger_reason = $2, expected_credentials_hash = $3,
			expected_proxy_hash = $4, updated_at = NOW() WHERE account_id = $1`,
			id, reauthSafeCode(reason), credentialsHash, proxyHash)
		if err != nil {
			return false, err
		}
	}
	_, err = tx.ExecContext(ctx, `UPDATE accounts SET extra = COALESCE(extra, '{}'::jsonb) ||
		'{"openai_auto_reauth_pending":true}'::jsonb, updated_at = NOW() WHERE id = $1`, id)
	if err != nil {
		return false, err
	}
	if err = reauthOutbox(ctx, tx, id); err != nil {
		return false, err
	}
	if err = tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}

func (r *openAIReauthRepository) Claim(ctx context.Context, leaseID string, ttl time.Duration) (*service.OpenAIReauthJob, error) {
	if leaseID == "" || ttl <= 0 {
		return nil, errors.New("invalid reauthorization lease")
	}
	// A process can die on its final attempt. Convert that expired lease to a
	// terminal state instead of leaving it stuck as running forever.
	_, err := r.db.ExecContext(ctx, `UPDATE openai_auto_reauth SET status = 'failed', stage = 'failed',
		last_error_code = 'lease_expired', lease_id = NULL, lease_until = NULL,
		next_attempt_at = NULL, updated_at = NOW() WHERE enabled AND status = 'running'
		AND lease_until <= NOW() AND attempts >= $1`, service.OpenAIReauthMaxAttempts)
	if err != nil {
		return nil, err
	}
	job := &service.OpenAIReauthJob{}
	err = reauthScanStatus(r.db.QueryRowContext(ctx, `WITH candidate AS (
		SELECT account_id FROM openai_auto_reauth WHERE enabled AND attempts < $3
		AND ((status = 'pending' AND next_attempt_at <= NOW()) OR
		(status = 'running' AND lease_until <= NOW()))
		ORDER BY next_attempt_at NULLS FIRST, account_id FOR UPDATE SKIP LOCKED LIMIT 1
	) UPDATE openai_auto_reauth r SET status = 'running', stage = 'starting', attempts = attempts + 1,
		lease_id = $1, lease_until = NOW() + ($2 * INTERVAL '1 second'), updated_at = NOW()
		FROM candidate c WHERE r.account_id = c.account_id
		RETURNING r.account_id, r.enabled, r.status, r.generation, r.attempts,
		r.next_attempt_at, r.last_error_code, r.last_success_at, r.trigger_reason, r.updated_at, r.stage,
		r.encrypted_secret, r.lease_id, r.lease_until, r.expected_credentials_hash, r.expected_proxy_hash`,
		leaseID, ttl.Seconds(), service.OpenAIReauthMaxAttempts), &job.OpenAIReauthStatus,
		&job.EncryptedSecret, &job.LeaseID, &job.LeaseUntil, &job.ExpectedCredentialsHash, &job.ExpectedProxyHash)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return job, err
}

func (r *openAIReauthRepository) Validate(ctx context.Context, job *service.OpenAIReauthJob) (bool, error) {
	if job == nil {
		return false, errors.New("missing reauthorization job")
	}
	var valid bool
	err := r.db.QueryRowContext(ctx, `SELECT EXISTS (SELECT 1 FROM openai_auto_reauth r
		JOIN accounts a ON a.id = r.account_id JOIN proxies p ON p.id = a.proxy_id
		WHERE r.account_id = $1 AND r.generation = $2 AND r.lease_id = $3
		AND r.enabled AND r.status = 'running' AND r.lease_until > NOW()
		AND a.deleted_at IS NULL AND a.platform = 'openai' AND a.type = 'oauth'
		AND a.parent_account_id IS NULL AND a.proxy_fallback_origin_id IS NULL
		AND a.status = 'active' AND a.schedulable IS TRUE
		AND p.status = 'active' AND p.deleted_at IS NULL
		AND (p.expires_at IS NULL OR p.expires_at > NOW())
		AND md5(COALESCE(a.credentials, '{}'::jsonb)::text) = r.expected_credentials_hash
		AND `+reauthProxyHashSQL+` = r.expected_proxy_hash
		AND a.extra->'openai_auto_reauth_pending' = 'true'::jsonb)`,
		job.AccountID, job.Generation, job.LeaseID).Scan(&valid)
	return valid, err
}

func (r *openAIReauthRepository) Fail(ctx context.Context, job *service.OpenAIReauthJob, code string, retryAfter time.Duration, blocked bool) (bool, error) {
	if job == nil {
		return false, errors.New("missing reauthorization job")
	}
	status := "failed"
	if blocked {
		status = "blocked"
	} else if retryAfter > 0 && job.Attempts < service.OpenAIReauthMaxAttempts {
		status = "pending"
	}
	result, err := r.db.ExecContext(ctx, `UPDATE openai_auto_reauth SET status = $4,
		stage = CASE WHEN $4 = 'pending' THEN 'queued' ELSE $4 END,
		last_error_code = $5, next_attempt_at = CASE WHEN $4 = 'pending' THEN
		NOW() + ($6 * INTERVAL '1 second') ELSE NULL END,
		lease_id = NULL, lease_until = NULL, updated_at = NOW()
		WHERE account_id = $1 AND generation = $2 AND lease_id = $3
		AND enabled AND status = 'running' AND lease_until > NOW()`,
		job.AccountID, job.Generation, job.LeaseID, status, reauthSafeCode(code), retryAfter.Seconds())
	if err != nil {
		return false, err
	}
	count, err := result.RowsAffected()
	return count > 0, err
}

func reauthTokenPatch(patch map[string]any) ([]byte, error) {
	allowed := map[string]bool{"access_token": true, "refresh_token": true, "id_token": true,
		"expires_at": true, "expires_in": true, "token_type": true, "scope": true,
		"_token_version": true, "chatgpt_account_id": true, "chatgpt_user_id": true,
		"organization_id": true, "email": true, "plan_type": true,
		"client_id": true, "auth_mode": true, "openai_auth_mode": true,
		"chatgpt_account_is_fedramp": true, "subscription_expires_at": true, "privacy_mode": true}
	for key := range patch {
		if !allowed[key] {
			return nil, fmt.Errorf("invalid reauthorization token field: %s", key)
		}
	}
	if token, ok := patch["access_token"].(string); !ok || token == "" {
		return nil, errors.New("reauthorization requires an access token")
	}
	return json.Marshal(patch)
}

func (r *openAIReauthRepository) Complete(ctx context.Context, job *service.OpenAIReauthJob, account *service.Account, patch map[string]any) (bool, error) {
	if job == nil || account == nil || job.AccountID != account.ID {
		return false, errors.New("invalid reauthorization account snapshot")
	}
	patchJSON, err := reauthTokenPatch(patch)
	if err != nil {
		return false, err
	}
	credentialsJSON, err := json.Marshal(account.Credentials)
	if err != nil {
		return false, err
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	// Only credentials and our private marker are updated; all user settings,
	// manual status/schedulable controls and unrelated cooldowns are untouched.
	result, err := tx.ExecContext(ctx, `UPDATE accounts a SET
		credentials = COALESCE(a.credentials, '{}'::jsonb) || $4::jsonb,
		extra = COALESCE(a.extra, '{}'::jsonb) - 'openai_auto_reauth_pending', updated_at = NOW()
		FROM openai_auto_reauth r, proxies p WHERE a.id = $1 AND r.account_id = a.id
		AND p.id = a.proxy_id AND p.status = 'active' AND p.deleted_at IS NULL
		AND (p.expires_at IS NULL OR p.expires_at > NOW())
		AND a.deleted_at IS NULL AND a.platform = 'openai' AND a.type = 'oauth'
		AND a.parent_account_id IS NULL AND a.proxy_fallback_origin_id IS NULL
		AND a.status = 'active' AND a.schedulable IS TRUE
		AND r.enabled AND r.status = 'running' AND r.generation = $2
		AND r.lease_id = $3 AND r.lease_until > NOW()
		AND a.credentials = $5::jsonb AND a.proxy_id IS NOT DISTINCT FROM $6
		AND md5(COALESCE(a.credentials, '{}'::jsonb)::text) = r.expected_credentials_hash
		AND `+reauthProxyHashSQL+` = r.expected_proxy_hash
		AND a.extra->'openai_auto_reauth_pending' = 'true'::jsonb`,
		job.AccountID, job.Generation, job.LeaseID, string(patchJSON), string(credentialsJSON), account.ProxyID)
	if err != nil {
		return false, err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	if count == 0 {
		return false, nil
	}
	result, err = tx.ExecContext(ctx, `UPDATE openai_auto_reauth SET status = 'succeeded', stage = 'succeeded',
		lease_id = NULL, lease_until = NULL, next_attempt_at = NULL, last_error_code = '',
		last_success_at = NOW(), updated_at = NOW()
		WHERE account_id = $1 AND generation = $2 AND lease_id = $3
		AND enabled AND status = 'running' AND lease_until > NOW()`, job.AccountID, job.Generation, job.LeaseID)
	if err != nil {
		return false, err
	}
	count, err = result.RowsAffected()
	if err != nil {
		return false, err
	}
	if count == 0 {
		return false, nil
	}
	if err = reauthOutbox(ctx, tx, job.AccountID); err != nil {
		return false, err
	}
	if err = tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}

func reauthOutbox(ctx context.Context, tx *sql.Tx, id int64) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO scheduler_outbox (event_type, account_id, group_id, payload)
		VALUES ($1, $2, NULL, NULL)`, service.SchedulerOutboxEventAccountChanged, id)
	return err
}
