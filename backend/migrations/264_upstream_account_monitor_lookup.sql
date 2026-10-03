-- Account dialogs resolve only their own credential identity, including
-- independent monitors, instead of reading and polling the full inventory.
CREATE INDEX IF NOT EXISTS idx_upstream_targets_account_monitor
    ON upstream_targets(api_key_fingerprint,provider)
    WHERE deleted_at IS NULL;
