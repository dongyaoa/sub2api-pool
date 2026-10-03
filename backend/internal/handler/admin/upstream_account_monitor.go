package admin

import (
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/gin-gonic/gin"
)

func (h *UpstreamCenterHandler) AccountMonitor(c *gin.Context) { h.accountMonitor(c, false) }

func (h *UpstreamCenterHandler) EnsureAccountMonitor(c *gin.Context) { h.accountMonitor(c, true) }

func (h *UpstreamCenterHandler) accountMonitor(c *gin.Context, ensure bool) {
	c.Header("Cache-Control", "no-store")
	id, ok := parseUpstreamID(c)
	if !ok {
		return
	}
	result, err := h.svc.AccountMonitor(c.Request.Context(), id, ensure)
	if response.ErrorFrom(c, err) {
		return
	}
	response.Success(c, result)
}
