-- Private qualitative model comparison. Generated documents are untrusted data.
CREATE TABLE IF NOT EXISTS intelligence_monitor_plans (
 id BIGSERIAL PRIMARY KEY,
 name VARCHAR(100) NOT NULL,
 source_type VARCHAR(20) NOT NULL CHECK(source_type IN ('external','upstream','local_group')),
 endpoint VARCHAR(500) NOT NULL DEFAULT '',
 api_key_encrypted TEXT NOT NULL DEFAULT '',
 upstream_target_id BIGINT REFERENCES upstream_targets(id),
 group_id BIGINT,
 local_api_key_id BIGINT,
 local_key_owner_id BIGINT,
 supplier_note VARCHAR(500) NOT NULL DEFAULT '',
 group_note VARCHAR(500) NOT NULL DEFAULT '',
 rate_note VARCHAR(500) NOT NULL DEFAULT '',
 notes TEXT NOT NULL DEFAULT '',
 api_mode VARCHAR(30) NOT NULL DEFAULT 'responses' CHECK(api_mode IN ('responses','chat_completions')),
 enabled BOOLEAN NOT NULL DEFAULT FALSE,
 interval_seconds INTEGER NOT NULL DEFAULT 3600 CHECK(interval_seconds BETWEEN 300 AND 86400),
 timeout_seconds INTEGER NOT NULL DEFAULT 300 CHECK(timeout_seconds BETWEEN 180 AND 300),
 created_by BIGINT NOT NULL,
 last_run_at TIMESTAMPTZ,
 next_run_at TIMESTAMPTZ,
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
 deleted_at TIMESTAMPTZ
);
CREATE INDEX IF NOT EXISTS idx_intelligence_plans_due ON intelligence_monitor_plans(next_run_at) WHERE enabled AND deleted_at IS NULL;

CREATE TABLE IF NOT EXISTS intelligence_monitor_runs (
 id BIGSERIAL PRIMARY KEY,
 plan_id BIGINT NOT NULL REFERENCES intelligence_monitor_plans(id),
 plan_name VARCHAR(100) NOT NULL,
 status VARCHAR(20) NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','running','succeeded','failed')),
 trigger VARCHAR(20) NOT NULL CHECK(trigger IN ('manual','scheduled')),
 model VARCHAR(100) NOT NULL,
 reasoning_effort VARCHAR(20) NOT NULL,
 prompt TEXT NOT NULL,
 source_type VARCHAR(20) NOT NULL,
 source_name VARCHAR(300) NOT NULL,
 source_endpoint VARCHAR(500) NOT NULL,
 source_snapshot JSONB NOT NULL DEFAULT '{}'::jsonb,
 rate_snapshot JSONB,
 notes_snapshot JSONB NOT NULL DEFAULT '{}'::jsonb,
 api_mode VARCHAR(30) NOT NULL,
 timeout_seconds INTEGER NOT NULL,
 request_key_encrypted TEXT NOT NULL DEFAULT '',
 lease_token VARCHAR(36) NOT NULL DEFAULT '',
 lease_until TIMESTAMPTZ,
 started_at TIMESTAMPTZ,
 finished_at TIMESTAMPTZ,
 duration_ms BIGINT,
 http_status INTEGER,
 error TEXT NOT NULL DEFAULT '',
 html TEXT NOT NULL DEFAULT '',
 raw_text TEXT NOT NULL DEFAULT '',
 created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_intelligence_runs_active_plan ON intelligence_monitor_runs(plan_id) WHERE status IN ('pending','running');
CREATE INDEX IF NOT EXISTS idx_intelligence_runs_history ON intelligence_monitor_runs(plan_id,created_at DESC,id DESC);
CREATE INDEX IF NOT EXISTS idx_intelligence_runs_pending ON intelligence_monitor_runs(created_at,id) WHERE status='pending';
CREATE INDEX IF NOT EXISTS idx_intelligence_runs_running ON intelligence_monitor_runs(lease_until) WHERE status='running';
