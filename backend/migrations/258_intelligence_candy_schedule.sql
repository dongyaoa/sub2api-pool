-- Candy testing has an independent cadence. Initialize only once so rerunning
-- the migration cannot reset existing countdowns or create duplicate work.
DO $$
DECLARE initialize_candy_schedule BOOLEAN;
BEGIN
  SELECT NOT EXISTS (
    SELECT 1 FROM information_schema.columns
    WHERE table_schema=current_schema() AND table_name='intelligence_monitor_plans'
      AND column_name='candy_interval_seconds'
  ) INTO initialize_candy_schedule;

  ALTER TABLE intelligence_monitor_plans
    ADD COLUMN IF NOT EXISTS candy_interval_seconds INTEGER NOT NULL DEFAULT 180,
    ADD COLUMN IF NOT EXISTS candy_last_run_at TIMESTAMPTZ,
    ADD COLUMN IF NOT EXISTS candy_next_run_at TIMESTAMPTZ;

  IF initialize_candy_schedule THEN
    UPDATE intelligence_monitor_plans p SET
      candy_last_run_at=(SELECT MAX(finished_at) FROM intelligence_monitor_runs r WHERE r.plan_id=p.id AND r.test_kind='candy' AND r.status IN ('succeeded','failed')),
      candy_next_run_at=CASE WHEN enabled AND candy_enabled AND deleted_at IS NULL AND NOT EXISTS(
        SELECT 1 FROM intelligence_monitor_runs r WHERE r.plan_id=p.id AND r.test_kind='candy' AND r.status IN ('pending','running')
      ) THEN NOW() ELSE NULL END,
      next_run_at=CASE WHEN enabled AND deleted_at IS NULL AND NOT EXISTS(
        SELECT 1 FROM intelligence_monitor_runs r WHERE r.plan_id=p.id AND r.test_kind='pelican' AND r.status IN ('pending','running')
      ) THEN COALESCE(next_run_at,NOW()) ELSE NULL END;
  END IF;
END $$;

ALTER TABLE intelligence_monitor_plans DROP CONSTRAINT IF EXISTS intelligence_monitor_plans_candy_interval_check;
ALTER TABLE intelligence_monitor_plans ADD CONSTRAINT intelligence_monitor_plans_candy_interval_check CHECK(candy_interval_seconds IN (180,300,600,900));
CREATE INDEX IF NOT EXISTS idx_intelligence_plans_candy_due
  ON intelligence_monitor_plans(candy_next_run_at) WHERE enabled AND candy_enabled AND deleted_at IS NULL;
