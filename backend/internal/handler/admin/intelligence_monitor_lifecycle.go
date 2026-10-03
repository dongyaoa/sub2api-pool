package admin

import (
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/gin-gonic/gin"
)

func (h *IntelligenceMonitorHandler) DeletePelicanRun(c *gin.Context) {
	id, ok := parseIntelligenceID(c)
	if !ok {
		return
	}
	if response.ErrorFrom(c, h.svc.DeletePelicanRun(c.Request.Context(), id)) {
		return
	}
	response.Success(c, nil)
}

func (h *IntelligenceMonitorHandler) DeleteOAuthPlanPermanently(c *gin.Context) {
	id, ok := parseIntelligenceID(c)
	if !ok {
		return
	}
	if response.ErrorFrom(c, h.svc.DeleteOAuthPlanPermanently(c.Request.Context(), id)) {
		return
	}
	response.Success(c, nil)
}

func (h *IntelligenceMonitorHandler) ScheduleStatus(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	status, err := h.svc.ScheduleStatus(c.Request.Context())
	if response.ErrorFrom(c, err) {
		return
	}
	response.Success(c, status)
}

func (h *IntelligenceMonitorHandler) SetPlansEnabled(c *gin.Context) {
	var input struct {
		Enabled *bool `json:"enabled" binding:"required"`
	}
	if c.ShouldBindJSON(&input) != nil {
		response.BadRequest(c, "enabled must be a boolean")
		return
	}
	status, err := h.svc.SetPlansEnabled(c.Request.Context(), *input.Enabled)
	if response.ErrorFrom(c, err) {
		return
	}
	c.Header("Cache-Control", "no-store")
	response.Success(c, status)
}
