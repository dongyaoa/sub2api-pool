-- Pelican generation can require fifteen minutes for every source type.
-- Keep legacy 180/240-second API inputs valid, but upgrade existing plans to
-- the new default without changing their scheduling or revision metadata.
ALTER TABLE intelligence_monitor_plans
    DROP CONSTRAINT IF EXISTS intelligence_monitor_plans_timeout_seconds_check;
ALTER TABLE intelligence_monitor_plans
    ADD CONSTRAINT intelligence_monitor_plans_timeout_seconds_check
        CHECK (timeout_seconds BETWEEN 180 AND 900);
ALTER TABLE intelligence_monitor_plans
    ALTER COLUMN timeout_seconds SET DEFAULT 900;

UPDATE intelligence_monitor_plans
SET timeout_seconds = 900
WHERE timeout_seconds BETWEEN 180 AND 300;

-- Do not rewrite intelligence_monitor_runs: pending/running requests and
-- completed history retain the timeout captured when they were queued.
