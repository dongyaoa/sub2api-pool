-- Regrade saved candy responses locally when the answer parser changes.
-- Keep response text, execution status, timestamps and request metadata intact.
ALTER TABLE intelligence_monitor_runs
    ADD COLUMN IF NOT EXISTS candy_grade_version INTEGER NOT NULL DEFAULT 0;

CREATE INDEX IF NOT EXISTS idx_intelligence_runs_candy_regrade
    ON intelligence_monitor_runs(candy_grade_version,id)
    WHERE test_kind='candy' AND status='succeeded' AND error='';
