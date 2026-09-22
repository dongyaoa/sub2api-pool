-- Login secrets are envelope-encrypted by the service. Do not copy them to
-- accounts.credentials/extra or return them from administrative account APIs.
CREATE TABLE IF NOT EXISTS openai_auto_reauth (
    account_id BIGINT PRIMARY KEY REFERENCES accounts(id) ON DELETE CASCADE,
    encrypted_secret TEXT NOT NULL,
    enabled BOOLEAN NOT NULL DEFAULT FALSE,
    status TEXT NOT NULL DEFAULT 'idle'
        CHECK (status IN ('idle', 'pending', 'running', 'succeeded', 'failed', 'blocked')),
    generation BIGINT NOT NULL DEFAULT 1,
    attempts INTEGER NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    next_attempt_at TIMESTAMPTZ,
    lease_id TEXT,
    lease_until TIMESTAMPTZ,
    last_error_code TEXT NOT NULL DEFAULT '',
    last_success_at TIMESTAMPTZ,
    trigger_reason TEXT NOT NULL DEFAULT '',
    expected_credentials_hash TEXT NOT NULL DEFAULT '',
    expected_proxy_hash TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_openai_auto_reauth_due
    ON openai_auto_reauth (next_attempt_at, account_id)
    WHERE enabled AND status IN ('pending', 'running');
