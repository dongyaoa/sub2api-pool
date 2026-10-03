-- Purge historical OAuth monitors whose account was deleted. This includes
-- archived plans and orphaned references cleared by an account's foreign key.
-- Active worker completions use UPDATE, so they cannot recreate deleted rows.
DELETE FROM intelligence_monitor_runs r
USING intelligence_monitor_plans p
WHERE r.plan_id=p.id AND p.source_type='openai_oauth'
  AND NOT EXISTS (SELECT 1 FROM accounts a WHERE a.id=p.account_id AND a.deleted_at IS NULL);

DELETE FROM intelligence_monitor_plans p
WHERE p.source_type='openai_oauth'
  AND NOT EXISTS (SELECT 1 FROM accounts a WHERE a.id=p.account_id AND a.deleted_at IS NULL);
