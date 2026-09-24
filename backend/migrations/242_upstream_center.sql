-- Private admin upstream inventory and active monitoring. Financial identities
-- are retained after archival; history stores the supplier at check time.
CREATE TABLE IF NOT EXISTS upstream_suppliers (
    id BIGSERIAL PRIMARY KEY,
    name VARCHAR(100) NOT NULL,
    website VARCHAR(500) NOT NULL DEFAULT '',
    notes TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMPTZ
);

CREATE TABLE IF NOT EXISTS upstream_targets (
    id BIGSERIAL PRIMARY KEY,
    supplier_id BIGINT REFERENCES upstream_suppliers(id),
    name VARCHAR(100) NOT NULL,
    provider VARCHAR(20) NOT NULL CHECK (provider IN ('openai','anthropic','gemini')),
    api_mode VARCHAR(30) NOT NULL DEFAULT 'chat_completions',
    endpoint VARCHAR(500) NOT NULL,
    api_key_encrypted TEXT NOT NULL,
    api_key_fingerprint VARCHAR(64) NOT NULL,
    models JSONB NOT NULL DEFAULT '[]'::JSONB,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    interval_seconds INTEGER NOT NULL DEFAULT 300 CHECK (interval_seconds BETWEEN 60 AND 3600),
    timeout_seconds INTEGER NOT NULL DEFAULT 45 CHECK (timeout_seconds BETWEEN 5 AND 45),
    degraded_threshold_ms INTEGER NOT NULL DEFAULT 6000 CHECK (degraded_threshold_ms BETWEEN 100 AND 45000),
    wallet_ref VARCHAR(100) NOT NULL DEFAULT 'default',
    notes TEXT NOT NULL DEFAULT '',
    last_checked_at TIMESTAMPTZ,
    next_check_at TIMESTAMPTZ DEFAULT NOW(),
    check_token VARCHAR(36) NOT NULL DEFAULT '',
    lease_until TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS idx_upstream_targets_due ON upstream_targets(next_check_at)
    WHERE deleted_at IS NULL AND enabled = TRUE;
CREATE INDEX IF NOT EXISTS idx_upstream_targets_supplier ON upstream_targets(supplier_id)
    WHERE deleted_at IS NULL;
CREATE UNIQUE INDEX IF NOT EXISTS idx_upstream_targets_unique_key ON upstream_targets(api_key_fingerprint)
    WHERE deleted_at IS NULL AND supplier_id IS NOT NULL;

CREATE TABLE IF NOT EXISTS upstream_account_bindings (
    id BIGSERIAL PRIMARY KEY,
    target_id BIGINT NOT NULL REFERENCES upstream_targets(id),
    -- Deliberately no account foreign key: keep attribution after hard deletion.
    account_id BIGINT NOT NULL,
    supplier_id BIGINT REFERENCES upstream_suppliers(id),
    target_name VARCHAR(100) NOT NULL,
    supplier_name VARCHAR(100) NOT NULL DEFAULT '',
    valid_from TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    valid_until TIMESTAMPTZ,
    CHECK (valid_until IS NULL OR valid_until >= valid_from)
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_upstream_bindings_active_account
    ON upstream_account_bindings(account_id) WHERE valid_until IS NULL;
CREATE INDEX IF NOT EXISTS idx_upstream_bindings_target ON upstream_account_bindings(target_id, valid_from);
CREATE INDEX IF NOT EXISTS idx_upstream_bindings_account_period ON upstream_account_bindings(account_id, valid_from, valid_until);

CREATE TABLE IF NOT EXISTS upstream_monitor_history (
    id BIGSERIAL PRIMARY KEY,
    target_id BIGINT NOT NULL REFERENCES upstream_targets(id),
    supplier_id BIGINT REFERENCES upstream_suppliers(id),
    target_name VARCHAR(100) NOT NULL,
    supplier_name VARCHAR(100) NOT NULL DEFAULT '',
    model VARCHAR(200) NOT NULL,
    status VARCHAR(20) NOT NULL CHECK (status IN ('operational','degraded','failed','error')),
    latency_ms INTEGER,
    ping_latency_ms INTEGER,
    http_status INTEGER,
    message VARCHAR(500) NOT NULL DEFAULT '',
    checked_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    cost NUMERIC(20,10),
    cost_source VARCHAR(20) NOT NULL DEFAULT 'unknown'
);
CREATE INDEX IF NOT EXISTS idx_upstream_history_target_model_time
    ON upstream_monitor_history(target_id, model, checked_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS idx_upstream_history_supplier_time
    ON upstream_monitor_history(supplier_id, checked_at DESC);
CREATE INDEX IF NOT EXISTS idx_upstream_history_target_time
    ON upstream_monitor_history(target_id, checked_at DESC, id DESC);
