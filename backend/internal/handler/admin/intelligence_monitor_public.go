package admin

import (
	"net/http"
	"strconv"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

func (h *IntelligenceMonitorHandler) GetPublicDisplay(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	cfg, err := h.svc.GetPublicDisplay(c.Request.Context())
	if response.ErrorFrom(c, err) {
		return
	}
	response.Success(c, cfg)
}

func (h *IntelligenceMonitorHandler) UpdatePublicDisplay(c *gin.Context) {
	var input struct {
		Enabled     *bool    `json:"enabled" binding:"required"`
		Title       string   `json:"title"`
		Description string   `json:"description"`
		Notice      string   `json:"notice"`
		PlanIDs     *[]int64 `json:"plan_ids" binding:"required"`
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 32*1024)
	if c.ShouldBindJSON(&input) != nil {
		response.BadRequest(c, "invalid pelican display settings")
		return
	}
	cfg, err := h.svc.UpdatePublicDisplay(c.Request.Context(), service.IntelligencePublicDisplay{
		PublicPelicanConfig: service.PublicPelicanConfig{Enabled: *input.Enabled, Title: input.Title, Description: input.Description, Notice: input.Notice}, PlanIDs: *input.PlanIDs,
	})
	if response.ErrorFrom(c, err) {
		return
	}
	c.Header("Cache-Control", "no-store")
	response.Success(c, cfg)
}

func publicPelicanUserID(c *gin.Context) (int64, bool) {
	c.Header("Cache-Control", "no-store")
	subject, ok := middleware.GetAuthSubjectFromContext(c)
	if !ok || subject.UserID <= 0 {
		response.Unauthorized(c, "authentication is required")
		return 0, false
	}
	return subject.UserID, true
}

// The same injected service serves these separate, read-only user handlers.
// Only explicit public DTOs can leave these endpoints, never admin DTOs.
func (h *IntelligenceMonitorHandler) PublicPelicanConfig(c *gin.Context) {
	if _, ok := publicPelicanUserID(c); !ok {
		return
	}
	cfg, err := h.svc.PublicPelicanConfig(c.Request.Context())
	if response.ErrorFrom(c, err) {
		return
	}
	response.Success(c, cfg)
}

func (h *IntelligenceMonitorHandler) ListPublicPelican(c *gin.Context) {
	userID, ok := publicPelicanUserID(c)
	if !ok {
		return
	}
	page, err := h.svc.ListPublicPelican(c.Request.Context(), userID)
	if response.ErrorFrom(c, err) {
		return
	}
	response.Success(c, page)
}

func (h *IntelligenceMonitorHandler) GetPublicPelicanRun(c *gin.Context) {
	userID, ok := publicPelicanUserID(c)
	if !ok {
		return
	}
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		response.ErrorFrom(c, service.ErrIntelligenceNotFound)
		return
	}
	run, err := h.svc.GetPublicPelicanRun(c.Request.Context(), userID, id)
	if response.ErrorFrom(c, err) {
		return
	}
	response.Success(c, run)
}
