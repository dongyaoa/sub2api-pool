-- Hourly financial facts survive monitor-detail retention and inventory purge.
-- No foreign keys: historical attribution must outlive current configuration.
CREATE TABLE IF NOT EXISTS upstream_monitor_cost_rollups (
    hour_start TIMESTAMPTZ NOT NULL,
    target_id BIGINT NOT NULL,
    supplier_id BIGINT,
    first_sample_at TIMESTAMPTZ NOT NULL,
    last_sample_at TIMESTAMPTZ NOT NULL,
    cost NUMERIC(24,10) NOT NULL DEFAULT 0,
    unpriced_count BIGINT NOT NULL DEFAULT 0,
    reported_count BIGINT NOT NULL DEFAULT 0,
    estimated_count BIGINT NOT NULL DEFAULT 0,
    sample_count BIGINT NOT NULL DEFAULT 0,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK (supplier_id IS NULL OR supplier_id > 0),
    CHECK (first_sample_at <= last_sample_at AND first_sample_at >= hour_start AND last_sample_at < hour_start + INTERVAL '1 hour'),
    CHECK (unpriced_count >= 0 AND reported_count >= 0 AND estimated_count >= 0 AND sample_count >= 0)
);
CREATE UNIQUE INDEX IF NOT EXISTS upstream_monitor_cost_rollups_identity_idx
    ON upstream_monitor_cost_rollups (hour_start,target_id,COALESCE(supplier_id,0));
CREATE INDEX IF NOT EXISTS upstream_monitor_cost_rollups_supplier_time_idx
    ON upstream_monitor_cost_rollups (supplier_id,hour_start);
CREATE INDEX IF NOT EXISTS upstream_monitor_cost_rollups_target_time_idx
    ON upstream_monitor_cost_rollups (target_id,hour_start);

CREATE INDEX IF NOT EXISTS upstream_monitor_history_retention_idx
    ON upstream_monitor_history (checked_at,id);
CREATE INDEX IF NOT EXISTS upstream_balance_snapshots_retention_idx
    ON upstream_balance_snapshots (synced_at,id);
CREATE INDEX IF NOT EXISTS upstream_balance_snapshots_good_idx
    ON upstream_balance_snapshots (target_id,identity_hash,synced_at DESC,id DESC) WHERE status='ok';
CREATE INDEX IF NOT EXISTS upstream_billing_snapshots_retention_idx
    ON upstream_billing_snapshots (attempted_at,id);

-- Used only to locate cleanup candidates. Deletion rechecks the current Go
-- identity under a target lock. Bytea separators match Go's NUL-separated hash.
CREATE OR REPLACE FUNCTION upstream_storage_identity_hash(
    provider TEXT, endpoint TEXT, api_key_encrypted TEXT, supplier_id BIGINT,
    wallet_ref TEXT, newapi_user_id BIGINT, newapi_access_token_encrypted TEXT
) RETURNS TEXT LANGUAGE SQL IMMUTABLE AS $$
    SELECT encode(sha256(
        convert_to(provider,'UTF8') || '\x00'::bytea ||
        convert_to(endpoint,'UTF8') || '\x00'::bytea ||
        convert_to(api_key_encrypted,'UTF8') || '\x00'::bytea ||
        convert_to(COALESCE(supplier_id::text,''),'UTF8') || '\x00'::bytea ||
        convert_to(wallet_ref,'UTF8') ||
        CASE WHEN newapi_user_id <> 0 OR newapi_access_token_encrypted <> '' THEN
            '\x00'::bytea || convert_to('newapi:' || newapi_user_id::text,'UTF8') ||
            '\x00'::bytea || convert_to(newapi_access_token_encrypted,'UTF8')
        ELSE ''::bytea END
    ),'hex');
$$;
