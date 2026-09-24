-- OAuth plans reference account identity only. Credentials remain owned by accounts.
ALTER TABLE intelligence_monitor_plans ADD COLUMN IF NOT EXISTS account_id BIGINT REFERENCES accounts(id) ON DELETE SET NULL;
ALTER TABLE intelligence_monitor_plans DROP CONSTRAINT IF EXISTS intelligence_monitor_plans_source_type_check;
ALTER TABLE intelligence_monitor_plans ADD CONSTRAINT intelligence_monitor_plans_source_type_check CHECK(source_type IN ('external','upstream','local_group','openai_oauth'));
CREATE INDEX IF NOT EXISTS idx_intelligence_plans_account ON intelligence_monitor_plans(account_id) WHERE deleted_at IS NULL AND source_type='openai_oauth';
