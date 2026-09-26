package repository

import (
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestUpstreamOverviewFinancePostgresMatchesIndividualReads(t *testing.T) {
	db, ctx, _ := upstreamStorageTestDB(t)
	_, err := db.ExecContext(ctx, `UPDATE upstream_targets SET supplier_id=2 WHERE id=1;
INSERT INTO upstream_suppliers(id,name) VALUES(4,'No transactions');
INSERT INTO upstream_targets(id,supplier_id,name,provider,endpoint,api_key_encrypted,api_key_fingerprint,newapi_user_id,newapi_access_token_encrypted) VALUES
(4,4,'Pending identity','openai','https://pending.example','newkey','newkey',9,'console-new');
INSERT INTO upstream_finance_ledger(usage_id,created_at,target_id,target_name,supplier_id,account_id,user_id,api_key_id,model,revenue,business_cost,billing_type,total_tokens) VALUES
(1,'2026-09-26T01:00:00Z',1,'Before move',1,1,1,1,'m',20,10,0,100),
(2,'2026-09-26T02:00:00Z',1,'After move',2,1,1,1,'m',40,15,0,200),
(3,'2026-09-26T03:00:00Z',2,'Independent historical',1,1,1,1,'m',7,2,0,NULL),
(4,'2026-09-26T04:00:00Z',2,'Independent',NULL,1,1,1,'m',9,3,0,40),
(5,'2026-09-26T05:00:00Z',3,'Archived',2,1,1,1,'m',30,10,0,500),
(6,'2026-09-23T00:00:00Z',1,'Earlier',2,1,1,1,'m',12,6,0,100);
INSERT INTO upstream_monitor_history(target_id,supplier_id,target_name,model,status,checked_at,cost,cost_source) VALUES
(1,1,'Before move','m','error','2026-09-26T00:00:00Z',NULL,'unknown'),
(1,2,'After move','m','operational','2026-09-26T01:00:00Z',0.1,'reported'),
(2,NULL,'Independent','m','operational','2026-09-26T02:00:00Z',200,'reported'),
(2,1,'Independent historical','m','operational','2026-09-26T03:00:00Z',0.2,'estimated'),
(3,2,'Archived','m','operational','2026-09-26T04:00:00Z',0.3,'estimated');
INSERT INTO upstream_monitor_cost_rollups(hour_start,first_sample_at,last_sample_at,target_id,supplier_id,cost,unpriced_count,reported_count,estimated_count,sample_count) VALUES
('2026-09-23T00:00:00Z','2026-09-23T00:15:00Z','2026-09-23T00:25:00Z',1,1,3,0,1,0,1),
('2026-09-23T00:00:00Z','2026-09-23T00:15:00Z','2026-09-23T00:25:00Z',2,NULL,4,0,0,1,1)`)
	require.NoError(t, err)
	repo := &upstreamFinanceRepository{db: db}
	finance := service.NewUpstreamFinanceService(repo, nil, nil, nil, nil)
	one, two, four := int64(1), int64(2), int64(4)
	suppliers := []*service.UpstreamSupplier{{ID: one}, {ID: two}, {ID: four}}
	targets := []*service.UpstreamTarget{{ID: 1, SupplierID: &two}, {ID: 2}, {ID: 4, SupplierID: &four}}
	now := time.Now().UTC().Truncate(time.Second)
	for _, id := range []int64{1, 2, 4} {
		target, err := repo.GetTarget(ctx, id)
		require.NoError(t, err)
		identity := service.UpstreamBalanceIdentity(target)
		if id == 4 {
			// Previous key/PAT identity cannot leak its wallet; billing-only data
			// must preserve the same pending semantics as LatestBalance.
			_, err = db.ExecContext(ctx, `INSERT INTO upstream_balance_snapshots(target_id,identity_hash,wallet_ref,kind,balance,status,synced_at) VALUES($1,'prior-credential','default','wallet',999,'ok',$2)`, id, now)
			require.NoError(t, err)
			_, err = db.ExecContext(ctx, `INSERT INTO upstream_billing_snapshots(target_id,identity_hash,status,source,data,attempted_at) VALUES($1,$3,'ok','newapi_token','{"status":"ok","effective_rate_multiplier":9}',$2)`, id, now, identity)
			require.NoError(t, err)
			continue
		}
		_, err = db.ExecContext(ctx, `INSERT INTO upstream_balance_snapshots(target_id,identity_hash,wallet_ref,kind,balance,quota_remaining,unlimited_quota,currency,currency_source,status,synced_at)
VALUES($1,$2,'default','wallet',12,30,TRUE,'USD','reported','ok',$3)`, id, identity, now.Add(-time.Minute))
		require.NoError(t, err)
		_, err = db.ExecContext(ctx, `INSERT INTO upstream_billing_snapshots(target_id,identity_hash,status,source,data,attempted_at)
VALUES($1,$2,'ok','sub2api_billing',jsonb_build_object('status','ok','effective_rate_multiplier',0.3,'synced_at',$3::timestamptz),$3)`, id, identity, now.Add(-time.Minute))
		require.NoError(t, err)
		if id == 1 {
			_, err = db.ExecContext(ctx, `INSERT INTO upstream_balance_snapshots(target_id,identity_hash,wallet_ref,kind,status,synced_at,error) VALUES($1,$2,'default','unknown','error',$3,'timeout')`, id, identity, now)
			require.NoError(t, err)
			_, err = db.ExecContext(ctx, `INSERT INTO upstream_billing_snapshots(target_id,identity_hash,status,source,data,attempted_at) VALUES($1,$2,'error','sub2api_billing','{"error":"http_502"}',$3)`, id, identity, now)
			require.NoError(t, err)
		}
	}
	end := time.Date(2026, 9, 27, 0, 0, 0, 0, time.UTC)
	for _, days := range []int{1, 7, 30} {
		from := end.AddDate(0, 0, -days)
		data, err := finance.OverviewFinance(ctx, suppliers, targets, from, end)
		require.NoError(t, err)
		summary, err := finance.Summary(ctx, nil, nil, from, end)
		require.NoError(t, err)
		require.Equal(t, summary, data.Summary)
		for _, supplier := range suppliers {
			summary, err := finance.Summary(ctx, &supplier.ID, nil, from, end)
			require.NoError(t, err)
			require.Equal(t, summary, data.Suppliers[supplier.ID])
		}
		for _, target := range targets {
			summary, err := finance.Summary(ctx, target.SupplierID, &target.ID, from, end)
			require.NoError(t, err)
			require.Equal(t, summary, data.Targets[target.ID])
			balance, err := finance.LatestBalance(ctx, target.ID)
			require.NoError(t, err)
			require.Equal(t, balance, data.Balances[target.ID])
		}
		require.Nil(t, data.Summary.Profit, "unpriced historical monitoring remains unknown")
		require.Nil(t, data.Summary.TotalTokens, "unknown historical token usage remains unknown")
		require.Equal(t, "pending", data.Balances[4].Status)
		require.Nil(t, data.Balances[4].Balance)
		require.Equal(t, "pending", data.Balances[4].Billing.Status)
		require.Equal(t, "error", data.Balances[1].Status)
		require.Equal(t, 12.0, *data.Balances[1].Balance)
		require.Equal(t, 0.3, *data.Balances[1].Billing.EffectiveRateMultiplier)
		require.True(t, data.Balances[1].Billing.Stale)
		require.Zero(t, data.Suppliers[4].Revenue)
		require.NotNil(t, data.Suppliers[4].Profit)
	}
	// Both read paths reject a range that only partially covers archived samples.
	partialFrom := time.Date(2026, 9, 23, 0, 20, 0, 0, time.UTC)
	_, err = finance.OverviewFinance(ctx, suppliers, targets, partialFrom, partialFrom.Add(time.Hour))
	require.ErrorIs(t, err, service.ErrUpstreamFinanceArchivedRange)
	_, err = finance.Summary(ctx, nil, nil, partialFrom, partialFrom.Add(time.Hour))
	require.ErrorIs(t, err, service.ErrUpstreamFinanceArchivedRange)
	_, err = db.ExecContext(ctx, `UPDATE upstream_targets SET deleted_at=NOW() WHERE id=1`)
	require.NoError(t, err)
	_, err = finance.OverviewFinance(ctx, suppliers, targets, end.Add(-24*time.Hour), end)
	require.ErrorIs(t, err, service.ErrUpstreamFinanceTargetNotFound, "concurrent archive uses the same missing-target behavior")
}
