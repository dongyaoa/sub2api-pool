package service

import (
	"context"
	"time"
)

// UpstreamFinanceSummary reports ledger consumption, not cash revenue. A missing
// monitor price makes the combined cost and profit incomplete, never zero.
type UpstreamFinanceSummary struct {
	Revenue      float64  `json:"revenue"`
	BusinessCost float64  `json:"business_cost"`
	MonitorCost  *float64 `json:"monitor_cost"`
	Profit       *float64 `json:"profit"`
	RequestCount int64    `json:"request_count"`
	// Includes input, output, cache creation and cache read tokens. An expired
	// source usage log without a token snapshot makes the aggregate unknown.
	TotalTokens          *int64 `json:"total_tokens"`
	UnknownTokenRequests int64  `json:"unknown_token_requests"`
	// Same snapshot as BusinessCost: account statistics cost times account rate,
	// independent from the user debit in Revenue and from monitoring costs.
	AccountBilled        float64   `json:"account_billed"`
	CostSource           string    `json:"cost_source"`
	Currency             string    `json:"currency"`
	From                 time.Time `json:"from"`
	To                   time.Time `json:"to"`
	RemoteUsed           *float64  `json:"remote_used"`
	ReconciliationDelta  *float64  `json:"reconciliation_delta"`
	UnpricedMonitorCount int64     `json:"unpriced_monitor_count"`
}

type UpstreamBalanceSnapshot struct {
	TargetID       int64      `json:"target_id"`
	WalletRef      string     `json:"wallet_ref"`
	Kind           string     `json:"kind"`
	Balance        *float64   `json:"balance"`
	QuotaRemaining *float64   `json:"quota_remaining"`
	TodayUsed      *float64   `json:"today_used"`
	TotalUsed      *float64   `json:"total_used"`
	Currency       string     `json:"currency"`
	Status         string     `json:"status"`
	SyncedAt       *time.Time `json:"synced_at"`
	LastAttemptAt  *time.Time `json:"last_attempt_at"`
	Error          string     `json:"error"`
	// A Sub2API response can omit unit. This marker distinguishes its documented
	// USD convention from an explicitly reported currency.
	CurrencySource string                         `json:"currency_source,omitempty"`
	Billing        *UpstreamRemoteBillingSnapshot `json:"billing"`
}

// UpstreamRemoteBillingSnapshot is a read-only upstream declaration. It never
// changes local procurement rates or rewrites historical financial records.
type UpstreamRemoteBillingSnapshot struct {
	GroupID                 *int64     `json:"group_id"`
	GroupName               *string    `json:"group_name"`
	GroupRateMultiplier     *float64   `json:"group_rate_multiplier"`
	UserRateMultiplier      *float64   `json:"user_rate_multiplier"`
	ResolvedRateMultiplier  *float64   `json:"resolved_rate_multiplier"`
	EffectiveRateMultiplier *float64   `json:"effective_rate_multiplier"`
	BillingScope            string     `json:"billing_scope"`
	Source                  string     `json:"source"`
	Status                  string     `json:"status"`
	Stale                   bool       `json:"stale"`
	SyncedAt                *time.Time `json:"synced_at"`
	LastAttemptAt           *time.Time `json:"last_attempt_at"`
	ObservedAt              *time.Time `json:"observed_at"`
	Error                   string     `json:"error"`
}

type UpstreamFinanceRow struct {
	ID            int64     `json:"id"`
	CreatedAt     time.Time `json:"created_at"`
	TargetID      int64     `json:"target_id"`
	TargetName    string    `json:"target_name"`
	SupplierID    *int64    `json:"supplier_id"`
	SupplierName  string    `json:"supplier_name"`
	AccountID     *int64    `json:"account_id"`
	GroupID       *int64    `json:"group_id"`
	Model         string    `json:"model"`
	RequestID     string    `json:"request_id"`
	Revenue       float64   `json:"revenue"`
	BusinessCost  float64   `json:"business_cost"`
	Profit        float64   `json:"profit"`
	BillingType   int       `json:"billing_type"`
	TotalTokens   *int64    `json:"total_tokens"`
	AccountBilled float64   `json:"account_billed"`
}

type UpstreamFinanceQuery struct {
	SupplierID *int64
	TargetID   *int64
	From       time.Time
	To         time.Time
	Page       int
	PageSize   int
}

type UpstreamFinancePage struct {
	Summary  *UpstreamFinanceSummary `json:"summary"`
	Items    []UpstreamFinanceRow    `json:"items"`
	Total    int64                   `json:"total"`
	Page     int                     `json:"page"`
	PageSize int                     `json:"page_size"`
}

// UpstreamFinanceTarget is internal and deliberately has no exported JSON form.
// Credentials must never be serialized by handlers or persisted in snapshots.
type UpstreamFinanceTarget struct {
	ID              int64  `json:"-"`
	SupplierID      *int64 `json:"-"`
	Provider        string `json:"-"`
	Endpoint        string `json:"-"`
	APIKeyEncrypted string `json:"-"`
	WalletRef       string `json:"-"`
}

type UpstreamFinanceRepository interface {
	Summary(context.Context, UpstreamFinanceQuery) (*UpstreamFinanceSummary, error)
	Details(context.Context, UpstreamFinanceQuery) ([]UpstreamFinanceRow, int64, error)
	GetTarget(context.Context, int64) (*UpstreamFinanceTarget, error)
	LatestBalance(context.Context, int64, string) (*UpstreamBalanceSnapshot, error)
	SaveBalance(context.Context, *UpstreamFinanceTarget, string, *UpstreamBalanceSnapshot, string, time.Time) error
	ClaimBalance(context.Context, int64, string, time.Time, time.Time) (bool, error)
	ReleaseBalance(context.Context, int64, string, time.Time) error
	DueBalanceTargetIDs(context.Context, time.Time, int) ([]int64, error)
	ActiveAccountIDs(context.Context, int64) ([]int64, error)
}
