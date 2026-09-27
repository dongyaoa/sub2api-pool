-- Preserve legacy duplicate plans; new membership is serialized by the existing
-- advisory transaction lock rather than a retroactive uniqueness constraint.
CREATE INDEX IF NOT EXISTS idx_intelligence_plans_upstream_display
    ON intelligence_monitor_plans(upstream_target_id, sort_order ASC NULLS LAST, created_at DESC, id DESC)
    WHERE deleted_at IS NULL AND source_type = 'upstream';
