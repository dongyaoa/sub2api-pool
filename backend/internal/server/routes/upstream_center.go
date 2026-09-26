package routes

import (
	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/gin-gonic/gin"
)

// registerUpstreamCenterRoutes is deliberately registered only beneath the
// authenticated administrator group. Neither public status pages nor the
// channel-monitor V1/V2 mode controls these independent probes.
func registerUpstreamCenterRoutes(admin *gin.RouterGroup, h *handler.Handlers) {
	if h == nil || h.Admin == nil || h.Admin.UpstreamCenter == nil {
		return
	}
	center := admin.Group("/upstream-center")
	api := h.Admin.UpstreamCenter
	center.GET("/overview", api.Overview)
	center.GET("/storage", api.StoragePolicy)
	center.PUT("/storage", api.SaveStoragePolicy)
	center.POST("/storage/cleanup", api.CleanupStorage)
	center.GET("/storage/archives", api.StorageArchives)
	center.POST("/storage/purge", api.PurgeStorage)
	center.PUT("/order", api.SaveOrder)
	center.POST("/suppliers", api.CreateSupplier)
	center.PUT("/suppliers/:id", api.UpdateSupplier)
	center.DELETE("/suppliers/:id", api.DeleteSupplier)
	center.POST("/targets", api.CreateTarget)
	center.PUT("/targets/:id", api.UpdateTarget)
	center.DELETE("/targets/:id", api.DeleteTarget)
	center.POST("/targets/:id/run", api.Run)
	center.POST("/targets/:id/sync-balance", api.SyncBalance)
	center.GET("/targets/:id/history", api.History)
	center.POST("/models", api.Models)
	center.GET("/finance", api.Finance)
}
