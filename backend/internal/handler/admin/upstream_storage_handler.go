package admin

import (
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

func (h *UpstreamCenterHandler) StoragePolicy(c *gin.Context) {
	policy, err := h.svc.StoragePolicy(c.Request.Context())
	if response.ErrorFrom(c, err) {
		return
	}
	response.Success(c, policy)
}

func (h *UpstreamCenterHandler) SaveStoragePolicy(c *gin.Context) {
	var in service.UpstreamStoragePolicyInput
	if c.ShouldBindJSON(&in) != nil {
		response.ErrorFrom(c, service.ErrUpstreamStoragePolicy)
		return
	}
	policy, err := h.svc.SaveStoragePolicy(c.Request.Context(), in)
	if response.ErrorFrom(c, err) {
		return
	}
	response.Success(c, policy)
}

func (h *UpstreamCenterHandler) CleanupStorage(c *gin.Context) {
	result, err := h.svc.CleanupStorage(c.Request.Context())
	if response.ErrorFrom(c, err) {
		return
	}
	response.Success(c, result)
}

func (h *UpstreamCenterHandler) StorageArchives(c *gin.Context) {
	page, err := h.svc.ListStorageArchives(c.Request.Context())
	if response.ErrorFrom(c, err) {
		return
	}
	response.Success(c, page)
}

func (h *UpstreamCenterHandler) PurgeStorage(c *gin.Context) {
	var in service.UpstreamStoragePurgeInput
	if c.ShouldBindJSON(&in) != nil {
		response.BadRequest(c, "invalid storage deletion request")
		return
	}
	if response.ErrorFrom(c, h.svc.PurgeStorage(c.Request.Context(), in)) {
		return
	}
	response.Success(c, nil)
}
