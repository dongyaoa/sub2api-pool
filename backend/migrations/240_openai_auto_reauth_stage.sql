-- The coarse job status retains lease semantics; stage describes actual work.
ALTER TABLE openai_auto_reauth ADD COLUMN IF NOT EXISTS stage TEXT NOT NULL DEFAULT 'idle';
ALTER TABLE openai_auto_reauth ADD COLUMN IF NOT EXISTS login_email TEXT NOT NULL DEFAULT '';
UPDATE openai_auto_reauth SET stage = CASE status
    WHEN 'pending' THEN 'queued' WHEN 'running' THEN 'starting' ELSE status END
WHERE stage = 'idle' AND status <> 'idle';
