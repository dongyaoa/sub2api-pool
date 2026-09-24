-- Ordinary upstream key groups and independent monitors share these creation
-- defaults. Existing targets and intelligence/OAuth plans retain their settings.
ALTER TABLE upstream_targets
    ALTER COLUMN models SET DEFAULT '["gpt-5.6-sol"]'::JSONB,
    ALTER COLUMN interval_seconds SET DEFAULT 30;
ALTER TABLE upstream_targets
    DROP CONSTRAINT IF EXISTS upstream_targets_interval_seconds_check;
ALTER TABLE upstream_targets
    ADD CONSTRAINT upstream_targets_interval_seconds_check
        CHECK (interval_seconds BETWEEN 30 AND 3600);

-- Standalone monitors also synchronize the upstream-declared billing snapshot.
DROP INDEX IF EXISTS upstream_targets_balance_due_idx;
CREATE INDEX upstream_targets_balance_due_idx ON upstream_targets(balance_next_sync_at)
    WHERE deleted_at IS NULL;
