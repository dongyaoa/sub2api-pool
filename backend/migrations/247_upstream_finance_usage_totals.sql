-- Preserve request usage after usage-log retention. Missing historical token
-- observations remain NULL rather than becoming fabricated zero-token calls.
ALTER TABLE upstream_finance_ledger ADD COLUMN IF NOT EXISTS total_tokens BIGINT;
UPDATE upstream_finance_ledger l SET
    total_tokens=u.input_tokens::bigint+u.output_tokens::bigint+u.cache_creation_tokens::bigint+u.cache_read_tokens::bigint
FROM usage_logs u
WHERE l.usage_id=u.id AND l.created_at=u.created_at AND l.total_tokens IS NULL;

CREATE OR REPLACE FUNCTION upstream_record_usage_finance()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE changed INTEGER;
BEGIN
    IF TG_OP = 'UPDATE' THEN
        -- Monetary/token corrections retain the originally captured attribution.
        UPDATE upstream_finance_ledger SET
            revenue = NEW.actual_cost,
            business_cost = COALESCE(NEW.account_stats_cost, NEW.total_cost) * COALESCE(NEW.account_rate_multiplier, 1),
            total_tokens = NEW.input_tokens::bigint+NEW.output_tokens::bigint+NEW.cache_creation_tokens::bigint+NEW.cache_read_tokens::bigint,
            model = COALESCE(NULLIF(NEW.requested_model, ''), NEW.model),
            request_id = COALESCE(NEW.request_id, ''),
            billing_type = NEW.billing_type,
            updated_at = clock_timestamp()
        WHERE usage_id = OLD.id AND created_at = OLD.created_at;
        GET DIAGNOSTICS changed = ROW_COUNT;
        IF changed > 0 THEN RETURN NEW; END IF;
    END IF;
    INSERT INTO upstream_finance_ledger
        (usage_id, created_at, target_id, target_name, supplier_id, supplier_name,
         account_id, group_id, user_id, api_key_id, model, request_id, revenue, business_cost, billing_type, total_tokens)
    SELECT NEW.id, NEW.created_at, b.target_id, b.target_name, b.supplier_id, b.supplier_name,
        NEW.account_id, NEW.group_id, NEW.user_id, NEW.api_key_id,
        COALESCE(NULLIF(NEW.requested_model, ''), NEW.model), COALESCE(NEW.request_id, ''),
        NEW.actual_cost,
        COALESCE(NEW.account_stats_cost, NEW.total_cost) * COALESCE(NEW.account_rate_multiplier, 1), NEW.billing_type,
        NEW.input_tokens::bigint+NEW.output_tokens::bigint+NEW.cache_creation_tokens::bigint+NEW.cache_read_tokens::bigint
    FROM upstream_account_bindings b
    WHERE b.account_id = NEW.account_id AND NEW.created_at >= b.valid_from
      AND (b.valid_until IS NULL OR NEW.created_at < b.valid_until)
    ORDER BY b.valid_from DESC, b.id DESC LIMIT 1
    ON CONFLICT (usage_id, created_at) DO UPDATE SET
        revenue = EXCLUDED.revenue, business_cost = EXCLUDED.business_cost,
        total_tokens = EXCLUDED.total_tokens,
        model = EXCLUDED.model, request_id = EXCLUDED.request_id,
        billing_type = EXCLUDED.billing_type, updated_at = clock_timestamp();
    RETURN NEW;
END;
$$;
DROP TRIGGER IF EXISTS upstream_usage_finance_snapshot ON usage_logs;
CREATE TRIGGER upstream_usage_finance_snapshot
    AFTER INSERT OR UPDATE OF actual_cost, total_cost, account_stats_cost, account_rate_multiplier,
        input_tokens, output_tokens, cache_creation_tokens, cache_read_tokens,
        requested_model, model, request_id, billing_type ON usage_logs
    FOR EACH ROW EXECUTE FUNCTION upstream_record_usage_finance();
