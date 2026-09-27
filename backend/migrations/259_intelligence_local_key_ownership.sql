-- Existing local monitor keys were generated for the plan. Borrowed administrator
-- keys must survive changing, archiving or permanently deleting their monitor.
ALTER TABLE intelligence_monitor_plans
    ADD COLUMN IF NOT EXISTS local_api_key_borrowed BOOLEAN NOT NULL DEFAULT FALSE;
