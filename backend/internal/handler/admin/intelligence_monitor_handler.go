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
func (h *IntelligenceMonitorHandler) ListPlans(c *gin.Context) {
	items, err := h.svc.ListPlans(c.Request.Context())
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
	runs, err := h.svc.ListRuns(c.Request.Context(), service.IntelligenceMonitorRunQuery{PlanID: planID, Page: page, PageSize: size})
	if response.ErrorFrom(c, err) {
		return
	}
	response.Success(c, runs)
}
func (h *IntelligenceMonitorHandler) GetRun(c *gin.Context) {
	id, ok := parseIntelligenceID(c)
	if !ok {
		return
	}
	run, err := h.svc.GetRun(c.Request.Context(), id)
	if response.ErrorFrom(c, err) {
		return
	}
	c.Header("Cache-Control", "no-store")
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
