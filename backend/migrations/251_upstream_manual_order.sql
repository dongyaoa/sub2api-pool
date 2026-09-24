-- Display order is independent of configuration timestamps and scheduler state.
-- NULL retains the original list ordering and places new entries after entries
-- that an administrator has explicitly ordered.
ALTER TABLE upstream_suppliers ADD COLUMN IF NOT EXISTS sort_order BIGINT;
ALTER TABLE upstream_targets ADD COLUMN IF NOT EXISTS sort_order BIGINT;
ALTER TABLE intelligence_monitor_plans ADD COLUMN IF NOT EXISTS sort_order BIGINT;

CREATE INDEX IF NOT EXISTS idx_upstream_suppliers_display_order
    ON upstream_suppliers(sort_order ASC NULLS LAST, id) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_upstream_targets_display_order
    ON upstream_targets(supplier_id, sort_order ASC NULLS LAST, id) WHERE deleted_at IS NULL;
CREATE INDEX IF NOT EXISTS idx_intelligence_plans_display_order
    ON intelligence_monitor_plans(sort_order ASC NULLS LAST, created_at DESC, id DESC) WHERE deleted_at IS NULL;
