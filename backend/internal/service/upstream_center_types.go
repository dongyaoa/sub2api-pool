package service

import (
	"context"
	"encoding/json"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

var (
	ErrUpstreamNotFound        = infraerrors.NotFound("UPSTREAM_NOT_FOUND", "upstream supplier or target not found")
	ErrUpstreamInvalid         = infraerrors.BadRequest("UPSTREAM_INVALID", "invalid upstream configuration")
	ErrUpstreamBusy            = infraerrors.Conflict("UPSTREAM_CHECK_RUNNING", "this target is already being checked")
	ErrUpstreamBindingConflict = infraerrors.Conflict("UPSTREAM_ACCOUNT_BOUND", "an account is already bound to another target, or its credentials changed; reload and retry")
	ErrUpstreamDuplicateKey    = infraerrors.Conflict("UPSTREAM_DUPLICATE_KEY", "this upstream endpoint and API key already belong to another supplier group")
)

type UpstreamSupplier struct {
	ID        int64                      `json:"id"`
	Name      string                     `json:"name"`
	Website   string                     `json:"website"`
	Notes     string                     `json:"notes"`
	CreatedAt time.Time                  `json:"created_at"`
	UpdatedAt time.Time                  `json:"updated_at"`
	Targets   []*UpstreamTarget          `json:"targets"`
	Finance   *UpstreamFinanceSummary    `json:"finance"`
	Wallets   []*UpstreamBalanceSnapshot `json:"wallets"`
}

type UpstreamBindingCredential struct {
	APIKey  string
	BaseURL string
}

type UpstreamTarget struct {
	ID                          int64                               `json:"id"`
	SupplierID                  *int64                              `json:"supplier_id"`
	Name                        string                              `json:"name"`
	Provider                    string                              `json:"provider"`
	APIMode                     string                              `json:"api_mode"`
	Endpoint                    string                              `json:"endpoint"`
	APIKeyEncrypted             string                              `json:"-"`
	APIKeyFingerprint           string                              `json:"-"`
	APIKeyMasked                string                              `json:"api_key_masked"`
	NewAPIUserID                int64                               `json:"newapi_user_id"`
	NewAPIAccessTokenEncrypted  string                              `json:"-"`
	NewAPIAccessTokenConfigured bool                                `json:"newapi_access_token_configured"`
	Models                      []string                            `json:"models"`
	Enabled                     bool                                `json:"enabled"`
	IntervalSeconds             int                                 `json:"interval_seconds"`
	TimeoutSeconds              int                                 `json:"timeout_seconds"`
	DegradedThresholdMs         int                                 `json:"degraded_threshold_ms"`
	AccountIDs                  []int64                             `json:"account_ids"`
	WalletRef                   string                              `json:"wallet_ref"`
	Notes                       string                              `json:"notes"`
	LastCheckedAt               *time.Time                          `json:"last_checked_at"`
	NextCheckAt                 *time.Time                          `json:"next_check_at"`
	CreatedAt                   time.Time                           `json:"created_at"`
	UpdatedAt                   time.Time                           `json:"updated_at"`
	Statistics                  []*UpstreamModelStatistics          `json:"statistics"`
	Balance                     *UpstreamBalanceSnapshot            `json:"balance"`
	Finance                     *UpstreamFinanceSummary             `json:"finance"`
	BindingCredentials          map[int64]UpstreamBindingCredential `json:"-"`
	ResetBindings               bool                                `json:"-"`
}

// Pointer fields retain partial-update semantics; an explicit null supplier_id
// moves a target to the independent monitor tab.
type UpstreamTargetInput struct {
	SupplierID json.RawMessage `json:"supplier_id"`
	Name       *string         `json:"name"`
	Provider   *string         `json:"provider"`
	APIMode    *string         `json:"api_mode"`
	Endpoint   *string         `json:"endpoint"`
	APIKey     *string         `json:"api_key"`
	// Console credentials are optional and separate from the inference key.
	// A zero user ID explicitly clears both fields; a blank token preserves it.
	NewAPIUserID      *int64  `json:"newapi_user_id"`
	NewAPIAccessToken *string `json:"newapi_access_token"`
	// SourceAccountID copies credentials once for an independent monitor. It is
	// never persisted as an account binding or returned in target responses.
	SourceAccountID     *int64    `json:"source_account_id"`
	Models              *[]string `json:"models"`
	Enabled             *bool     `json:"enabled"`
	IntervalSeconds     *int      `json:"interval_seconds"`
	TimeoutSeconds      *int      `json:"timeout_seconds"`
	DegradedThresholdMs *int      `json:"degraded_threshold_ms"`
	AccountIDs          *[]int64  `json:"account_ids"`
	WalletRef           *string   `json:"wallet_ref"`
	Notes               *string   `json:"notes"`
}

type UpstreamModelStatistics struct {
	Model        string   `json:"model"`
	Status       string   `json:"status"`
	Availability *float64 `json:"availability"`
	// Fixed rolling seven-day availability, independent of the selected window.
	Availability7d *float64 `json:"availability_7d"`
	// The latest observation may be a failure or have no measured latency.
	LatestLatencyMs *int                     `json:"latest_latency_ms"`
	AvgLatencyMs    *float64                 `json:"avg_latency_ms"`
	P95LatencyMs    *float64                 `json:"p95_latency_ms"`
	SampleCount     int64                    `json:"sample_count"`
	SuccessCount    int64                    `json:"success_count"`
	LastCheckedAt   *time.Time               `json:"last_checked_at"`
	Timeline        []*UpstreamHistoryRecord `json:"timeline"`
}

type UpstreamHistoryRecord struct {
	ID            int64     `json:"id"`
	TargetID      int64     `json:"target_id"`
	Model         string    `json:"model"`
	Status        string    `json:"status"`
	LatencyMs     *int      `json:"latency_ms"`
	PingLatencyMs *int      `json:"ping_latency_ms"`
	HTTPStatus    *int      `json:"http_status"`
	Message       string    `json:"message"`
	CheckedAt     time.Time `json:"checked_at"`
	Cost          *float64  `json:"cost"`
	CostSource    string    `json:"cost_source"`
}

type UpstreamHistoryQuery struct {
	TargetID int64
	Model    string
	From     *time.Time
	To       *time.Time
	Page     int
	PageSize int
}
type UpstreamHistoryPage struct {
	Items    []*UpstreamHistoryRecord `json:"items"`
	Total    int64                    `json:"total"`
	Page     int                      `json:"page"`
	PageSize int                      `json:"page_size"`
}
type UpstreamOverview struct {
	Suppliers []*UpstreamSupplier     `json:"suppliers"`
	Monitors  []*UpstreamTarget       `json:"monitors"`
	Summary   *UpstreamFinanceSummary `json:"summary"`
}
type UpstreamModelsInput struct {
	TargetID  *int64 `json:"target_id"`
	AccountID *int64 `json:"account_id"`
	Provider  string `json:"provider"`
	Endpoint  string `json:"endpoint"`
	APIKey    string `json:"api_key"`
}

type UpstreamCenterRepository interface {
	SaveOrder(context.Context, UpstreamOrderInput) error
	ListSuppliers(context.Context) ([]*UpstreamSupplier, error)
	GetSupplier(context.Context, int64) (*UpstreamSupplier, error)
	SaveSupplier(context.Context, *UpstreamSupplier) error
	ArchiveSupplier(context.Context, int64) error
	ListTargets(context.Context) ([]*UpstreamTarget, error)
	GetTarget(context.Context, int64) (*UpstreamTarget, error)
	SaveTarget(context.Context, *UpstreamTarget) error
	ArchiveTarget(context.Context, int64) error
	PopulateStatistics(context.Context, []*UpstreamTarget, time.Time) error
	History(context.Context, UpstreamHistoryQuery) (*UpstreamHistoryPage, error)
	DueTargetIDs(context.Context, int) ([]int64, error)
	ClaimCheck(context.Context, int64, string, bool) (bool, error)
	CompleteCheck(context.Context, int64, string, []*UpstreamHistoryRecord) (bool, error)
	ReleaseCheck(context.Context, int64, string) error
}
