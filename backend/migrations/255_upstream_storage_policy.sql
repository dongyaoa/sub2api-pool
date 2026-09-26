-- Retention is independent from probe and artwork scheduling. Archival remains
-- reversible at the data level; permanent purge always requires a named action.
CREATE TABLE IF NOT EXISTS upstream_storage_policy (
    id SMALLINT PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    history_retention_days INTEGER NOT NULL DEFAULT 30 CHECK (history_retention_days BETWEEN 30 AND 365),
    snapshot_retention_days INTEGER NOT NULL DEFAULT 7 CHECK (snapshot_retention_days BETWEEN 1 AND 90),
    last_cleanup_at TIMESTAMPTZ,
    last_result JSONB,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
INSERT INTO upstream_storage_policy(id) VALUES(1) ON CONFLICT(id) DO NOTHING;
