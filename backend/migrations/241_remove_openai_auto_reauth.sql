-- Remove the retired password/TOTP recovery feature without changing existing
-- grants or account settings. Newly imported accounts without either OAuth
-- token must remain out of scheduling after their private pending gate is gone.
WITH updated_accounts AS (
    UPDATE accounts
    SET schedulable = CASE
            WHEN platform = 'openai' AND type = 'oauth'
                AND parent_account_id IS NULL
                AND BTRIM(COALESCE(credentials->>'access_token', '')) = ''
                AND BTRIM(COALESCE(credentials->>'refresh_token', '')) = ''
            THEN FALSE
            ELSE schedulable
        END,
        extra = extra - 'openai_auto_reauth_enabled' - 'openai_auto_reauth_pending',
        updated_at = NOW()
    WHERE extra ? 'openai_auto_reauth_enabled'
        OR extra ? 'openai_auto_reauth_pending'
    RETURNING id, deleted_at
)
INSERT INTO scheduler_outbox (event_type, account_id)
SELECT 'account_changed', id FROM updated_accounts WHERE deleted_at IS NULL;

-- The account password and TOTP seed existed only in this encrypted table.
-- Keep migrations 239/240 unchanged so installed databases retain valid history.
DROP TABLE IF EXISTS openai_auto_reauth;
