package repository

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/Wei-Shaw/sub2api/migrations"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

// Optional real PostgreSQL regression: runs in its own random schema, never
// migrates or changes public. Set UPSTREAM_TEST_DATABASE_URL in the process.
func TestUpstreamFinancePostgresLedger(t *testing.T) {
	dsn := os.Getenv("UPSTREAM_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("UPSTREAM_TEST_DATABASE_URL is not configured")
	}
	db, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	db.SetMaxOpenConns(1)
	defer func() { _ = db.Close() }()
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	schema := "upstream_finance_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	_, err = db.ExecContext(ctx, `CREATE SCHEMA "`+schema+`"`)
	require.NoError(t, err)
	defer func() { _, _ = db.ExecContext(context.Background(), `DROP SCHEMA "`+schema+`" CASCADE`) }()
	_, err = db.ExecContext(ctx, `SET search_path TO "`+schema+`"`)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `CREATE TABLE accounts (id BIGINT PRIMARY KEY, credentials JSONB NOT NULL DEFAULT '{}', platform TEXT NOT NULL DEFAULT 'openai', type TEXT NOT NULL DEFAULT 'apikey', deleted_at TIMESTAMPTZ);
CREATE TABLE usage_logs (id BIGSERIAL PRIMARY KEY, created_at TIMESTAMPTZ NOT NULL, account_id BIGINT NOT NULL, group_id BIGINT, user_id BIGINT NOT NULL DEFAULT 1, api_key_id BIGINT NOT NULL DEFAULT 1, requested_model TEXT, model TEXT NOT NULL DEFAULT 'gpt-test', request_id TEXT, actual_cost NUMERIC NOT NULL DEFAULT 0, total_cost NUMERIC NOT NULL DEFAULT 0, account_stats_cost NUMERIC, account_rate_multiplier NUMERIC, billing_type SMALLINT NOT NULL DEFAULT 0, input_tokens INT NOT NULL DEFAULT 0, output_tokens INT NOT NULL DEFAULT 0, cache_creation_tokens INT NOT NULL DEFAULT 0, cache_read_tokens INT NOT NULL DEFAULT 0);`)
	require.NoError(t, err)
	for _, name := range []string{"242_upstream_center.sql", "243_upstream_finance.sql", "243_upstream_finance.sql", "244_upstream_remote_billing.sql", "244_upstream_remote_billing.sql", "247_upstream_finance_usage_totals.sql", "247_upstream_finance_usage_totals.sql"} {
		migration, err := migrations.FS.ReadFile(name)
		require.NoError(t, err)
		_, err = db.ExecContext(ctx, string(migration))
		require.NoError(t, err, name)
	}
	_, err = db.ExecContext(ctx, `INSERT INTO upstream_suppliers(id,name) VALUES(1,'First supplier'),(2,'Second supplier');
INSERT INTO upstream_targets(id,supplier_id,name,provider,endpoint,api_key_encrypted,api_key_fingerprint) VALUES
(1,1,'Old key','openai','https://example.com','cipher1','fingerprint1'),
(2,2,'New key','openai','https://example.org','cipher2','fingerprint2'),
(3,NULL,'Independent monitor','openai','https://example.net','cipher3','fingerprint3');
INSERT INTO accounts(id,credentials) VALUES(1,'{"api_key":"first","base_url":"https://example.com"}');
INSERT INTO upstream_account_bindings(target_id,account_id,supplier_id,target_name,supplier_name,valid_from,valid_until) VALUES
(1,1,1,'Old key','First supplier','2026-09-23T01:00:00Z','2026-09-23T02:00:00Z'),
(2,1,2,'New key','Second supplier','2026-09-23T02:00:00Z',NULL);
INSERT INTO usage_logs(id,created_at,account_id,actual_cost,total_cost,account_rate_multiplier,request_id) VALUES
(1,'2026-09-23T00:59:59Z',1,100,100,1,'before-binding'),
(2,'2026-09-23T01:00:00Z',1,5,4,0.5,'first'),
(3,'2026-09-23T02:00:00Z',1,10,4,0.5,'boundary');`)
	require.NoError(t, err)
	var count int
	require.NoError(t, db.QueryRowContext(ctx, `SELECT COUNT(*) FROM upstream_finance_ledger`).Scan(&count))
	require.Equal(t, 2, count)
	var supplier int64
	require.NoError(t, db.QueryRowContext(ctx, `SELECT supplier_id FROM upstream_finance_ledger WHERE usage_id=3`).Scan(&supplier))
	require.Equal(t, int64(2), supplier)
	// Monetary corrections and target ownership edits cannot rewrite attribution.
	_, err = db.ExecContext(ctx, `UPDATE upstream_targets SET supplier_id=2,name='Renamed' WHERE id=1;
UPDATE usage_logs SET actual_cost=9,account_stats_cost=3,account_rate_multiplier=2 WHERE id=2;
UPDATE usage_logs SET actual_cost=9 WHERE id=2;
INSERT INTO upstream_monitor_history(target_id,supplier_id,target_name,supplier_name,model,status,checked_at,cost,cost_source) VALUES
(1,1,'Old key','First supplier','gpt-test','operational','2026-09-23T01:30:00Z',0.2,'estimated'),
(3,NULL,'Independent monitor','','gpt-test','operational','2026-09-23T01:30:00Z',100,'estimated');`)
	require.NoError(t, err)
	repo := &upstreamFinanceRepository{db: db}
	from := time.Date(2026, 9, 23, 0, 0, 0, 0, time.UTC)
	q := service.UpstreamFinanceQuery{From: from, To: from.Add(24 * time.Hour), Page: 1, PageSize: 50}
	summary, err := repo.Summary(ctx, q)
	require.NoError(t, err)
	require.Equal(t, int64(2), summary.RequestCount)
	require.InDelta(t, 19, summary.Revenue, 1e-9)
	require.InDelta(t, 8, summary.BusinessCost, 1e-9)
	require.InDelta(t, 8, summary.AccountBilled, 1e-9)
	require.NotNil(t, summary.TotalTokens)
	require.Zero(t, *summary.TotalTokens)
	require.NotNil(t, summary.Profit)
	require.InDelta(t, 10.8, *summary.Profit, 1e-9, "independent monitor must not pollute supplier profit")
	one := int64(1)
	q.SupplierID = &one
	summary, err = repo.Summary(ctx, q)
	require.NoError(t, err)
	require.InDelta(t, 9, summary.Revenue, 1e-9)
	require.InDelta(t, 2.8, *summary.Profit, 1e-9)
	// Usage retention and hard account deletion leave the financial ledger intact.
	_, err = db.ExecContext(ctx, `UPDATE accounts SET credentials='{"api_key":"changed","base_url":"https://example.com"}' WHERE id=1;
DELETE FROM usage_logs; DELETE FROM accounts WHERE id=1;`)
	require.NoError(t, err)
	require.NoError(t, db.QueryRowContext(ctx, `SELECT COUNT(*) FROM upstream_account_bindings WHERE valid_until IS NULL`).Scan(&count))
	require.Zero(t, count)
	rows, total, err := repo.Details(ctx, q)
	require.NoError(t, err)
	require.Equal(t, int64(1), total)
	require.Len(t, rows, 1)
	require.Equal(t, "Old key", rows[0].TargetName)
	require.Equal(t, "First supplier", rows[0].SupplierName)
	require.InDelta(t, 6, rows[0].BusinessCost, 1e-9)
	// Missing monitor cost must make combined profit unknown, not silently zero.
	_, err = db.ExecContext(ctx, `INSERT INTO upstream_monitor_history(target_id,supplier_id,target_name,supplier_name,model,status,checked_at) VALUES(1,1,'Old key','First supplier','unknown','error','2026-09-23T01:31:00Z')`)
	require.NoError(t, err)
	summary, err = repo.Summary(ctx, q)
	require.NoError(t, err)
	require.Nil(t, summary.Profit)
	require.Nil(t, summary.MonitorCost)
	require.Equal(t, int64(1), summary.UnpricedMonitorCount)

	// Sync claims are exclusive, old credentials cannot persist a new observation.
	now := time.Now().UTC()
	claimed, err := repo.ClaimBalance(ctx, 1, "first-token", now, now.Add(time.Minute))
	require.NoError(t, err)
	require.True(t, claimed)
	claimed, err = repo.ClaimBalance(ctx, 1, "second-token", now, now.Add(time.Minute))
	require.NoError(t, err)
	require.False(t, claimed)
	target, err := repo.GetTarget(ctx, 1)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `UPDATE upstream_targets SET api_key_encrypted='newcipher' WHERE id=1`)
	require.NoError(t, err)
	err = repo.SaveBalance(ctx, target, "identity", &service.UpstreamBalanceSnapshot{Kind: "wallet", Status: "ok", SyncedAt: &now}, "first-token", now.Add(time.Minute))
	require.ErrorIs(t, err, service.ErrUpstreamFinanceIdentityChanged)
	var snapshots int
	require.NoError(t, db.QueryRowContext(ctx, `SELECT COUNT(*) FROM upstream_balance_snapshots`).Scan(&snapshots))
	require.Zero(t, snapshots)
	// A failed refresh retains the last successful amounts with their original
	// timestamp, while exposing the newer failure and its attempt timestamp.
	target, err = repo.GetTarget(ctx, 1)
	require.NoError(t, err)
	balance := 12.5
	rate := .3
	billing := &service.UpstreamRemoteBillingSnapshot{Status: "ok", Source: "sub2api_billing", BillingScope: "token", GroupRateMultiplier: &rate, ResolvedRateMultiplier: &rate, EffectiveRateMultiplier: &rate, SyncedAt: &now, LastAttemptAt: &now, ObservedAt: &now}
	err = repo.SaveBalance(ctx, target, "current-identity", &service.UpstreamBalanceSnapshot{Kind: "wallet", Status: "ok", Balance: &balance, Currency: "USD", CurrencySource: "reported", SyncedAt: &now, Billing: billing}, "first-token", now.Add(time.Minute))
	require.NoError(t, err)
	later := now.Add(time.Minute)
	claimed, err = repo.ClaimBalance(ctx, 1, "failure-token", later, later.Add(time.Minute))
	require.NoError(t, err)
	require.True(t, claimed)
	err = repo.SaveBalance(ctx, target, "current-identity", &service.UpstreamBalanceSnapshot{Kind: "unknown", Status: "error", Error: "upstream_request_failed", SyncedAt: &later, Billing: &service.UpstreamRemoteBillingSnapshot{Status: "error", Source: "sub2api_billing", Error: "upstream_http_503", LastAttemptAt: &later}}, "failure-token", later.Add(time.Minute))
	require.NoError(t, err)
	view, err := repo.LatestBalance(ctx, 1, "current-identity")
	require.NoError(t, err)
	require.NotNil(t, view.Balance)
	require.InDelta(t, 12.5, *view.Balance, 1e-9)
	require.Equal(t, "error", view.Status)
	require.Equal(t, "wallet", view.Kind)
	require.WithinDuration(t, now, *view.SyncedAt, time.Microsecond)
	require.WithinDuration(t, later, *view.LastAttemptAt, time.Microsecond)
	require.NotNil(t, view.Billing)
	require.Equal(t, "error", view.Billing.Status)
	require.True(t, view.Billing.Stale)
	require.Equal(t, "upstream_http_503", view.Billing.Error)
	require.NotNil(t, view.Billing.EffectiveRateMultiplier)
	require.InDelta(t, .3, *view.Billing.EffectiveRateMultiplier, 1e-10)
	require.WithinDuration(t, now, *view.Billing.SyncedAt, time.Microsecond)
	require.WithinDuration(t, later, *view.Billing.LastAttemptAt, time.Microsecond)
	changedAt := later.Add(time.Minute)
	claimed, err = repo.ClaimBalance(ctx, 1, "rate-zero-token", changedAt, changedAt.Add(time.Minute))
	require.NoError(t, err)
	require.True(t, claimed)
	rate = 0
	billing.SyncedAt = &changedAt
	billing.LastAttemptAt = &changedAt
	billing.ObservedAt = &changedAt
	err = repo.SaveBalance(ctx, target, "current-identity", &service.UpstreamBalanceSnapshot{Kind: "wallet", Status: "ok", Balance: &balance, SyncedAt: &changedAt, Billing: billing}, "rate-zero-token", changedAt.Add(time.Minute))
	require.NoError(t, err)
	view, err = repo.LatestBalance(ctx, 1, "current-identity")
	require.NoError(t, err)
	require.Equal(t, "ok", view.Billing.Status)
	require.NotNil(t, view.Billing.EffectiveRateMultiplier)
	require.Zero(t, *view.Billing.EffectiveRateMultiplier)
	missing, err := repo.LatestBalance(ctx, 1, "unrelated-new-key")
	require.NoError(t, err)
	require.Nil(t, missing, "a replacement key must never inherit old wallet amounts")

	t.Run("token snapshots preserve unknown history and account billing", func(t *testing.T) {
		legacy, err := migrations.FS.ReadFile("243_upstream_finance.sql")
		require.NoError(t, err)
		_, err = db.ExecContext(ctx, string(legacy))
		require.NoError(t, err)
		at := time.Now().UTC().Truncate(time.Microsecond)
		_, err = db.ExecContext(ctx, `INSERT INTO accounts(id) VALUES(101);
INSERT INTO upstream_account_bindings(target_id,account_id,supplier_id,target_name,supplier_name,valid_from) VALUES(2,101,2,'New key','Second supplier','2020-01-01');`)
		require.NoError(t, err)
		_, err = db.ExecContext(ctx, `INSERT INTO usage_logs(id,created_at,account_id,actual_cost,total_cost,account_stats_cost,account_rate_multiplier,input_tokens,output_tokens,cache_creation_tokens,cache_read_tokens) VALUES
(101,$1,101,99,20,8,0.5,10,20,30,40),
(102,$1,101,77,6,NULL,0.5,100,200,300,400)`, at)
		require.NoError(t, err)
		_, err = db.ExecContext(ctx, `DELETE FROM usage_logs WHERE id=102`)
		require.NoError(t, err)
		migration, err := migrations.FS.ReadFile("247_upstream_finance_usage_totals.sql")
		require.NoError(t, err)
		for range 2 {
			_, err = db.ExecContext(ctx, string(migration))
			require.NoError(t, err)
		}
		targetID := int64(2)
		query := service.UpstreamFinanceQuery{TargetID: &targetID, From: at.Add(-time.Second), To: at.Add(time.Second), Page: 1, PageSize: 50}
		value, err := repo.Summary(ctx, query)
		require.NoError(t, err)
		require.Nil(t, value.TotalTokens, "a partial known sum must not be reported as the complete total")
		require.Equal(t, int64(1), value.UnknownTokenRequests)
		require.Equal(t, float64(7), value.AccountBilled, "account billing is 8*.5 + 6*.5, not the user debit of 176")
		items, _, err := repo.Details(ctx, query)
		require.NoError(t, err)
		require.Len(t, items, 2)
		require.Nil(t, items[0].TotalTokens)
		require.Equal(t, int64(100), *items[1].TotalTokens)
		_, err = db.ExecContext(ctx, `UPDATE usage_logs SET input_tokens=110,account_stats_cost=10 WHERE id=101`)
		require.NoError(t, err)
		_, err = db.ExecContext(ctx, `INSERT INTO usage_logs(id,created_at,account_id,actual_cost,total_cost,account_stats_cost,account_rate_multiplier,input_tokens,output_tokens,cache_creation_tokens,cache_read_tokens) VALUES(103,$1,101,100,10,5,0,1,2,3,4)`, at.Add(10*time.Second))
		require.NoError(t, err)
		items, _, err = repo.Details(ctx, query)
		require.NoError(t, err)
		require.Equal(t, int64(200), *items[1].TotalTokens)
		require.Equal(t, float64(5), items[1].AccountBilled)
		query.From, query.To = at.Add(9*time.Second), at.Add(11*time.Second)
		value, err = repo.Summary(ctx, query)
		require.NoError(t, err)
		require.Equal(t, int64(10), *value.TotalTokens)
		require.Zero(t, value.AccountBilled, "a zero account multiplier remains zero")
		_, err = db.ExecContext(ctx, `UPDATE usage_logs SET cache_read_tokens=14 WHERE id=103; DELETE FROM usage_logs WHERE id IN(101,103); DELETE FROM accounts WHERE id=101`)
		require.NoError(t, err)
		value, err = repo.Summary(ctx, query)
		require.NoError(t, err)
		require.Equal(t, int64(20), *value.TotalTokens, "token-only corrections persist after usage/account deletion")
		query.From, query.To = at.Add(20*time.Second), at.Add(21*time.Second)
		value, err = repo.Summary(ctx, query)
		require.NoError(t, err)
		require.NotNil(t, value.TotalTokens)
		require.Zero(t, *value.TotalTokens)
		require.Zero(t, value.AccountBilled)
	})

	t.Run("statistics use fixed seven days and sixty latest samples", func(t *testing.T) {
		_, err := db.ExecContext(ctx, `UPDATE upstream_targets SET models='["recent","old","missing","null-latency"]'::jsonb WHERE id=3;
INSERT INTO upstream_monitor_history(target_id,target_name,model,status,latency_ms,checked_at)
SELECT 3,'Independent monitor','recent',CASE WHEN n=61 THEN 'error' ELSE 'operational' END,n,NOW()-make_interval(mins=>62-n) FROM generate_series(1,61) n;
INSERT INTO upstream_monitor_history(target_id,target_name,model,status,latency_ms,checked_at) VALUES
(3,'Independent monitor','recent','failed',900,NOW()-INTERVAL '3 days'),
(3,'Independent monitor','recent','operational',800,NOW()-INTERVAL '8 days'),
(3,'Independent monitor','old','operational',700,NOW()-INTERVAL '10 days'),
(3,'Independent monitor','null-latency','operational',100,NOW()-INTERVAL '2 minutes'),
(3,'Independent monitor','null-latency','error',NULL,NOW()-INTERVAL '1 minute');`)
		require.NoError(t, err)
		center := &upstreamCenterRepository{db: db}
		target := &service.UpstreamTarget{ID: 3, Models: []string{"recent", "old", "missing", "null-latency"}}
		require.NoError(t, center.PopulateStatistics(ctx, []*service.UpstreamTarget{target}, time.Now().Add(-24*time.Hour)))
		recent := target.Statistics[0]
		require.Equal(t, int64(61), recent.SampleCount)
		require.InDelta(t, 60.0/61*100, *recent.Availability, 1e-9)
		require.InDelta(t, 60.0/62*100, *recent.Availability7d, 1e-9)
		require.Len(t, recent.Timeline, 60)
		require.Equal(t, 61, *recent.LatestLatencyMs)
		require.Equal(t, "error", recent.Status)
		require.Equal(t, 2, *recent.Timeline[0].LatencyMs)
		require.NotNil(t, recent.LastCheckedAt)
		require.Nil(t, target.Statistics[1].Availability7d)
		require.Nil(t, target.Statistics[2].Availability7d)
		require.Nil(t, target.Statistics[2].LatestLatencyMs)
		require.Nil(t, target.Statistics[3].LatestLatencyMs, "do not reuse the older successful latency")
		require.NoError(t, center.PopulateStatistics(ctx, []*service.UpstreamTarget{target}, time.Now().Add(-30*24*time.Hour)))
		recent = target.Statistics[0]
		require.Equal(t, int64(63), recent.SampleCount)
		require.InDelta(t, 60.0/62*100, *recent.Availability7d, 1e-9, "fixed metric must ignore the selected 30-day window")
	})

	t.Run("identity edits queue an immediate sync", func(t *testing.T) {
		center := &upstreamCenterRepository{db: db}
		edited := &service.UpstreamTarget{
			Name: "Sync schedule", Provider: "openai", APIMode: "chat_completions",
			Endpoint: "https://schedule.example", APIKeyEncrypted: "schedule-key",
			APIKeyFingerprint: "schedule-fingerprint", Models: []string{}, AccountIDs: []int64{},
			IntervalSeconds: 300, TimeoutSeconds: 45, DegradedThresholdMs: 6000, WalletRef: "default",
		}
		// The earlier fixture uses explicit IDs; advance its sequence for creation.
		_, err = db.ExecContext(ctx, `SELECT setval(pg_get_serial_sequence('upstream_targets','id'),(SELECT MAX(id) FROM upstream_targets))`)
		require.NoError(t, err)
		require.NoError(t, center.SaveTarget(ctx, edited))
		var scheduled time.Time
		require.NoError(t, db.QueryRowContext(ctx, `SELECT balance_next_sync_at FROM upstream_targets WHERE id=$1`, edited.ID).Scan(&scheduled))
		require.WithinDuration(t, time.Now(), scheduled, 5*time.Second)
		tests := []struct {
			name   string
			change func()
			reset  bool
		}{
			{"rename", func() { edited.Name = "Renamed schedule" }, false},
			{"notes", func() { edited.Notes = "New note" }, false},
			{"endpoint", func() { edited.Endpoint = "https://new-schedule.example" }, true},
			{"key", func() { edited.APIKeyEncrypted = "new-schedule-key" }, true},
			{"provider", func() { edited.Provider = "anthropic" }, true},
			{"wallet", func() { edited.WalletRef = "second-wallet" }, true},
			{"attach supplier", func() { edited.SupplierID = &one }, true},
			{"same identity", func() {}, false},
			{"detach supplier", func() { edited.SupplierID = nil }, true},
		}
		for _, test := range tests {
			t.Run(test.name, func(t *testing.T) {
				future := time.Now().UTC().Add(time.Hour).Truncate(time.Microsecond)
				_, err = db.ExecContext(ctx, `UPDATE upstream_targets SET balance_next_sync_at=$2 WHERE id=$1`, edited.ID, future)
				require.NoError(t, err)
				test.change()
				require.NoError(t, center.SaveTarget(ctx, edited))
				require.NoError(t, db.QueryRowContext(ctx, `SELECT balance_next_sync_at FROM upstream_targets WHERE id=$1`, edited.ID).Scan(&scheduled))
				if test.reset {
					require.WithinDuration(t, time.Now(), scheduled, 5*time.Second)
				} else {
					require.True(t, scheduled.Equal(future), "non-identity edit must retain the balance schedule")
				}
			})
		}
	})

	t.Run("ordinary monitor defaults preserve existing settings and schedule thirty seconds", func(t *testing.T) {
		_, err := db.ExecContext(ctx, `UPDATE upstream_targets SET models='["existing-model"]'::jsonb,interval_seconds=300 WHERE id=3`)
		require.NoError(t, err)
		migration, err := migrations.FS.ReadFile("248_upstream_monitor_defaults.sql")
		require.NoError(t, err)
		for range 2 {
			_, err = db.ExecContext(ctx, string(migration))
			require.NoError(t, err)
		}
		var models string
		var interval int
		require.NoError(t, db.QueryRowContext(ctx, `SELECT models::text,interval_seconds FROM upstream_targets WHERE id=3`).Scan(&models, &interval))
		require.JSONEq(t, `["existing-model"]`, models)
		require.Equal(t, 300, interval)
		var id int64
		require.NoError(t, db.QueryRowContext(ctx, `INSERT INTO upstream_targets(name,provider,endpoint,api_key_encrypted,api_key_fingerprint) VALUES('Default monitor','openai','https://defaults.example','test-cipher','test-fingerprint') RETURNING id,models::text,interval_seconds`).Scan(&id, &models, &interval))
		require.JSONEq(t, `["gpt-5.6-sol"]`, models)
		require.Equal(t, 30, interval)
		for _, seconds := range []int{29, 3601} {
			_, err = db.ExecContext(ctx, `UPDATE upstream_targets SET interval_seconds=$2 WHERE id=$1`, id, seconds)
			require.Error(t, err, "the database must reject interval %d", seconds)
		}
		for _, seconds := range []int{3600, 30} {
			_, err = db.ExecContext(ctx, `UPDATE upstream_targets SET interval_seconds=$2 WHERE id=$1`, id, seconds)
			require.NoError(t, err)
		}
		center := &upstreamCenterRepository{db: db}
		claimed, err := center.ClaimCheck(ctx, id, "first-thirty-second-check", false)
		require.NoError(t, err)
		require.True(t, claimed)
		overlapped, err := center.ClaimCheck(ctx, id, "overlapping-check", true)
		require.NoError(t, err)
		require.False(t, overlapped, "even a manual check must respect the active lease")
		completed, err := center.CompleteCheck(ctx, id, "first-thirty-second-check", []*service.UpstreamHistoryRecord{{Model: "gpt-5.6-sol", Status: "operational", CheckedAt: time.Now(), CostSource: "unknown"}})
		require.NoError(t, err)
		require.True(t, completed)
		var last, next time.Time
		require.NoError(t, db.QueryRowContext(ctx, `SELECT last_checked_at,next_check_at FROM upstream_targets WHERE id=$1`, id).Scan(&last, &next))
		require.Equal(t, 30*time.Second, next.Sub(last))
		claimed, err = center.ClaimCheck(ctx, id, "too-early-check", false)
		require.NoError(t, err)
		require.False(t, claimed, "completion must schedule the next check rather than immediately repeat")
		_, err = db.ExecContext(ctx, `UPDATE upstream_targets SET next_check_at=NOW()-INTERVAL '1 second' WHERE id=$1`, id)
		require.NoError(t, err)
		claimed, err = center.ClaimCheck(ctx, id, "next-thirty-second-check", false)
		require.NoError(t, err)
		require.True(t, claimed)
		require.NoError(t, center.ReleaseCheck(ctx, id, "next-thirty-second-check"))
		require.NoError(t, db.QueryRowContext(ctx, `SELECT next_check_at FROM upstream_targets WHERE id=$1`, id).Scan(&next))
		require.WithinDuration(t, time.Now().Add(30*time.Second), next, 2*time.Second)
	})

	t.Run("balance scheduler includes independent monitors and respects leases and archives", func(t *testing.T) {
		now := time.Now().UTC().Truncate(time.Microsecond)
		_, err := db.ExecContext(ctx, `UPDATE upstream_targets SET balance_next_sync_at=$1`, now.Add(time.Hour))
		require.NoError(t, err)
		_, err = db.ExecContext(ctx, `SELECT setval(pg_get_serial_sequence('upstream_suppliers','id'),(SELECT MAX(id) FROM upstream_suppliers))`)
		require.NoError(t, err)
		var liveSupplier, archivedSupplier int64
		require.NoError(t, db.QueryRowContext(ctx, `INSERT INTO upstream_suppliers(name) VALUES('Live billing supplier') RETURNING id`).Scan(&liveSupplier))
		require.NoError(t, db.QueryRowContext(ctx, `INSERT INTO upstream_suppliers(name,deleted_at) VALUES('Archived billing supplier',NOW()) RETURNING id`).Scan(&archivedSupplier))
		insert := func(supplier *int64, name string, lease, deleted *time.Time) int64 {
			var id int64
			require.NoError(t, db.QueryRowContext(ctx, `INSERT INTO upstream_targets(supplier_id,name,provider,endpoint,api_key_encrypted,api_key_fingerprint,balance_lease_until,deleted_at) VALUES($1,$2,'openai','https://billing.example','test-cipher',$2,$3,$4) RETURNING id`, supplier, name, lease, deleted).Scan(&id))
			return id
		}
		independent := insert(nil, "Independent initial billing", nil, nil)
		group := insert(&liveSupplier, "Supplier initial billing", nil, nil)
		future := now.Add(time.Hour)
		insert(nil, "Leased independent billing", &future, nil)
		insert(&archivedSupplier, "Archived supplier billing", nil, nil)
		insert(nil, "Archived independent billing", nil, &now)
		firstPoll := time.Now().UTC()
		ids, err := repo.DueBalanceTargetIDs(ctx, firstPoll, 32)
		require.NoError(t, err)
		require.ElementsMatch(t, []int64{independent, group}, ids)
		claimed, err := repo.ClaimBalance(ctx, independent, "independent-sync", firstPoll, firstPoll.Add(time.Minute))
		require.NoError(t, err)
		require.True(t, claimed)
		ids, err = repo.DueBalanceTargetIDs(ctx, firstPoll, 32)
		require.NoError(t, err)
		require.Equal(t, []int64{group}, ids)
		next := firstPoll.Add(time.Minute)
		require.NoError(t, repo.ReleaseBalance(ctx, independent, "independent-sync", next))
		ids, err = repo.DueBalanceTargetIDs(ctx, next.Add(-time.Microsecond), 32)
		require.NoError(t, err)
		require.NotContains(t, ids, independent)
		ids, err = repo.DueBalanceTargetIDs(ctx, next, 32)
		require.NoError(t, err)
		require.ElementsMatch(t, []int64{independent, group}, ids, "independent monitors must participate in periodic billing refresh")
	})
}
