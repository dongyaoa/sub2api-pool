-- Each comparison plan fixes its model. Existing plans keep their original model;
-- historical runs already store the requested model and are never rewritten.
ALTER TABLE intelligence_monitor_plans
 ADD COLUMN IF NOT EXISTS model VARCHAR(100) NOT NULL DEFAULT 'gpt-6-astra';
ALTER TABLE intelligence_monitor_plans DROP CONSTRAINT IF EXISTS intelligence_monitor_plans_model_check;
ALTER TABLE intelligence_monitor_plans ADD CONSTRAINT intelligence_monitor_plans_model_check
 CHECK(model IN ('gpt-6-astra','gpt-6.1-sol'));

-- Membership changes are serialized by the repository. Keep these non-unique so
-- old duplicate plans remain editable until administrators choose to remove them.
CREATE INDEX IF NOT EXISTS idx_intelligence_plans_target_model
 ON intelligence_monitor_plans(upstream_target_id,model)
 WHERE deleted_at IS NULL AND source_type='upstream';
CREATE INDEX IF NOT EXISTS idx_intelligence_plans_account_model
 ON intelligence_monitor_plans(account_id,model)
 WHERE deleted_at IS NULL AND source_type='openai_oauth';
CREATE INDEX IF NOT EXISTS idx_intelligence_plans_group_model
 ON intelligence_monitor_plans(group_id,model)
 WHERE deleted_at IS NULL AND source_type='local_group';
