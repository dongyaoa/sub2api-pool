-- Optional console credentials unlock New API wallet and effective group rates.
-- Existing inference-only targets retain their current behavior.
ALTER TABLE upstream_targets
    ADD COLUMN IF NOT EXISTS newapi_user_id BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS newapi_access_token_encrypted TEXT NOT NULL DEFAULT '';

DO $$ BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conrelid = 'upstream_targets'::regclass
          AND conname = 'upstream_targets_newapi_credentials_pair'
    ) THEN
        ALTER TABLE upstream_targets
            ADD CONSTRAINT upstream_targets_newapi_credentials_pair CHECK (
                (newapi_user_id = 0 AND newapi_access_token_encrypted = '') OR
                (newapi_user_id > 0 AND newapi_access_token_encrypted <> '')
            );
    END IF;
END $$;

ALTER TABLE IF EXISTS upstream_balance_snapshots
    ADD COLUMN IF NOT EXISTS unlimited_quota BOOLEAN NOT NULL DEFAULT FALSE;

-- Minimal repository fixtures may omit finance tables; production has them.
DO $$ BEGIN
    IF to_regclass('upstream_billing_snapshots') IS NOT NULL THEN
        ALTER TABLE upstream_billing_snapshots
            DROP CONSTRAINT IF EXISTS upstream_billing_snapshots_source_check;
        ALTER TABLE upstream_billing_snapshots
            ADD CONSTRAINT upstream_billing_snapshots_source_check CHECK (
                source IN ('sub2api_billing','sub2api_usage','newapi_token','newapi_account','newapi_pricing','unknown')
            );
    END IF;
END $$;
