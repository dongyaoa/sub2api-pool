package admin

import (
	"strconv"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

type IntelligenceMonitorHandler struct {
	svc *service.IntelligenceMonitorService
}

func NewIntelligenceMonitorHandler(svc *service.IntelligenceMonitorService) *IntelligenceMonitorHandler {
	return &IntelligenceMonitorHandler{svc: svc}
}
func (h *IntelligenceMonitorHandler) GetConcurrency(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	settings, err := h.svc.GetConcurrency(c.Request.Context())
	if response.ErrorFrom(c, err) {
		return
	}
	response.Success(c, settings)
}
func (h *IntelligenceMonitorHandler) UpdateConcurrency(c *gin.Context) {
	var input struct {
		MaxConcurrency      *int `json:"max_concurrency" binding:"required,min=1,max=256"`
		CandyMaxConcurrency *int `json:"candy_max_concurrency" binding:"required,min=1,max=128"`
	}
	if c.ShouldBindJSON(&input) != nil {
		response.BadRequest(c, "invalid intelligence monitor concurrency: pelican must be 1-256 and candy must be 1-128")
		return
	}
	settings, err := h.svc.UpdateConcurrency(c.Request.Context(), service.IntelligenceMonitorConcurrency{
		MaxConcurrency: *input.MaxConcurrency, CandyMaxConcurrency: *input.CandyMaxConcurrency,
	})
	if response.ErrorFrom(c, err) {
		return
	}
	c.Header("Cache-Control", "no-store")
	response.Success(c, settings)
}
func (h *IntelligenceMonitorHandler) ListPlans(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	var items []*service.IntelligenceMonitorPlan
	var err error
	query := c.Request.URL.Query()
	upstreamValues, upstreamFilter := query["upstream_target_id"]
	accountValues, accountFilter := query["account_id"]
	if upstreamFilter && accountFilter {
		response.BadRequest(c, "upstream_target_id and account_id filters cannot be combined")
		return
	}
	if upstreamFilter || accountFilter {
		values, label := upstreamValues, "upstream target ID"
		if accountFilter {
			values, label = accountValues, "account ID"
		}
		if len(values) != 1 {
			response.BadRequest(c, "invalid "+label)
			return
		}
		id, parseErr := strconv.ParseInt(values[0], 10, 64)
		if parseErr != nil || id <= 0 {
			response.BadRequest(c, "invalid "+label)
			return
		}
		if accountFilter {
			items, err = h.svc.ListPlansForAccount(c.Request.Context(), id)
		} else {
			items, err = h.svc.ListPlansForUpstream(c.Request.Context(), id)
		}
	} else {
		items, err = h.svc.ListPlans(c.Request.Context())
	}
	if response.ErrorFrom(c, err) {
		return
	}
	response.Success(c, gin.H{"items": items})
}
func (h *IntelligenceMonitorHandler) SaveOrder(c *gin.Context) {
	var in service.IntelligenceOrderInput
	if c.ShouldBindJSON(&in) != nil {
		response.ErrorFrom(c, service.ErrManualOrderInvalid)
		return
	}
	if response.ErrorFrom(c, h.svc.SaveOrder(c.Request.Context(), in)) {
		return
	}
	response.Success(c, nil)
}
func (h *IntelligenceMonitorHandler) CreatePlan(c *gin.Context) { h.save(c, 0) }
func (h *IntelligenceMonitorHandler) UpdatePlan(c *gin.Context) {
	if id, ok := parseIntelligenceID(c); ok {
		h.save(c, id)
	}
}
func (h *IntelligenceMonitorHandler) save(c *gin.Context, id int64) {
	actor, ok := middleware2.GetAuthSubjectFromContext(c)
	if !ok || actor.UserID <= 0 {
		response.Unauthorized(c, "administrator identity is required")
		return
	}
	var input service.IntelligenceMonitorInput
	if c.ShouldBindJSON(&input) != nil {
		response.BadRequest(c, "invalid intelligence monitoring configuration")
		return
	}
	plan, err := h.svc.SavePlan(c.Request.Context(), id, actor.UserID, input)
	if response.ErrorFrom(c, err) {
		return
	}
	response.Success(c, plan)
}
func (h *IntelligenceMonitorHandler) DeletePlan(c *gin.Context) {
	id, ok := parseIntelligenceID(c)
	if !ok {
		return
	}
	if response.ErrorFrom(c, h.svc.DeletePlan(c.Request.Context(), id)) {
		return
	}
	response.Success(c, nil)
}
func (h *IntelligenceMonitorHandler) Run(c *gin.Context) {
	id, ok := parseIntelligenceID(c)
	if !ok {
		return
	}
	run, err := h.svc.Enqueue(c.Request.Context(), id)
	if response.ErrorFrom(c, err) {
		return
	}
	response.Accepted(c, run)
}
func (h *IntelligenceMonitorHandler) ListRuns(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	var planID *int64
	if raw := c.Query("plan_id"); raw != "" {
		id, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || id <= 0 {
			response.BadRequest(c, "invalid plan ID")
			return
		}
		planID = &id
	}
	page, size := upstreamPagination(c)
	runs, err := h.svc.ListRuns(c.Request.Context(), service.IntelligenceMonitorRunQuery{PlanID: planID, TestKind: c.Query("test_kind"), Page: page, PageSize: size})
	if response.ErrorFrom(c, err) {
		return
	}
	response.Success(c, runs)
}
func (h *IntelligenceMonitorHandler) RunCandy(c *gin.Context) {
	id, ok := parseIntelligenceID(c)
	if !ok {
		return
	}
	run, err := h.svc.EnqueueCandy(c.Request.Context(), id)
	if response.ErrorFrom(c, err) {
		return
	}
	response.Accepted(c, run)
}
func (h *IntelligenceMonitorHandler) GetRun(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	id, ok := parseIntelligenceID(c)
	if !ok {
		return
	}
	run, err := h.svc.GetRun(c.Request.Context(), id)
	if response.ErrorFrom(c, err) {
		return
	}
	response.Success(c, run)
}
func parseIntelligenceID(c *gin.Context) (int64, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.BadRequest(c, "invalid intelligence monitoring ID")
		return 0, false
	}
	return id, true
}
