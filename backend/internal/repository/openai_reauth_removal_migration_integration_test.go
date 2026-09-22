//go:build integration

package repository

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/migrations"
	"github.com/stretchr/testify/require"
)

func TestRemoveOpenAIAutoReauthMigration_PreservesAccountsAndRetiresSecrets(t *testing.T) {
	tx := testTx(t)
	ctx := context.Background()
	// Temporary tables and a transaction-local search path isolate every mutation,
	// including the table drop, from the integration database's actual accounts.
	_, err := tx.ExecContext(ctx, `
		SET LOCAL search_path = pg_temp;
		CREATE TEMP TABLE accounts (
			id BIGINT PRIMARY KEY, platform TEXT DEFAULT 'openai', type TEXT DEFAULT 'oauth',
			parent_account_id BIGINT, credentials JSONB DEFAULT '{}', extra JSONB DEFAULT '{}',
			schedulable BOOLEAN DEFAULT TRUE, status TEXT DEFAULT 'active', proxy_id BIGINT DEFAULT 7,
			concurrency INT DEFAULT 4, priority INT DEFAULT 9,
			rate_limit_reset_at TIMESTAMPTZ DEFAULT '2030-01-01',
			updated_at TIMESTAMPTZ DEFAULT '2020-01-01', deleted_at TIMESTAMPTZ
		);
		CREATE TEMP TABLE scheduler_outbox (event_type TEXT, account_id BIGINT);
		CREATE TEMP TABLE openai_auto_reauth (account_id BIGINT, encrypted_secret TEXT);
		INSERT INTO accounts (id, credentials, extra) VALUES
			(1, '{"email":"new@example.test"}', '{"openai_auto_reauth_enabled":true,"openai_auto_reauth_pending":true,"setting":"keep"}'),
			(2, '{"access_token":"existing"}', '{"openai_auto_reauth_enabled":true,"setting":"keep"}'),
			(3, '{"refresh_token":"refresh-only"}', '{"openai_auto_reauth_pending":true}'),
			(4, '{"access_token":"disabled"}', '{"openai_auto_reauth_enabled":false}'),
			(5, '{}', '{"setting":"unrelated"}'),
			(6, '{}', '{"openai_auto_reauth_pending":true}'),
			(7, '{}', '{"openai_auto_reauth_pending":true}'),
			(8, '{}', '{"openai_auto_reauth_enabled":true}'),
			(9, '{"access_token":"   ","refresh_token":" "}', '{"openai_auto_reauth_pending":true}'),
			(10, '{"access_token":"paused"}', '{"openai_auto_reauth_enabled":true}'),
			(11, '{}', '{"openai_auto_reauth_enabled":true}');
		UPDATE accounts SET status='disabled', schedulable=FALSE WHERE id=4;
		UPDATE accounts SET parent_account_id=2 WHERE id=6;
		UPDATE accounts SET platform='anthropic' WHERE id=7;
		UPDATE accounts SET type='apikey' WHERE id=8;
		UPDATE accounts SET schedulable=FALSE WHERE id=10;
		UPDATE accounts SET deleted_at=NOW() WHERE id=11;
		INSERT INTO openai_auto_reauth VALUES (1, 'synthetic-encrypted-secret');
		CREATE TEMP TABLE original_accounts AS SELECT * FROM accounts;
	`)
	require.NoError(t, err)
	migration, err := migrations.FS.ReadFile("241_remove_openai_auto_reauth.sql")
	require.NoError(t, err)
	_, err = tx.ExecContext(ctx, string(migration))
	require.NoError(t, err)

	var changedSettings, remainingFlags, missingAccounts, unexpectedScheduling, outboxCount int
	require.NoError(t, tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM accounts a JOIN original_accounts o USING (id)
		WHERE to_jsonb(a) - ARRAY['extra','schedulable','updated_at']
			IS DISTINCT FROM to_jsonb(o) - ARRAY['extra','schedulable','updated_at']
		OR a.extra IS DISTINCT FROM o.extra - 'openai_auto_reauth_enabled' - 'openai_auto_reauth_pending'`).Scan(&changedSettings))
	require.Zero(t, changedSettings, "credentials, proxy, limits and unrelated settings must remain unchanged")
	require.NoError(t, tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM accounts
		WHERE extra ? 'openai_auto_reauth_enabled' OR extra ? 'openai_auto_reauth_pending'`).Scan(&remainingFlags))
	require.Zero(t, remainingFlags)
	require.NoError(t, tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM original_accounts o
		WHERE NOT EXISTS (SELECT 1 FROM accounts a WHERE a.id=o.id)`).Scan(&missingAccounts))
	require.Zero(t, missingAccounts)
	require.NoError(t, tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM accounts a JOIN original_accounts o USING (id)
		WHERE a.schedulable IS DISTINCT FROM CASE WHEN a.id IN (1,9,11) THEN FALSE ELSE o.schedulable END`).Scan(&unexpectedScheduling))
	require.Zero(t, unexpectedScheduling, "only uncredentialed imported owners should be taken out of scheduling")
	var secretsRemoved bool
	require.NoError(t, tx.QueryRowContext(ctx, `SELECT to_regclass('pg_temp.openai_auto_reauth') IS NULL`).Scan(&secretsRemoved))
	require.True(t, secretsRemoved)
	require.NoError(t, tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM scheduler_outbox
		WHERE event_type='account_changed' AND account_id IN (1,2,3,4,6,7,8,9,10)`).Scan(&outboxCount))
	require.Equal(t, 9, outboxCount)
	require.NoError(t, tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM scheduler_outbox`).Scan(&outboxCount))
	require.Equal(t, 9, outboxCount, "unrelated and deleted accounts should not be published")

	_, err = tx.ExecContext(ctx, string(migration))
	require.NoError(t, err, "retirement migration should be idempotent")
	require.NoError(t, tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM scheduler_outbox`).Scan(&outboxCount))
	require.Equal(t, 9, outboxCount, "second application should not republish account changes")
}
