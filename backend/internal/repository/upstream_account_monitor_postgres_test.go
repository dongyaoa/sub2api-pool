package repository

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/Wei-Shaw/sub2api/migrations"
	"github.com/stretchr/testify/require"
)

func accountMonitorTestDB(t *testing.T) (*sql.DB, context.Context, func() *sql.DB) {
	t.Helper()
	db, ctx, newDB := manualOrderTestDB(t)
	applyManualOrderMigration(t, db, ctx)
	_, err := db.ExecContext(ctx, `ALTER TABLE accounts ADD COLUMN platform TEXT NOT NULL DEFAULT 'openai';
ALTER TABLE accounts ADD COLUMN parent_account_id BIGINT;
ALTER TABLE accounts ADD COLUMN extra JSONB NOT NULL DEFAULT '{}';
SELECT setval('upstream_suppliers_id_seq',100);
SELECT setval('upstream_targets_id_seq',100);
INSERT INTO accounts(id,credentials) VALUES(70,'{"api_key":"account-key","base_url":"https://example.net"}'),(71,'{"api_key":"account-key","base_url":"https://example.net"}')`)
	require.NoError(t, err)
	for _, name := range []string{"248_upstream_monitor_defaults.sql", "264_upstream_account_monitor_lookup.sql", "264_upstream_account_monitor_lookup.sql"} {
		migration, err := migrations.FS.ReadFile(name)
		require.NoError(t, err)
		_, err = db.ExecContext(ctx, string(migration))
		require.NoError(t, err)
	}
	return db, ctx, newDB
}

func accountMonitorPGIdentity() service.UpstreamAccountMonitorIdentity {
	fingerprint := sha256.Sum256([]byte("https://example.net\x00account-key"))
	return service.UpstreamAccountMonitorIdentity{AccountID: 70, AccountName: "Account seventy", Provider: "openai", Endpoint: "https://example.net",
		Fingerprint: hex.EncodeToString(fingerprint[:]), Credentials: service.UpstreamBindingCredential{APIKey: "account-key", BaseURL: "https://example.net"}}
}

func accountMonitorPGCandidate(identity service.UpstreamAccountMonitorIdentity) *service.UpstreamTarget {
	return &service.UpstreamTarget{Name: identity.AccountName, Provider: identity.Provider, Endpoint: identity.Endpoint, APIKeyFingerprint: identity.Fingerprint,
		APIKeyEncrypted: "encrypted:key", APIMode: "chat_completions", Models: []string{"gpt-5.6-sol"}, Enabled: false, IntervalSeconds: 30, TimeoutSeconds: 45, DegradedThresholdMs: 6000, WalletRef: "default"}
}

func TestUpstreamAccountMonitorPostgresAtomicConcurrentCreation(t *testing.T) {
	db, ctx, newDB := accountMonitorTestDB(t)
	identity := accountMonitorPGIdentity()
	first, second := &upstreamCenterRepository{db: newDB()}, &upstreamCenterRepository{db: newDB()}
	start := make(chan struct{})
	type result struct {
		target *service.UpstreamTarget
		err    error
	}
	results := make(chan result, 2)
	for _, repo := range []*upstreamCenterRepository{first, second} {
		go func(r *upstreamCenterRepository) {
			<-start
			target, err := r.EnsureAccountMonitor(ctx, identity, accountMonitorPGCandidate(identity))
			results <- result{target, err}
		}(repo)
	}
	close(start)
	a, b := <-results, <-results
	require.NoError(t, a.err)
	require.NoError(t, b.err)
	require.Equal(t, a.target.ID, b.target.ID)
	require.Equal(t, a.target.SupplierID, b.target.SupplierID)
	require.False(t, a.target.Enabled)
	require.Nil(t, a.target.NextCheckAt)
	require.Equal(t, []int64{70}, a.target.AccountIDs)
	var count int
	require.NoError(t, db.QueryRowContext(ctx, `SELECT COUNT(*) FROM upstream_targets WHERE api_key_fingerprint=$1`, identity.Fingerprint).Scan(&count))
	require.Equal(t, 1, count)
	require.NoError(t, db.QueryRowContext(ctx, `SELECT COUNT(*) FROM upstream_suppliers WHERE name=$1`, identity.AccountName).Scan(&count))
	require.Equal(t, 1, count)
	// A second account forwarding exactly the same key attaches to this target.
	identity.AccountID = 71
	matched, err := first.EnsureAccountMonitor(ctx, identity, accountMonitorPGCandidate(identity))
	require.NoError(t, err)
	require.Equal(t, a.target.ID, matched.ID)
	require.Equal(t, []int64{70, 71}, matched.AccountIDs)
	require.NoError(t, db.QueryRowContext(ctx, `SELECT COUNT(*) FROM intelligence_monitor_plans WHERE source_type='upstream'`).Scan(&count))
	require.Zero(t, count, "ensuring inventory does not create or launch an intelligence plan")
}

func TestUpstreamAccountMonitorPostgresReuseBindingAndIndependent(t *testing.T) {
	db, ctx, _ := accountMonitorTestDB(t)
	repo := &upstreamCenterRepository{db: db}
	identity := accountMonitorPGIdentity()
	_, err := db.ExecContext(ctx, `UPDATE upstream_targets SET api_key_fingerprint=$1,endpoint=$2 WHERE id=30`, identity.Fingerprint, identity.Endpoint)
	require.NoError(t, err)
	target, err := repo.FindAccountMonitor(ctx, identity)
	require.NoError(t, err)
	require.Equal(t, int64(30), target.ID)
	target, err = repo.EnsureAccountMonitor(ctx, identity, nil)
	require.NoError(t, err)
	require.Nil(t, target.SupplierID)
	require.Empty(t, target.AccountIDs, "independent checks are not silently converted into procurement groups")
	var count int
	require.NoError(t, db.QueryRowContext(ctx, `SELECT COUNT(*) FROM upstream_suppliers`).Scan(&count))
	require.Equal(t, 2, count)
	// A supplier group takes precedence over an unbound independent copy.
	_, err = db.ExecContext(ctx, `UPDATE upstream_targets SET api_key_fingerprint=$1,endpoint=$2 WHERE id=10`, identity.Fingerprint, identity.Endpoint)
	require.NoError(t, err)
	target, err = repo.EnsureAccountMonitor(ctx, identity, nil)
	require.NoError(t, err)
	require.Equal(t, int64(10), target.ID)
	require.Equal(t, []int64{70}, target.AccountIDs)
	// An explicit, valid binding wins even when the other target is a supplier.
	_, err = db.ExecContext(ctx, `UPDATE upstream_account_bindings SET target_id=30 WHERE account_id=70`)
	require.NoError(t, err)
	target, err = repo.FindAccountMonitor(ctx, identity)
	require.NoError(t, err)
	require.Equal(t, int64(30), target.ID)
	// Archived suppliers and deleted targets are not resurrected.
	_, err = db.ExecContext(ctx, `UPDATE upstream_account_bindings SET valid_until=NOW(); UPDATE upstream_suppliers SET deleted_at=NOW() WHERE id=1; UPDATE upstream_targets SET deleted_at=NOW() WHERE id=30`)
	require.NoError(t, err)
	target, err = repo.FindAccountMonitor(ctx, identity)
	require.NoError(t, err)
	require.Nil(t, target)
}

func TestUpstreamAccountMonitorPostgresRejectsAmbiguityAndCredentialRaces(t *testing.T) {
	db, ctx, _ := accountMonitorTestDB(t)
	repo := &upstreamCenterRepository{db: db}
	identity := accountMonitorPGIdentity()
	_, err := db.ExecContext(ctx, `UPDATE upstream_targets SET api_key_fingerprint=$1,endpoint=$2 WHERE id IN(30,31)`, identity.Fingerprint, identity.Endpoint)
	require.NoError(t, err)
	_, err = repo.FindAccountMonitor(ctx, identity)
	require.ErrorIs(t, err, service.ErrUpstreamAccountMonitorAmbiguous)
	_, err = repo.EnsureAccountMonitor(ctx, identity, accountMonitorPGCandidate(identity))
	require.ErrorIs(t, err, service.ErrUpstreamAccountMonitorAmbiguous)
	_, err = db.ExecContext(ctx, `UPDATE upstream_targets SET deleted_at=NOW() WHERE id IN(30,31)`)
	require.NoError(t, err)
	for _, query := range []string{
		`UPDATE accounts SET credentials=jsonb_set(credentials,'{api_key}','"changed"') WHERE id=70`,
		`UPDATE accounts SET credentials=jsonb_set(credentials,'{base_url}','"https://changed.example"') WHERE id=70`,
		`UPDATE accounts SET platform='anthropic' WHERE id=70`,
		`UPDATE accounts SET type='oauth' WHERE id=70`,
		`UPDATE accounts SET parent_account_id=1 WHERE id=70`,
		`UPDATE accounts SET extra='{"synthetic_ui_test":true}' WHERE id=70`,
		`UPDATE accounts SET deleted_at=NOW() WHERE id=70`,
	} {
		_, err = db.ExecContext(ctx, `UPDATE accounts SET credentials='{"api_key":"account-key","base_url":"https://example.net"}',platform='openai',type='apikey',parent_account_id=NULL,extra='{}',deleted_at=NULL WHERE id=70`)
		require.NoError(t, err)
		_, err = db.ExecContext(ctx, query)
		require.NoError(t, err)
		_, err = repo.EnsureAccountMonitor(ctx, identity, accountMonitorPGCandidate(identity))
		require.ErrorIs(t, err, service.ErrUpstreamBindingConflict, query)
	}
	var count int
	require.NoError(t, db.QueryRowContext(ctx, `SELECT COUNT(*) FROM upstream_suppliers WHERE name=$1`, identity.AccountName).Scan(&count))
	require.Zero(t, count, "rejected writes cannot leave empty suppliers")
}
