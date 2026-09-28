-- Keep lightweight comparison summaries in list reads, full evidence in detail.
-- Historical runs have no collected fingerprint and must not imply a pass.
ALTER TABLE intelligence_monitor_runs ADD COLUMN IF NOT EXISTS fingerprint JSONB NOT NULL DEFAULT '{}'::jsonb;
ALTER TABLE intelligence_monitor_runs ADD COLUMN IF NOT EXISTS fingerprint_detail JSONB NOT NULL DEFAULT '{}'::jsonb;
