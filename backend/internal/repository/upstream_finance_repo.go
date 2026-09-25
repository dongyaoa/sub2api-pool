package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

type upstreamFinanceRepository struct{ db *sql.DB }

func NewUpstreamFinanceRepository(db *sql.DB) service.UpstreamFinanceRepository {
	return &upstreamFinanceRepository{db: db}
}

const upstreamFinanceLedgerSQL = `FROM upstream_finance_ledger l
WHERE l.created_at >= $1 AND l.created_at < $2
 AND ($4::bigint IS NOT NULL OR l.supplier_id IS NOT NULL)
 AND ($3::bigint IS NULL OR l.supplier_id = $3)
 AND ($4::bigint IS NULL OR l.target_id = $4)`

func upstreamFinanceArgs(q service.UpstreamFinanceQuery) []any {
	return []any{q.From, q.To, q.SupplierID, q.TargetID}
}

func (r *upstreamFinanceRepository) Summary(ctx context.Context, q service.UpstreamFinanceQuery) (*service.UpstreamFinanceSummary, error) {
	query := `WITH business AS (
 SELECT COALESCE(SUM(l.revenue),0) AS revenue,
 COALESCE(SUM(l.business_cost),0) AS cost, COUNT(*) AS requests,
 CASE WHEN COUNT(*) FILTER (WHERE l.total_tokens IS NULL)=0 THEN COALESCE(SUM(l.total_tokens),0) END AS tokens,
 COUNT(*) FILTER (WHERE l.total_tokens IS NULL) AS unknown_tokens
 ` + upstreamFinanceLedgerSQL + `
), monitoring AS (
 SELECT COALESCE(SUM(h.cost),0) AS cost,
 COUNT(*) FILTER (WHERE h.cost IS NULL OR h.cost_source = 'unknown') AS unpriced,
 COUNT(*) FILTER (WHERE h.cost IS NOT NULL AND h.cost_source = 'reported') AS reported,
 COUNT(*) FILTER (WHERE h.cost IS NOT NULL AND h.cost_source = 'estimated') AS estimated
 FROM upstream_monitor_history h
 WHERE h.checked_at >= $1 AND h.checked_at < $2
 AND ($4::bigint IS NOT NULL OR h.supplier_id IS NOT NULL)
 AND ($3::bigint IS NULL OR h.supplier_id = $3)
 AND ($4::bigint IS NULL OR h.target_id = $4)
)
 SELECT b.revenue,b.cost,b.requests,m.cost,m.unpriced,m.reported,m.estimated,
 b.tokens,b.unknown_tokens
 FROM business b CROSS JOIN monitoring m`
	summary := &service.UpstreamFinanceSummary{Currency: "USD", From: q.From, To: q.To, CostSource: "estimated"}
	var monitorCost float64
	var reported, estimated int64
	err := r.db.QueryRowContext(ctx, query, upstreamFinanceArgs(q)...).Scan(&summary.Revenue, &summary.BusinessCost, &summary.RequestCount, &monitorCost, &summary.UnpricedMonitorCount, &reported, &estimated, &summary.TotalTokens, &summary.UnknownTokenRequests)
	if err != nil {
		return nil, fmt.Errorf("aggregate upstream finance: %w", err)
	}
	// The ledger captures the same historical account billing formula as the
	// account statistics page, independently from the user's actual_cost.
	summary.AccountBilled = summary.BusinessCost
	if summary.UnpricedMonitorCount > 0 {
		summary.CostSource = "unknown"
	} else {
		summary.MonitorCost = &monitorCost
		profit := summary.Revenue - summary.BusinessCost - monitorCost
		summary.Profit = &profit
		if reported > 0 {
			if summary.RequestCount > 0 || estimated > 0 {
				summary.CostSource = "mixed"
			} else {
				summary.CostSource = "reported"
			}
		}
	}
	// /v1/usage reports the upstream's own "today", without a timezone or
	// per-request billing identifiers. It cannot be reconciled to an arbitrary
	// local half-open range reliably. The reported value remains on its balance
	// card; these range-specific fields intentionally stay unknown.
	return summary, nil
}

func (r *upstreamFinanceRepository) Details(ctx context.Context, q service.UpstreamFinanceQuery) ([]service.UpstreamFinanceRow, int64, error) {
	args := upstreamFinanceArgs(q)
	var total int64
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) `+upstreamFinanceLedgerSQL, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count upstream financial records: %w", err)
	}
	query := `SELECT l.id,l.created_at,l.target_id,l.target_name,l.supplier_id,l.supplier_name,
 l.account_id,l.group_id,l.model,l.request_id,l.revenue,l.business_cost,l.revenue-l.business_cost,l.billing_type,l.total_tokens
 ` + upstreamFinanceLedgerSQL + ` ORDER BY l.created_at DESC,l.id DESC LIMIT $5 OFFSET $6`
	args = append(args, q.PageSize, (q.Page-1)*q.PageSize)
	rows, err := r.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, 0, fmt.Errorf("list upstream financial records: %w", err)
	}
	defer func() { _ = rows.Close() }()
	items := make([]service.UpstreamFinanceRow, 0)
	for rows.Next() {
		var item service.UpstreamFinanceRow
		if err := rows.Scan(&item.ID, &item.CreatedAt, &item.TargetID, &item.TargetName, &item.SupplierID, &item.SupplierName, &item.AccountID, &item.GroupID, &item.Model, &item.RequestID, &item.Revenue, &item.BusinessCost, &item.Profit, &item.BillingType, &item.TotalTokens); err != nil {
			return nil, 0, err
		}
		item.AccountBilled = item.BusinessCost
		items = append(items, item)
	}
	return items, total, rows.Err()
}

func (r *upstreamFinanceRepository) GetTarget(ctx context.Context, id int64) (*service.UpstreamFinanceTarget, error) {
	t := &service.UpstreamFinanceTarget{}
	err := r.db.QueryRowContext(ctx, `SELECT t.id,t.supplier_id,t.provider,t.endpoint,t.api_key_encrypted,t.wallet_ref,t.newapi_user_id,t.newapi_access_token_encrypted
 FROM upstream_targets t LEFT JOIN upstream_suppliers s ON s.id=t.supplier_id
 WHERE t.id=$1 AND t.deleted_at IS NULL AND (t.supplier_id IS NULL OR s.deleted_at IS NULL)`, id).Scan(&t.ID, &t.SupplierID, &t.Provider, &t.Endpoint, &t.APIKeyEncrypted, &t.WalletRef, &t.NewAPIUserID, &t.NewAPIAccessTokenEncrypted)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrUpstreamFinanceTargetNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("load upstream financial target: %w", err)
	}
	return t, nil
}

func (r *upstreamFinanceRepository) LatestBalance(ctx context.Context, id int64, identity string) (*service.UpstreamBalanceSnapshot, error) {
	s := &service.UpstreamBalanceSnapshot{}
	err := r.db.QueryRowContext(ctx, `WITH latest AS (
 SELECT * FROM upstream_balance_snapshots WHERE target_id=$1 AND identity_hash=$2 ORDER BY synced_at DESC,id DESC LIMIT 1
), good AS (
 SELECT * FROM upstream_balance_snapshots WHERE target_id=$1 AND identity_hash=$2 AND status='ok' ORDER BY synced_at DESC,id DESC LIMIT 1
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
 FROM latest l LEFT JOIN good g ON TRUE`, id, identity).Scan(&s.TargetID, &s.WalletRef, &s.Kind, &s.Balance, &s.QuotaRemaining, &s.TodayUsed, &s.TotalUsed, &s.UnlimitedQuota, &s.Currency, &s.CurrencySource, &s.Status, &s.SyncedAt, &s.Error, &s.LastAttemptAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load upstream balance snapshot: %w", err)
	}
	s.Billing, err = r.latestRemoteBilling(ctx, id, identity)
	if err != nil {
		return nil, err
	}
	return s, nil
}

func (r *upstreamFinanceRepository) latestRemoteBilling(ctx context.Context, id int64, identity string) (*service.UpstreamRemoteBillingSnapshot, error) {
	var data []byte
	var status, errorCode string
	var attemptedAt time.Time
	err := r.db.QueryRowContext(ctx, `WITH latest AS (
 SELECT * FROM upstream_billing_snapshots WHERE target_id=$1 AND identity_hash=$2 ORDER BY attempted_at DESC,id DESC LIMIT 1
), good AS (
 SELECT * FROM upstream_billing_snapshots WHERE target_id=$1 AND identity_hash=$2 AND status='ok' ORDER BY attempted_at DESC,id DESC LIMIT 1
)
 SELECT COALESCE(g.data,l.data),l.status,l.attempted_at,COALESCE(l.data->>'error','')
 FROM latest l LEFT JOIN good g ON TRUE`, id, identity).Scan(&data, &status, &attemptedAt, &errorCode)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load upstream billing snapshot: %w", err)
	}
	var snapshot service.UpstreamRemoteBillingSnapshot
	if err = json.Unmarshal(data, &snapshot); err != nil {
		return nil, fmt.Errorf("decode upstream billing snapshot: %w", err)
	}
	snapshot.Status = status
	snapshot.LastAttemptAt = &attemptedAt
	snapshot.Error = errorCode
	if status != "ok" {
		snapshot.Stale = true
	}
	return &snapshot, nil
}

func (r *upstreamFinanceRepository) SaveBalance(ctx context.Context, t *service.UpstreamFinanceTarget, identity string, s *service.UpstreamBalanceSnapshot, token string, next time.Time) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var id int64
	err = tx.QueryRowContext(ctx, `SELECT id FROM upstream_targets WHERE id=$1 AND deleted_at IS NULL
 AND supplier_id IS NOT DISTINCT FROM $2::bigint AND provider=$3 AND endpoint=$4
 AND api_key_encrypted=$5 AND wallet_ref=$6 AND balance_lease_token=$7
 AND newapi_user_id=$8 AND newapi_access_token_encrypted=$9 FOR UPDATE`, t.ID, t.SupplierID, t.Provider, t.Endpoint, t.APIKeyEncrypted, t.WalletRef, token, t.NewAPIUserID, t.NewAPIAccessTokenEncrypted).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return service.ErrUpstreamFinanceIdentityChanged
	}
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO upstream_balance_snapshots
 (target_id,supplier_id,wallet_ref,identity_hash,kind,balance,quota_remaining,today_used,total_used,currency,currency_source,status,synced_at,error,unlimited_quota)
 VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)`, t.ID, t.SupplierID, t.WalletRef, identity, s.Kind, s.Balance, s.QuotaRemaining, s.TodayUsed, s.TotalUsed, s.Currency, s.CurrencySource, s.Status, s.SyncedAt, s.Error, s.UnlimitedQuota)
	if err != nil {
		return fmt.Errorf("save upstream balance snapshot: %w", err)
	}
	if s.Billing != nil {
		data, marshalErr := json.Marshal(s.Billing)
		if marshalErr != nil {
			return fmt.Errorf("encode upstream billing snapshot: %w", marshalErr)
		}
		attempted := s.Billing.LastAttemptAt
		if attempted == nil {
			attempted = s.SyncedAt
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO upstream_billing_snapshots (target_id,identity_hash,status,source,data,attempted_at) VALUES($1,$2,$3,$4,$5::jsonb,$6)`, t.ID, identity, s.Billing.Status, s.Billing.Source, string(data), attempted)
		if err != nil {
			return fmt.Errorf("save upstream billing snapshot: %w", err)
		}
	}
	_, err = tx.ExecContext(ctx, `UPDATE upstream_targets SET balance_next_sync_at=$3,balance_lease_until=NULL,balance_lease_token=NULL WHERE id=$1 AND balance_lease_token=$2`, t.ID, token, next)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func (r *upstreamFinanceRepository) ClaimBalance(ctx context.Context, id int64, token string, now, until time.Time) (bool, error) {
	result, err := r.db.ExecContext(ctx, `UPDATE upstream_targets t SET balance_lease_token=$2,balance_lease_until=$4
 WHERE t.id=$1 AND t.deleted_at IS NULL
 AND (t.balance_lease_until IS NULL OR t.balance_lease_until <= $3)
 AND (t.supplier_id IS NULL OR EXISTS(SELECT 1 FROM upstream_suppliers s WHERE s.id=t.supplier_id AND s.deleted_at IS NULL))`, id, token, now, until)
	if err != nil {
		return false, err
	}
	n, err := result.RowsAffected()
	return n == 1, err
}

func (r *upstreamFinanceRepository) ReleaseBalance(ctx context.Context, id int64, token string, next time.Time) error {
	_, err := r.db.ExecContext(ctx, `UPDATE upstream_targets SET balance_next_sync_at=$3,balance_lease_until=NULL,balance_lease_token=NULL WHERE id=$1 AND balance_lease_token=$2`, id, token, next)
	return err
}

func (r *upstreamFinanceRepository) DueBalanceTargetIDs(ctx context.Context, now time.Time, limit int) ([]int64, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT t.id FROM upstream_targets t LEFT JOIN upstream_suppliers s ON s.id=t.supplier_id
 WHERE t.deleted_at IS NULL AND (t.supplier_id IS NULL OR s.deleted_at IS NULL) AND t.balance_next_sync_at <= $1
 AND (t.balance_lease_until IS NULL OR t.balance_lease_until <= $1)
 ORDER BY t.balance_next_sync_at,t.id LIMIT $2`, now, limit)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	ids := make([]int64, 0)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (r *upstreamFinanceRepository) ActiveAccountIDs(ctx context.Context, targetID int64) ([]int64, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT b.account_id FROM upstream_account_bindings b JOIN accounts a ON a.id=b.account_id
 WHERE b.target_id=$1 AND b.valid_until IS NULL AND a.deleted_at IS NULL ORDER BY b.account_id`, targetID)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()
	ids := make([]int64, 0)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
