-- Independent ledger survives usage-log retention, account removal and later
-- supplier/key edits. No FK to any source entity is intentional.
CREATE TABLE IF NOT EXISTS upstream_finance_ledger (
    id BIGSERIAL PRIMARY KEY,
    usage_id BIGINT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL,
    target_id BIGINT NOT NULL,
    target_name VARCHAR(100) NOT NULL,
    supplier_id BIGINT,
    supplier_name VARCHAR(100) NOT NULL DEFAULT '',
    account_id BIGINT NOT NULL,
    group_id BIGINT,
    user_id BIGINT NOT NULL,
    api_key_id BIGINT NOT NULL,
    model VARCHAR(200) NOT NULL,
    request_id TEXT NOT NULL DEFAULT '',
    revenue NUMERIC(24,10) NOT NULL,
    business_cost NUMERIC(24,10) NOT NULL,
    billing_type SMALLINT NOT NULL,
    recorded_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (usage_id, created_at)
);
CREATE INDEX IF NOT EXISTS upstream_finance_ledger_supplier_time_idx
    ON upstream_finance_ledger (supplier_id, created_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS upstream_finance_ledger_target_time_idx
    ON upstream_finance_ledger (target_id, created_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS upstream_finance_ledger_time_idx
    ON upstream_finance_ledger (created_at DESC, id DESC);

CREATE OR REPLACE FUNCTION upstream_record_usage_finance()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE changed INTEGER;
BEGIN
    IF TG_OP = 'UPDATE' THEN
        -- Corrections retain the attribution captured on the first insertion.
        UPDATE upstream_finance_ledger SET
            revenue = NEW.actual_cost,
            business_cost = COALESCE(NEW.account_stats_cost, NEW.total_cost) * COALESCE(NEW.account_rate_multiplier, 1),
            model = COALESCE(NULLIF(NEW.requested_model, ''), NEW.model),
            request_id = COALESCE(NEW.request_id, ''),
            billing_type = NEW.billing_type,
            updated_at = clock_timestamp()
        WHERE usage_id = OLD.id AND created_at = OLD.created_at;
        GET DIAGNOSTICS changed = ROW_COUNT;
        IF changed > 0 THEN RETURN NEW; END IF;
    END IF;
    INSERT INTO upstream_finance_ledger
        (usage_id, created_at, target_id, target_name, supplier_id, supplier_name,
         account_id, group_id, user_id, api_key_id, model, request_id, revenue, business_cost, billing_type)
    SELECT NEW.id, NEW.created_at, b.target_id, b.target_name, b.supplier_id, b.supplier_name,
        NEW.account_id, NEW.group_id, NEW.user_id, NEW.api_key_id,
        COALESCE(NULLIF(NEW.requested_model, ''), NEW.model), COALESCE(NEW.request_id, ''),
        NEW.actual_cost,
        COALESCE(NEW.account_stats_cost, NEW.total_cost) * COALESCE(NEW.account_rate_multiplier, 1), NEW.billing_type
    FROM upstream_account_bindings b
    WHERE b.account_id = NEW.account_id AND NEW.created_at >= b.valid_from
      AND (b.valid_until IS NULL OR NEW.created_at < b.valid_until)
    ORDER BY b.valid_from DESC, b.id DESC LIMIT 1
    ON CONFLICT (usage_id, created_at) DO UPDATE SET
        revenue = EXCLUDED.revenue, business_cost = EXCLUDED.business_cost,
        model = EXCLUDED.model, request_id = EXCLUDED.request_id,
        billing_type = EXCLUDED.billing_type, updated_at = clock_timestamp();
    RETURN NEW;
END;
$$;
DROP TRIGGER IF EXISTS upstream_usage_finance_snapshot ON usage_logs;
CREATE TRIGGER upstream_usage_finance_snapshot
    AFTER INSERT OR UPDATE OF actual_cost, total_cost, account_stats_cost, account_rate_multiplier,
        requested_model, model, request_id, billing_type ON usage_logs
    FOR EACH ROW EXECUTE FUNCTION upstream_record_usage_finance();

-- Financial observations are append-only. Each observation retains the
-- supplier/wallet and credential identity that were actually queried.
CREATE TABLE IF NOT EXISTS upstream_balance_snapshots (
    id BIGSERIAL PRIMARY KEY,
    target_id BIGINT NOT NULL REFERENCES upstream_targets(id),
    supplier_id BIGINT REFERENCES upstream_suppliers(id),
    wallet_ref VARCHAR(100) NOT NULL DEFAULT 'default',
    identity_hash VARCHAR(64) NOT NULL,
    kind VARCHAR(20) NOT NULL CHECK (kind IN ('wallet','key_quota','subscription','unsupported','unknown')),
    balance NUMERIC(24,10),
    quota_remaining NUMERIC(24,10),
    today_used NUMERIC(24,10),
    total_used NUMERIC(24,10),
    currency VARCHAR(16) NOT NULL DEFAULT '',
    currency_source VARCHAR(20) NOT NULL DEFAULT '',
    status VARCHAR(20) NOT NULL CHECK (status IN ('ok','error','unsupported')),
    synced_at TIMESTAMPTZ NOT NULL,
    error VARCHAR(500) NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS upstream_balance_snapshots_latest_idx
    ON upstream_balance_snapshots (target_id, identity_hash, synced_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS upstream_balance_snapshots_supplier_time_idx
    ON upstream_balance_snapshots (supplier_id, synced_at);

ALTER TABLE upstream_targets ADD COLUMN IF NOT EXISTS balance_next_sync_at TIMESTAMPTZ NOT NULL DEFAULT NOW();
ALTER TABLE upstream_targets ADD COLUMN IF NOT EXISTS balance_lease_until TIMESTAMPTZ;
ALTER TABLE upstream_targets ADD COLUMN IF NOT EXISTS balance_lease_token VARCHAR(64);
CREATE INDEX IF NOT EXISTS upstream_targets_balance_due_idx
    ON upstream_targets (balance_next_sync_at) WHERE deleted_at IS NULL AND supplier_id IS NOT NULL;

-- Accounts can also be edited outside the upstream center. Close the historical
-- binding whenever its forwarding identity changes; do not silently bind the new
-- credentials to the old supplier. Renames and operational state edits keep it.
CREATE OR REPLACE FUNCTION upstream_close_changed_account_bindings()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'DELETE' THEN
        UPDATE upstream_account_bindings
        SET valid_until = GREATEST(clock_timestamp(), valid_from)
        WHERE account_id = OLD.id AND valid_until IS NULL;
        RETURN OLD;
    END IF;
    IF OLD.credentials ->> 'api_key' IS DISTINCT FROM NEW.credentials ->> 'api_key'
       OR OLD.credentials ->> 'base_url' IS DISTINCT FROM NEW.credentials ->> 'base_url'
       OR OLD.platform IS DISTINCT FROM NEW.platform
       OR OLD.type IS DISTINCT FROM NEW.type
       OR (OLD.deleted_at IS NULL AND NEW.deleted_at IS NOT NULL) THEN
        UPDATE upstream_account_bindings
        SET valid_until = GREATEST(clock_timestamp(), valid_from)
        WHERE account_id = NEW.id AND valid_until IS NULL;
    END IF;
    RETURN NEW;
END;
$$;
DROP TRIGGER IF EXISTS upstream_account_binding_identity_guard ON accounts;
CREATE TRIGGER upstream_account_binding_identity_guard
    BEFORE UPDATE OF credentials, platform, type, deleted_at OR DELETE ON accounts
    FOR EACH ROW EXECUTE FUNCTION upstream_close_changed_account_bindings();
