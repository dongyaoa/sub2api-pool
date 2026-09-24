-- Remote key billing declarations are observational. No local account pricing
-- column is changed by this migration or by the synchronization worker.
CREATE TABLE IF NOT EXISTS upstream_billing_snapshots (
    id BIGSERIAL PRIMARY KEY,
    target_id BIGINT NOT NULL REFERENCES upstream_targets(id),
    identity_hash VARCHAR(64) NOT NULL,
    status VARCHAR(20) NOT NULL CHECK (status IN ('ok','error','unsupported')),
    source VARCHAR(30) NOT NULL CHECK (source IN ('sub2api_billing','sub2api_usage','unknown')),
    data JSONB NOT NULL,
    attempted_at TIMESTAMPTZ NOT NULL
);
CREATE INDEX IF NOT EXISTS upstream_billing_snapshots_latest_idx
    ON upstream_billing_snapshots (target_id, identity_hash, attempted_at DESC, id DESC);
CREATE INDEX IF NOT EXISTS upstream_billing_snapshots_good_idx
    ON upstream_billing_snapshots (target_id, identity_hash, attempted_at DESC, id DESC) WHERE status = 'ok';

-- Existing supplier keys are eligible immediately; subsequent refreshes run
-- every minute without making the overview wait for upstream network calls.
UPDATE upstream_targets SET balance_next_sync_at = LEAST(balance_next_sync_at, NOW())
WHERE deleted_at IS NULL AND supplier_id IS NOT NULL;
