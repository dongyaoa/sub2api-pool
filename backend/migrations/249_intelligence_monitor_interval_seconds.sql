-- Allow precise short intervals without changing existing schedules, the
-- one-hour creation default, or the generation timeout budget.
ALTER TABLE intelligence_monitor_plans
    DROP CONSTRAINT IF EXISTS intelligence_monitor_plans_interval_seconds_check;
ALTER TABLE intelligence_monitor_plans
    ADD CONSTRAINT intelligence_monitor_plans_interval_seconds_check
        CHECK (interval_seconds BETWEEN 30 AND 86400);
