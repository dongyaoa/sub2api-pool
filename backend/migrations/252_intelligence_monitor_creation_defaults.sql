-- Defaults apply only to future plans. Existing plans and queued/history runs
-- retain their explicitly saved scheduling and generation timeout settings.
ALTER TABLE intelligence_monitor_plans
    ALTER COLUMN interval_seconds SET DEFAULT 300,
    ALTER COLUMN timeout_seconds SET DEFAULT 600;
