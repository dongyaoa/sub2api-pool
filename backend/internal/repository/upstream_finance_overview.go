package repository

import (
	"context"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
)

var _ service.UpstreamFinanceOverviewRepository = (*upstreamFinanceRepository)(nil)

// Each fact source is scanned once for the time range, then its grouped facts
// contribute to at most three display scopes. Supplier identity is historical;
// moving a target never moves its ledger or monitoring costs between suppliers.
const upstreamOverviewSummarySQL = `WITH requested_targets AS (
 SELECT id,NULLIF(supplier_id,0) AS supplier_id FROM unnest($4::bigint[],$5::bigint[]) AS t(id,supplier_id)
), facts AS MATERIALIZED (
 SELECT target_id,supplier_id,SUM(revenue) AS revenue,SUM(business_cost) AS business_cost,COUNT(*) AS requests,
 COALESCE(SUM(total_tokens),0) AS tokens,COUNT(*) FILTER(WHERE total_tokens IS NULL) AS unknown_tokens,
 0::numeric AS monitor_cost,0::bigint AS unpriced,0::bigint AS reported,0::bigint AS estimated,FALSE AS partial
 FROM upstream_finance_ledger WHERE created_at >= $1 AND created_at < $2 GROUP BY target_id,supplier_id
 UNION ALL
 SELECT target_id,supplier_id,0,0,0,0,0,COALESCE(SUM(cost),0),
 COUNT(*) FILTER(WHERE cost IS NULL OR cost_source='unknown'),
 COUNT(*) FILTER(WHERE cost IS NOT NULL AND cost_source='reported'),
 COUNT(*) FILTER(WHERE cost IS NOT NULL AND cost_source='estimated'),FALSE
 FROM upstream_monitor_history WHERE checked_at >= $1 AND checked_at < $2 GROUP BY target_id,supplier_id
 UNION ALL
 SELECT target_id,supplier_id,0,0,0,0,0,SUM(cost),SUM(unpriced_count),SUM(reported_count),SUM(estimated_count),
 BOOL_OR(first_sample_at < $1 OR last_sample_at >= $2)
 FROM upstream_monitor_cost_rollups
 WHERE hour_start < $2 AND hour_start+INTERVAL '1 hour' > $1 AND first_sample_at < $2 AND last_sample_at >= $1
 GROUP BY target_id,supplier_id
), scoped AS (
 SELECT 'total'::text AS kind,0::bigint AS id,f.* FROM facts f WHERE supplier_id IS NOT NULL
 UNION ALL
 SELECT 'supplier',supplier_id,f.* FROM facts f WHERE supplier_id=ANY($3::bigint[])
 UNION ALL
 SELECT 'target',t.id,f.* FROM requested_targets t JOIN facts f ON f.target_id=t.id AND (t.supplier_id IS NULL OR f.supplier_id=t.supplier_id)
), totals AS (
 SELECT kind,id,SUM(revenue) AS revenue,SUM(business_cost) AS business_cost,SUM(requests) AS requests,
 SUM(tokens) AS tokens,SUM(unknown_tokens) AS unknown_tokens,SUM(monitor_cost) AS monitor_cost,
 SUM(unpriced) AS unpriced,SUM(reported) AS reported,SUM(estimated) AS estimated,BOOL_OR(partial) AS partial
 FROM scoped GROUP BY kind,id
), scopes AS (
 SELECT 'total'::text AS kind,0::bigint AS id
 UNION ALL SELECT 'supplier',unnest($3::bigint[])
 UNION ALL SELECT 'target',id FROM requested_targets
)
SELECT s.kind,s.id,COALESCE(t.revenue,0),COALESCE(t.business_cost,0),COALESCE(t.requests,0),
 COALESCE(t.monitor_cost,0),COALESCE(t.unpriced,0),COALESCE(t.reported,0),COALESCE(t.estimated,0),
 CASE WHEN COALESCE(t.unknown_tokens,0)=0 THEN COALESCE(t.tokens,0) END,COALESCE(t.unknown_tokens,0),COALESCE(t.partial,FALSE)
FROM scopes s LEFT JOIN totals t USING(kind,id)`

const upstreamOverviewBalanceSQL = `WITH identities AS (
 SELECT * FROM unnest($1::bigint[],$2::text[]) AS value(target_id,identity_hash)
)
SELECT l.target_id,l.wallet_ref,
 CASE WHEN l.status='ok' OR g.id IS NULL THEN l.kind ELSE g.kind END,
 CASE WHEN l.status='ok' THEN l.balance ELSE g.balance END,
 CASE WHEN l.status='ok' THEN l.quota_remaining ELSE g.quota_remaining END,
 CASE WHEN l.status='ok' THEN l.today_used ELSE g.today_used END,
 CASE WHEN l.status='ok' THEN l.total_used ELSE g.total_used END,
 CASE WHEN l.status='ok' OR g.id IS NULL THEN l.unlimited_quota ELSE g.unlimited_quota END,
 CASE WHEN l.status='ok' OR g.id IS NULL THEN l.currency ELSE g.currency END,
 CASE WHEN l.status='ok' OR g.id IS NULL THEN l.currency_source ELSE g.currency_source END,
 l.status,CASE WHEN l.status='ok' THEN l.synced_at ELSE g.synced_at END,l.error,l.synced_at
FROM identities i
CROSS JOIN LATERAL (SELECT * FROM upstream_balance_snapshots WHERE target_id=i.target_id AND identity_hash=i.identity_hash ORDER BY synced_at DESC,id DESC LIMIT 1) l
LEFT JOIN LATERAL (SELECT * FROM upstream_balance_snapshots WHERE target_id=i.target_id AND identity_hash=i.identity_hash AND status='ok' ORDER BY synced_at DESC,id DESC LIMIT 1) g ON TRUE`

const upstreamOverviewBillingSQL = `WITH identities AS (
 SELECT * FROM unnest($1::bigint[],$2::text[]) AS value(target_id,identity_hash)
)
SELECT i.target_id,COALESCE(g.data,l.data),l.status,l.attempted_at,COALESCE(l.data->>'error','')
FROM identities i
CROSS JOIN LATERAL (SELECT * FROM upstream_billing_snapshots WHERE target_id=i.target_id AND identity_hash=i.identity_hash ORDER BY attempted_at DESC,id DESC LIMIT 1) l
LEFT JOIN LATERAL (SELECT * FROM upstream_billing_snapshots WHERE target_id=i.target_id AND identity_hash=i.identity_hash AND status='ok' ORDER BY attempted_at DESC,id DESC LIMIT 1) g ON TRUE`

func (r *upstreamFinanceRepository) LoadOverviewFinance(ctx context.Context, q service.UpstreamFinanceQuery, supplierIDs []int64, targets []service.UpstreamFinanceOverviewTarget) (*service.UpstreamFinanceOverviewData, error) {
	out := &service.UpstreamFinanceOverviewData{Suppliers: map[int64]*service.UpstreamFinanceSummary{}, Targets: map[int64]*service.UpstreamFinanceSummary{}, Balances: map[int64]*service.UpstreamBalanceSnapshot{}, BalanceTargets: map[int64]*service.UpstreamFinanceTarget{}}
	ids, suppliers := make([]int64, 0, len(targets)), make([]int64, 0, len(targets))
	for _, target := range targets {
		ids = append(ids, target.ID)
		supplier := int64(0)
		if target.SupplierID != nil {
			supplier = *target.SupplierID
		}
		suppliers = append(suppliers, supplier)
	}
	rows, err := r.db.QueryContext(ctx, upstreamOverviewSummarySQL, q.From, q.To, pq.Array(supplierIDs), pq.Array(ids), pq.Array(suppliers))
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var kind string
		var id int64
		summary, err := scanUpstreamFinanceSummary(rows, q, &kind, &id)
		if err != nil {
			_ = rows.Close()
			return nil, err
		}
		switch kind {
		case "total":
			out.Summary = summary
		case "supplier":
			out.Suppliers[id] = summary
		case "target":
			out.Targets[id] = summary
		}
	}
	err = rows.Err()
	_ = rows.Close()
	if err != nil || len(ids) == 0 {
		return out, err
	}
	// Resolve credentials in one current read, using the same Go identity as
	// single-item balance reads; no copied SQL hashing rules or secret logging.
	rows, err = r.db.QueryContext(ctx, `SELECT t.id,t.supplier_id,t.provider,t.endpoint,t.api_key_encrypted,t.wallet_ref,t.newapi_user_id,t.newapi_access_token_encrypted
 FROM upstream_targets t LEFT JOIN upstream_suppliers s ON s.id=t.supplier_id
 WHERE t.id=ANY($1) AND t.deleted_at IS NULL AND (t.supplier_id IS NULL OR s.deleted_at IS NULL) ORDER BY t.id`, pq.Array(ids))
	if err != nil {
		return nil, err
	}
	currentIDs, identities := []int64{}, []string{}
	for rows.Next() {
		t := &service.UpstreamFinanceTarget{}
		if err = rows.Scan(&t.ID, &t.SupplierID, &t.Provider, &t.Endpoint, &t.APIKeyEncrypted, &t.WalletRef, &t.NewAPIUserID, &t.NewAPIAccessTokenEncrypted); err != nil {
			_ = rows.Close()
			return nil, err
		}
		out.BalanceTargets[t.ID] = t
		currentIDs = append(currentIDs, t.ID)
		identities = append(identities, service.UpstreamBalanceIdentity(t))
	}
	err = rows.Err()
	_ = rows.Close()
	if err != nil {
		return nil, err
	}
	for _, id := range ids {
		if out.BalanceTargets[id] == nil {
			return nil, service.ErrUpstreamFinanceTargetNotFound
		}
	}
	rows, err = r.db.QueryContext(ctx, upstreamOverviewBalanceSQL, pq.Array(currentIDs), pq.Array(identities))
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		snapshot, err := scanUpstreamBalanceSnapshot(rows)
		if err != nil {
			_ = rows.Close()
			return nil, err
		}
		out.Balances[snapshot.TargetID] = snapshot
	}
	err = rows.Err()
	_ = rows.Close()
	if err != nil || len(out.Balances) == 0 {
		return out, err
	}
	// Match LatestBalance: a standalone billing row without a balance observation
	// must not replace the pending state. Such rows can occur in old databases.
	balanceIDs, balanceIdentities := []int64{}, []string{}
	for i, id := range currentIDs {
		if out.Balances[id] != nil {
			balanceIDs, balanceIdentities = append(balanceIDs, id), append(balanceIdentities, identities[i])
		}
	}
	rows, err = r.db.QueryContext(ctx, upstreamOverviewBillingSQL, pq.Array(balanceIDs), pq.Array(balanceIdentities))
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	for rows.Next() {
		var id int64
		snapshot, err := scanUpstreamRemoteBilling(rows, &id)
		if err != nil {
			return nil, err
		}
		out.Balances[id].Billing = snapshot
	}
	return out, rows.Err()
}
