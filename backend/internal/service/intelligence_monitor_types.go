package service

import (
	"context"
	"encoding/json"
	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"time"
)

const IntelligenceMonitorModel = "gpt-6-astra"
const IntelligenceMonitorReasoning = "high"
const IntelligenceMonitorPrompt = "创建一个 HTML，内容是用 SVG 绘制一个鹈鹕骑自行车的 2D 动画。你不需要任何测试。"
const IntelligenceMonitorRetainedRuns = 20
const IntelligenceMonitorDefaultTimeoutSeconds = 600
const IntelligenceMonitorDefaultIntervalSeconds = 300
const IntelligenceMonitorMinTimeoutSeconds = 180
const IntelligenceMonitorMaxTimeoutSeconds = 900

// Covers source preparation, result persistence and scheduler delay after the
// generation deadline. A claimed run keeps its own captured timeout budget.
const IntelligenceMonitorLeaseGraceSeconds = 180

var (
	ErrIntelligenceNotFound = infraerrors.NotFound("INTELLIGENCE_MONITOR_NOT_FOUND", "intelligence monitoring plan or run not found")
	ErrIntelligenceInvalid  = infraerrors.BadRequest("INTELLIGENCE_MONITOR_INVALID", "invalid intelligence monitoring configuration")
	ErrIntelligenceBusy     = infraerrors.Conflict("INTELLIGENCE_MONITOR_BUSY", "this plan has a pending or running generation; wait for completion")
)

type IntelligenceMonitorPlan struct {
	ID               int64                          `json:"id"`
	Name             string                         `json:"name"`
	SourceType       string                         `json:"source_type"`
	Endpoint         string                         `json:"endpoint"`
	APIKeyEncrypted  string                         `json:"-"`
	APIKeyMasked     string                         `json:"api_key_masked"`
	UpstreamTargetID *int64                         `json:"upstream_target_id"`
	GroupID          *int64                         `json:"group_id"`
	AccountID        *int64                         `json:"account_id"`
	OAuth            bool                           `json:"oauth"`
	LocalAPIKeyID    *int64                         `json:"-"`
	LocalKeyOwnerID  *int64                         `json:"-"`
	SupplierNote     string                         `json:"supplier_note"`
	GroupNote        string                         `json:"group_note"`
	RateNote         string                         `json:"rate_note"`
	Notes            string                         `json:"notes"`
	APIMode          string                         `json:"api_mode"`
	Enabled          bool                           `json:"enabled"`
	IntervalSeconds  int                            `json:"interval_seconds"`
	TimeoutSeconds   int                            `json:"timeout_seconds"`
	CreatedBy        int64                          `json:"created_by"`
	LastRunAt        *time.Time                     `json:"last_run_at"`
	NextRunAt        *time.Time                     `json:"next_run_at"`
	CreatedAt        time.Time                      `json:"created_at"`
	UpdatedAt        time.Time                      `json:"updated_at"`
	Model            string                         `json:"model"`
	ReasoningEffort  string                         `json:"reasoning_effort"`
	Prompt           string                         `json:"prompt"`
	SourceName       string                         `json:"source_name"`
	RateSnapshot     *UpstreamRemoteBillingSnapshot `json:"rate_snapshot"`
	LatestRun        *IntelligenceMonitorRun        `json:"latest_run"`
	RecentRuns       []*IntelligenceMonitorRun      `json:"recent_runs"`
	AllowWhileBusy   bool                           `json:"-"`
}
type IntelligenceMonitorInput struct {
	Name             *string         `json:"name"`
	SourceType       *string         `json:"source_type"`
	Endpoint         *string         `json:"endpoint"`
	APIKey           *string         `json:"api_key"`
	UpstreamTargetID json.RawMessage `json:"upstream_target_id"`
	GroupID          json.RawMessage `json:"group_id"`
	AccountID        json.RawMessage `json:"account_id"`
	SupplierNote     *string         `json:"supplier_note"`
	GroupNote        *string         `json:"group_note"`
	RateNote         *string         `json:"rate_note"`
	Notes            *string         `json:"notes"`
	APIMode          *string         `json:"api_mode"`
	Enabled          *bool           `json:"enabled"`
	IntervalSeconds  *int            `json:"interval_seconds"`
	TimeoutSeconds   *int            `json:"timeout_seconds"`
}
type IntelligenceMonitorRun struct {
	ID                  int64                          `json:"id"`
	PlanID              int64                          `json:"plan_id"`
	PlanUpdatedAt       time.Time                      `json:"-"`
	PlanName            string                         `json:"plan_name"`
	Status              string                         `json:"status"`
	Trigger             string                         `json:"trigger"`
	Model               string                         `json:"model"`
	ReasoningEffort     string                         `json:"reasoning_effort"`
	Prompt              string                         `json:"prompt"`
	SourceType          string                         `json:"source_type"`
	OAuth               bool                           `json:"oauth"`
	SourceName          string                         `json:"source_name"`
	SourceEndpoint      string                         `json:"source_endpoint"`
	SourceSnapshot      map[string]any                 `json:"source_snapshot"`
	RateSnapshot        *UpstreamRemoteBillingSnapshot `json:"rate_snapshot"`
	NotesSnapshot       map[string]string              `json:"notes_snapshot"`
	APIMode             string                         `json:"api_mode"`
	TimeoutSeconds      int                            `json:"timeout_seconds"`
	RequestKeyEncrypted string                         `json:"-"`
	LeaseToken          string                         `json:"-"`
	StartedAt           *time.Time                     `json:"started_at"`
	FinishedAt          *time.Time                     `json:"finished_at"`
	DurationMs          *int64                         `json:"duration_ms"`
	HTTPStatus          *int                           `json:"http_status"`
	Error               string                         `json:"error"`
	HTML                string                         `json:"html,omitempty"`
	RawText             string                         `json:"raw_text,omitempty"`
	CreatedAt           time.Time                      `json:"created_at"`
}
type IntelligenceMonitorRunQuery struct {
	PlanID   *int64
	Page     int
	PageSize int
}
type IntelligenceMonitorRunPage struct {
	Items    []*IntelligenceMonitorRun `json:"items"`
	Total    int64                     `json:"total"`
	Page     int                       `json:"page"`
	PageSize int                       `json:"page_size"`
}
type IntelligenceMonitorRepository interface {
	SaveOrder(context.Context, IntelligenceOrderInput) error
	ListPlans(context.Context) ([]*IntelligenceMonitorPlan, error)
	GetPlan(context.Context, int64) (*IntelligenceMonitorPlan, error)
	SavePlan(context.Context, *IntelligenceMonitorPlan) error
	ArchivePlan(context.Context, int64) error
	ListRuns(context.Context, IntelligenceMonitorRunQuery) (*IntelligenceMonitorRunPage, error)
	GetRun(context.Context, int64) (*IntelligenceMonitorRun, error)
	Enqueue(context.Context, *IntelligenceMonitorRun, bool) error
	DuePlanIDs(context.Context, int) ([]int64, error)
	ClaimNext(context.Context, string) (*IntelligenceMonitorRun, error)
	CompleteRun(context.Context, *IntelligenceMonitorRun) error
	ExpireRuns(context.Context) error
	PruneRuns(context.Context) error
}
