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
const IntelligenceMonitorTestPelican = "pelican"
const IntelligenceMonitorTestCandy = "candy"
const IntelligenceMonitorCandyRetainedRuns = 60
const IntelligenceMonitorCandyMaxOutputTokens = 24000
const IntelligenceMonitorCandyDefaultIntervalSeconds = 180

// The expected answer belongs only to the evaluator, never the model's input.
const IntelligenceMonitorCandyPrompt = `不使用任何外部工具回答以下问题：

在一个黑色的袋子里放有三种口味的糖果，每种糖果有两种不同的形状（圆形和五角星形，不同的形状靠手感可以分辨）。现已知不同口味的糖和不同形状的数量统计如下表。参赛者需要在活动前决定摸出的糖果数目，那么，最少取出多少个糖果才能保证手中同时拥有不同形状的苹果味和桃子味的糖？（同时手中有圆形苹果味匹配五角星桃子味糖果，或者有圆形桃子味匹配五角星苹果味糖果都满足要求）

           苹果味  桃子味  西瓜味
圆形          7       9       8
五角星形      7       6       4`
const IntelligenceMonitorDefaultTimeoutSeconds = 600
const IntelligenceMonitorDefaultIntervalSeconds = 300
const IntelligenceMonitorMinTimeoutSeconds = 180
const IntelligenceMonitorMaxTimeoutSeconds = 900

// Covers source preparation, result persistence and scheduler delay after the
// generation deadline. A claimed run keeps its own captured timeout budget.
const IntelligenceMonitorLeaseGraceSeconds = 180

var (
	ErrIntelligenceNotFound           = infraerrors.NotFound("INTELLIGENCE_MONITOR_NOT_FOUND", "intelligence monitoring plan or run not found")
	ErrIntelligenceInvalid            = infraerrors.BadRequest("INTELLIGENCE_MONITOR_INVALID", "invalid intelligence monitoring configuration")
	ErrIntelligenceBusy               = infraerrors.Conflict("INTELLIGENCE_MONITOR_BUSY", "this plan has a pending or running generation; wait for completion")
	ErrIntelligenceUpstreamPlanExists = infraerrors.Conflict("INTELLIGENCE_UPSTREAM_PLAN_EXISTS", "this upstream group already has an intelligence monitoring plan")
	ErrIntelligenceOAuthPlanExists    = infraerrors.Conflict("INTELLIGENCE_OAUTH_PLAN_EXISTS", "this OAuth account already has an intelligence monitoring plan")
	ErrIntelligenceOAuthCoolingDown   = infraerrors.Conflict("INTELLIGENCE_OAUTH_COOLING_DOWN", "OAuth monitoring is paused until the weekly quota recovers")
	ErrIntelligenceOAuthUnavailable   = infraerrors.Conflict("INTELLIGENCE_OAUTH_UNAVAILABLE", "OAuth monitoring is paused until the account becomes available")
	ErrIntelligenceFilterUnavailable  = infraerrors.ServiceUnavailable("INTELLIGENCE_MONITOR_FILTER_UNAVAILABLE", "filtered intelligence monitoring is unavailable")
)

type IntelligenceMonitorPlan struct {
	ID                       int64                           `json:"id"`
	Name                     string                          `json:"name"`
	SourceType               string                          `json:"source_type"`
	Endpoint                 string                          `json:"endpoint"`
	APIKeyEncrypted          string                          `json:"-"`
	APIKeyMasked             string                          `json:"api_key_masked"`
	UpstreamTargetID         *int64                          `json:"upstream_target_id"`
	GroupID                  *int64                          `json:"group_id"`
	AccountID                *int64                          `json:"account_id"`
	OAuth                    bool                            `json:"oauth"`
	OAuthAccountStatus       *IntelligenceOAuthAccountStatus `json:"oauth_account_status,omitempty"`
	LocalAPIKeyID            *int64                          `json:"local_api_key_id"`
	LocalKeyOwnerID          *int64                          `json:"-"`
	LocalAPIKeyBorrowed      bool                            `json:"-"`
	LocalAPIKeyManaged       bool                            `json:"local_api_key_managed"`
	LocalAPIKeyName          string                          `json:"local_api_key_name"`
	LocalGroupName           string                          `json:"local_group_name"`
	LocalGroupRateMultiplier *float64                        `json:"local_group_rate_multiplier"`
	LocalGroupStatus         string                          `json:"local_group_status"`
	SupplierNote             string                          `json:"supplier_note"`
	GroupNote                string                          `json:"group_note"`
	RateNote                 string                          `json:"rate_note"`
	Notes                    string                          `json:"notes"`
	APIMode                  string                          `json:"api_mode"`
	Enabled                  bool                            `json:"enabled"`
	CandyEnabled             bool                            `json:"candy_enabled"`
	CandyIntervalSeconds     int                             `json:"candy_interval_seconds"`
	CandyLastRunAt           *time.Time                      `json:"candy_last_run_at"`
	CandyNextRunAt           *time.Time                      `json:"candy_next_run_at"`
	IntervalSeconds          int                             `json:"interval_seconds"`
	TimeoutSeconds           int                             `json:"timeout_seconds"`
	CreatedBy                int64                           `json:"created_by"`
	LastRunAt                *time.Time                      `json:"last_run_at"`
	NextRunAt                *time.Time                      `json:"next_run_at"`
	CreatedAt                time.Time                       `json:"created_at"`
	UpdatedAt                time.Time                       `json:"updated_at"`
	Model                    string                          `json:"model"`
	ReasoningEffort          string                          `json:"reasoning_effort"`
	Prompt                   string                          `json:"prompt"`
	SourceName               string                          `json:"source_name"`
	RateSnapshot             *UpstreamRemoteBillingSnapshot  `json:"rate_snapshot"`
	LatestRun                *IntelligenceMonitorRun         `json:"latest_run"`
	RecentRuns               []*IntelligenceMonitorRun       `json:"recent_runs"`
	CandyLatestRun           *IntelligenceMonitorRun         `json:"candy_latest_run"`
	CandyRecentRuns          []*IntelligenceMonitorRun       `json:"candy_recent_runs"`
	AllowWhileBusy           bool                            `json:"-"`
}
type IntelligenceMonitorInput struct {
	Name                 *string         `json:"name"`
	SourceType           *string         `json:"source_type"`
	Endpoint             *string         `json:"endpoint"`
	APIKey               *string         `json:"api_key"`
	UpstreamTargetID     json.RawMessage `json:"upstream_target_id"`
	GroupID              json.RawMessage `json:"group_id"`
	AccountID            json.RawMessage `json:"account_id"`
	LocalAPIKeyID        json.RawMessage `json:"local_api_key_id"`
	SupplierNote         *string         `json:"supplier_note"`
	GroupNote            *string         `json:"group_note"`
	RateNote             *string         `json:"rate_note"`
	Notes                *string         `json:"notes"`
	APIMode              *string         `json:"api_mode"`
	Enabled              *bool           `json:"enabled"`
	CandyEnabled         *bool           `json:"candy_enabled"`
	CandyIntervalSeconds *int            `json:"candy_interval_seconds"`
	IntervalSeconds      *int            `json:"interval_seconds"`
	TimeoutSeconds       *int            `json:"timeout_seconds"`
}
type IntelligenceMonitorRun struct {
	ID                  int64                          `json:"id"`
	PlanID              int64                          `json:"plan_id"`
	TestKind            string                         `json:"test_kind"`
	Correct             *bool                          `json:"correct"`
	Answer              string                         `json:"answer"`
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
	TestKind string
	Page     int
	PageSize int
}
type IntelligenceMonitorRunPage struct {
	Items    []*IntelligenceMonitorRun `json:"items"`
	Total    int64                     `json:"total"`
	Page     int                       `json:"page"`
	PageSize int                       `json:"page_size"`
}

// Optional batched read capability for the frequently refreshed plan gallery.
// SourceNames contains only existing, non-deleted source records. Runs contains
// at most twenty terminal runs and one active run per requested plan.
type IntelligenceMonitorPlanListData struct {
	Runs        map[int64][]*IntelligenceMonitorRun
	CandyRuns   map[int64][]*IntelligenceMonitorRun
	SourceNames map[int64]string
}

type IntelligenceMonitorListRepository interface {
	LoadPlanListData(context.Context, []int64) (*IntelligenceMonitorPlanListData, error)
}

type IntelligenceMonitorUpstreamListRepository interface {
	ListPlansForUpstream(context.Context, int64) ([]*IntelligenceMonitorPlan, error)
}

type IntelligenceMonitorOAuthListRepository interface {
	ListPlansForAccount(context.Context, int64) ([]*IntelligenceMonitorPlan, error)
}

// Optional so existing repository decorators retain their original contract.
type IntelligenceMonitorCandyScheduleRepository interface {
	DueCandyPlanIDs(context.Context, int) ([]int64, error)
}

// Separate durable queues keep long artwork requests from consuming all candy
// capacity. Implementations enforce each limit across app instances and allow
// both kinds for a plan to execute together, with one active run per kind.
type IntelligenceMonitorQueueRepository interface {
	ClaimNextForKind(context.Context, string, string, int) (*IntelligenceMonitorRun, error)
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
