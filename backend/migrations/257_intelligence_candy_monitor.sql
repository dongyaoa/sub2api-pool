-- Existing artwork remains pelican; candy monitoring is opt-in per plan.
ALTER TABLE intelligence_monitor_plans ADD COLUMN IF NOT EXISTS candy_enabled BOOLEAN NOT NULL DEFAULT FALSE;
ALTER TABLE intelligence_monitor_runs ADD COLUMN IF NOT EXISTS test_kind VARCHAR(20) NOT NULL DEFAULT 'pelican';
ALTER TABLE intelligence_monitor_runs ADD COLUMN IF NOT EXISTS correct BOOLEAN;
ALTER TABLE intelligence_monitor_runs ADD COLUMN IF NOT EXISTS answer TEXT NOT NULL DEFAULT '';

ALTER TABLE intelligence_monitor_runs DROP CONSTRAINT IF EXISTS intelligence_monitor_runs_test_kind_check;
ALTER TABLE intelligence_monitor_runs ADD CONSTRAINT intelligence_monitor_runs_test_kind_check CHECK(test_kind IN ('pelican','candy'));

-- A scheduled round may contain one run of each kind, while repeated requests
-- of the same kind remain exclusive. Global execution is still limited to two.
DROP INDEX IF EXISTS idx_intelligence_runs_active_plan;
CREATE UNIQUE INDEX IF NOT EXISTS idx_intelligence_runs_active_plan_kind
    ON intelligence_monitor_runs(plan_id,test_kind) WHERE status IN ('pending','running');
CREATE INDEX IF NOT EXISTS idx_intelligence_runs_kind_history
    ON intelligence_monitor_runs(plan_id,test_kind,created_at DESC,id DESC);
