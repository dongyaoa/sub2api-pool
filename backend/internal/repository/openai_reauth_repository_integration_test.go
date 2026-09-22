//go:build integration

package repository

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func reauthIntegrationAccount(t *testing.T) (*service.Account, service.OpenAIReauthRepository) {
	t.Helper()
	ctx := context.Background()
	var proxyID, accountID int64
	err := integrationDB.QueryRowContext(ctx, `INSERT INTO proxies (name, protocol, host, port, status)
		VALUES ('reauth-test', 'http', 'proxy.example', 8080, 'active') RETURNING id`).Scan(&proxyID)
	require.NoError(t, err)
	err = integrationDB.QueryRowContext(ctx, `INSERT INTO accounts
		(name, platform, type, credentials, extra, status, schedulable, proxy_id, concurrency, priority)
		VALUES ('reauth-test', 'openai', 'oauth', '{"access_token":"old","model_mapping":{"a":"b"}}',
		'{"ordinary":"keep"}', 'active', true, $1, 7, 4) RETURNING id`, proxyID).Scan(&accountID)
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = integrationDB.ExecContext(ctx, `DELETE FROM scheduler_outbox WHERE account_id=$1`, accountID)
		_, _ = integrationDB.ExecContext(ctx, `DELETE FROM accounts WHERE id=$1`, accountID)
		_, _ = integrationDB.ExecContext(ctx, `DELETE FROM proxies WHERE id=$1`, proxyID)
	})
	account := &service.Account{ID: accountID, Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth,
		Status: service.StatusActive, Schedulable: true, ProxyID: &proxyID,
		Credentials: map[string]any{"access_token": "old", "model_mapping": map[string]any{"a": "b"}},
		Extra:       map[string]any{"ordinary": "keep"},
		Proxy:       &service.Proxy{ID: proxyID, Protocol: "http", Host: "proxy.example", Port: 8080, Status: service.StatusActive}}
	return account, NewOpenAIReauthRepository(integrationDB)
}

func TestOpenAIReauthIntegrationBindingPreservesHealthAndManualDisable(t *testing.T) {
	ctx := context.Background()
	a, repo := reauthIntegrationAccount(t)
	for _, disabled := range []bool{false, true} {
		if disabled {
			_, err := integrationDB.ExecContext(ctx, `UPDATE accounts SET status='disabled',schedulable=false WHERE id=$1`, a.ID)
			require.NoError(t, err)
		}
		require.NoError(t, repo.SaveBinding(ctx, a, "synthetic-secret", "fixture@example.test", true))
		cfg, err := repo.Get(ctx, a.ID)
		require.NoError(t, err)
		require.Equal(t, "idle", cfg.Status)
		require.Equal(t, "idle", cfg.Stage)
		job, err := repo.Claim(ctx, "unexpected-work", time.Minute)
		require.NoError(t, err)
		require.Nil(t, job)
		var status, token string
		var schedulable, pending bool
		require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT status,schedulable,
			credentials->>'access_token',COALESCE(extra->'openai_auto_reauth_pending'='true'::jsonb,false)
			FROM accounts WHERE id=$1`, a.ID).Scan(&status, &schedulable, &token, &pending))
		require.Equal(t, !disabled, schedulable)
		require.Equal(t, "old", token)
		require.False(t, pending)
		if disabled {
			require.Equal(t, "disabled", status)
		}
	}
}

func TestOpenAIReauthIntegrationBindingCancelsStagesWithoutClearingQuarantine(t *testing.T) {
	ctx := context.Background()
	a, repo := reauthIntegrationAccount(t)
	require.NoError(t, repo.Save(ctx, a.ID, "synthetic-secret", true))
	job, err := repo.Claim(ctx, "stage-worker", time.Minute)
	require.NoError(t, err)
	require.NotNil(t, job)
	for _, stage := range []string{"refreshing", "browser_login", "exchanging"} {
		applied, err := repo.SetStage(ctx, job, stage)
		require.NoError(t, err)
		require.True(t, applied)
		cfg, err := repo.Get(ctx, a.ID)
		require.NoError(t, err)
		require.Equal(t, stage, cfg.Stage)
	}
	require.NoError(t, repo.SaveBinding(ctx, a, "replacement-secret", "fixture@example.test", true))
	applied, err := repo.SetStage(ctx, job, "exchanging")
	require.NoError(t, err)
	require.False(t, applied)
	cfg, err := repo.Get(ctx, a.ID)
	require.NoError(t, err)
	require.Equal(t, "blocked", cfg.Status)
	require.Equal(t, "credentials_updated", cfg.LastErrorCode)
	var pending bool
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT extra->'openai_auto_reauth_pending'='true'::jsonb FROM accounts WHERE id=$1`, a.ID).Scan(&pending))
	require.True(t, pending)
	_, err = integrationDB.ExecContext(ctx, `UPDATE accounts SET credentials=credentials||'{"access_token":"rotated"}'::jsonb WHERE id=$1`, a.ID)
	require.NoError(t, err)
	require.ErrorIs(t, repo.SaveBinding(ctx, a, "stale", "fixture@example.test", true), service.ErrOpenAIReauthInvalidAccount)
}

func TestOpenAIReauthIntegrationAtomicCompletionPreservesSettings(t *testing.T) {
	ctx := context.Background()
	a, repo := reauthIntegrationAccount(t)
	require.NoError(t, repo.Save(ctx, a.ID, "synthetic-encrypted-secret", true))
	job, err := repo.Claim(ctx, "test-worker", time.Minute)
	require.NoError(t, err)
	require.NotNil(t, job)
	valid, err := repo.Validate(ctx, job)
	require.NoError(t, err)
	require.True(t, valid)
	// Settings and unrelated cooldowns can change during browser authorization.
	_, err = integrationDB.ExecContext(ctx, `UPDATE accounts SET concurrency=13, priority=11,
		rate_limit_reset_at=NOW()+INTERVAL '1 hour', temp_unschedulable_until=NOW()+INTERVAL '30 minutes',
		temp_unschedulable_reason='unrelated_limit' WHERE id=$1`, a.ID)
	require.NoError(t, err)
	applied, err := repo.Complete(ctx, job, a, map[string]any{"access_token": "new", "refresh_token": "new-refresh"})
	require.NoError(t, err)
	require.True(t, applied)
	var rawCredentials, rawExtra []byte
	var concurrency, priority int
	var reason string
	var rateLimit, tempUntil *time.Time
	err = integrationDB.QueryRowContext(ctx, `SELECT credentials, extra, concurrency, priority,
		rate_limit_reset_at, temp_unschedulable_until, temp_unschedulable_reason FROM accounts WHERE id=$1`, a.ID).
		Scan(&rawCredentials, &rawExtra, &concurrency, &priority, &rateLimit, &tempUntil, &reason)
	require.NoError(t, err)
	var credentials, extra map[string]any
	require.NoError(t, json.Unmarshal(rawCredentials, &credentials))
	require.NoError(t, json.Unmarshal(rawExtra, &extra))
	require.Equal(t, "new", credentials["access_token"])
	require.Equal(t, map[string]any{"a": "b"}, credentials["model_mapping"])
	require.Equal(t, "keep", extra["ordinary"])
	require.NotContains(t, extra, service.OpenAIReauthPendingKey)
	require.Equal(t, 13, concurrency)
	require.Equal(t, 11, priority)
	require.NotNil(t, rateLimit)
	require.NotNil(t, tempUntil)
	require.Equal(t, "unrelated_limit", reason)
	status, err := repo.Get(ctx, a.ID)
	require.NoError(t, err)
	require.Equal(t, "succeeded", status.Status)
}

func TestOpenAIReauthIntegrationConcurrentClaimAndStaleLease(t *testing.T) {
	ctx := context.Background()
	a, repo := reauthIntegrationAccount(t)
	require.NoError(t, repo.Save(ctx, a.ID, "synthetic", true))
	var wg sync.WaitGroup
	jobs := make(chan *service.OpenAIReauthJob, 2)
	errs := make(chan error, 2)
	for _, worker := range []string{"first", "second"} {
		wg.Add(1)
		go func(worker string) {
			defer wg.Done()
			job, err := repo.Claim(ctx, worker, time.Minute)
			jobs <- job
			errs <- err
		}(worker)
	}
	wg.Wait()
	close(jobs)
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	var winner *service.OpenAIReauthJob
	for job := range jobs {
		if job != nil {
			require.Nil(t, winner)
			winner = job
		}
	}
	require.NotNil(t, winner)
	// Saving configuration cancels the old generation before it can write tokens.
	require.NoError(t, repo.Save(ctx, a.ID, "replacement", true))
	valid, err := repo.Validate(ctx, winner)
	require.NoError(t, err)
	require.False(t, valid)
	applied, err := repo.Complete(ctx, winner, a, map[string]any{"access_token": "stale"})
	require.NoError(t, err)
	require.False(t, applied)
}

func TestOpenAIReauthIntegrationRejectsConcurrentIdentityChanges(t *testing.T) {
	for _, tc := range []struct{ name, sql string }{
		{"manual_disable", `UPDATE accounts SET schedulable=false WHERE id=$1`},
		{"manual_status", `UPDATE accounts SET status='disabled' WHERE id=$1`},
		{"new_token", `UPDATE accounts SET credentials=credentials || '{"access_token":"admin-token"}' WHERE id=$1`},
		{"proxy_endpoint", `UPDATE proxies SET host='new-proxy.example' WHERE id=(SELECT proxy_id FROM accounts WHERE id=$1)`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			a, repo := reauthIntegrationAccount(t)
			require.NoError(t, repo.Save(ctx, a.ID, "synthetic", true))
			job, err := repo.Claim(ctx, "worker", time.Minute)
			require.NoError(t, err)
			require.NotNil(t, job)
			_, err = integrationDB.ExecContext(ctx, tc.sql, a.ID)
			require.NoError(t, err)
			valid, err := repo.Validate(ctx, job)
			require.NoError(t, err)
			require.False(t, valid)
			applied, err := repo.Complete(ctx, job, a, map[string]any{"access_token": "browser-token"})
			require.NoError(t, err)
			require.False(t, applied)
		})
	}
}

func TestOpenAIReauthIntegrationExpiredLeaseHasFiniteAttempts(t *testing.T) {
	ctx := context.Background()
	a, repo := reauthIntegrationAccount(t)
	require.NoError(t, repo.Save(ctx, a.ID, "synthetic", true))
	for attempt := 1; attempt <= service.OpenAIReauthMaxAttempts; attempt++ {
		job, err := repo.Claim(ctx, "restart", time.Minute)
		require.NoError(t, err)
		require.NotNil(t, job)
		require.Equal(t, attempt, job.Attempts)
		_, err = integrationDB.ExecContext(ctx, `UPDATE openai_auto_reauth SET lease_until=NOW()-INTERVAL '1 second' WHERE account_id=$1`, a.ID)
		require.NoError(t, err)
	}
	job, err := repo.Claim(ctx, "restart", time.Minute)
	require.NoError(t, err)
	require.Nil(t, job)
	status, err := repo.Get(ctx, a.ID)
	require.NoError(t, err)
	require.Equal(t, "failed", status.Status)
	var pending bool
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT extra->'openai_auto_reauth_pending'='true'::jsonb FROM accounts WHERE id=$1`, a.ID).Scan(&pending))
	require.True(t, pending)
	require.NoError(t, repo.Save(ctx, a.ID, "", false))
	require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT extra->'openai_auto_reauth_pending'='true'::jsonb FROM accounts WHERE id=$1`, a.ID).Scan(&pending))
	require.True(t, pending, "disabling automation must not reactivate rejected credentials")
}

func TestOpenAIReauthIntegrationManualCompletionPreservesDisabledState(t *testing.T) {
	ctx := context.Background()
	a, repo := reauthIntegrationAccount(t)
	require.NoError(t, repo.Save(ctx, a.ID, "synthetic", true))
	oldJob, err := repo.Claim(ctx, "old-worker", time.Minute)
	require.NoError(t, err)
	require.NotNil(t, oldJob)
	require.NoError(t, repo.Save(ctx, a.ID, "", false))
	config, err := repo.Get(ctx, a.ID)
	require.NoError(t, err)
	_, err = integrationDB.ExecContext(ctx, `UPDATE accounts SET status='disabled', schedulable=false,
		rate_limit_reset_at=NOW()+INTERVAL '1 hour', temp_unschedulable_until=NOW()+INTERVAL '30 minutes',
		temp_unschedulable_reason='other_reason' WHERE id=$1`, a.ID)
	require.NoError(t, err)
	applied, err := repo.CompleteManual(ctx, a, config.Generation, map[string]any{"access_token": "manually-authorized"})
	require.NoError(t, err)
	require.True(t, applied)
	var status, reason, accessToken string
	var schedulable, enabled, pending bool
	var rateLimit, tempUntil *time.Time
	err = integrationDB.QueryRowContext(ctx, `SELECT a.status, a.schedulable, r.enabled,
		COALESCE(a.extra->'openai_auto_reauth_pending'='true'::jsonb,false),
		a.rate_limit_reset_at, a.temp_unschedulable_until, a.temp_unschedulable_reason, a.credentials->>'access_token'
		FROM accounts a JOIN openai_auto_reauth r ON r.account_id=a.id WHERE a.id=$1`, a.ID).
		Scan(&status, &schedulable, &enabled, &pending, &rateLimit, &tempUntil, &reason, &accessToken)
	require.NoError(t, err)
	require.Equal(t, "disabled", status)
	require.False(t, schedulable)
	require.False(t, enabled)
	require.False(t, pending)
	require.NotNil(t, rateLimit)
	require.NotNil(t, tempUntil)
	require.Equal(t, "other_reason", reason)
	require.Equal(t, "manually-authorized", accessToken)
	valid, err := repo.Validate(ctx, oldJob)
	require.NoError(t, err)
	require.False(t, valid)
	after, err := repo.Get(ctx, a.ID)
	require.NoError(t, err)
	require.Equal(t, config.Generation+1, after.Generation)
	require.Equal(t, "succeeded", after.Status)
}

func TestOpenAIReauthIntegrationManualCompletionRejectsStaleSnapshot(t *testing.T) {
	for _, tc := range []struct{ name, mutation string }{
		{"generation", `UPDATE openai_auto_reauth SET generation=generation+1 WHERE account_id=$1`},
		{"credentials", `UPDATE accounts SET credentials=credentials || '{"access_token":"new-admin-token"}' WHERE id=$1`},
		{"proxy", `UPDATE proxies SET password='new-password' WHERE id=(SELECT proxy_id FROM accounts WHERE id=$1)`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			a, repo := reauthIntegrationAccount(t)
			require.NoError(t, repo.Save(ctx, a.ID, "synthetic", true))
			config, err := repo.Get(ctx, a.ID)
			require.NoError(t, err)
			_, err = integrationDB.ExecContext(ctx, tc.mutation, a.ID)
			require.NoError(t, err)
			applied, err := repo.CompleteManual(ctx, a, config.Generation, map[string]any{"access_token": "stale-callback"})
			require.NoError(t, err)
			require.False(t, applied)
			var pending bool
			require.NoError(t, integrationDB.QueryRowContext(ctx, `SELECT extra->'openai_auto_reauth_pending'='true'::jsonb FROM accounts WHERE id=$1`, a.ID).Scan(&pending))
			require.True(t, pending)
		})
	}
}
