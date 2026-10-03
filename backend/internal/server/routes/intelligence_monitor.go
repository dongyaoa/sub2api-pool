package routes

import (
	"github.com/Wei-Shaw/sub2api/internal/handler"
	"github.com/gin-gonic/gin"
)

func registerIntelligenceMonitorRoutes(admin *gin.RouterGroup, h *handler.Handlers) {
	if h == nil || h.Admin == nil || h.Admin.IntelligenceMonitor == nil {
		return
	}
	api := h.Admin.IntelligenceMonitor
	group := admin.Group("/intelligence-monitors")
	group.GET("/concurrency", api.GetConcurrency)
	group.PUT("/concurrency", api.UpdateConcurrency)
	group.GET("/public-display", api.GetPublicDisplay)
	group.PUT("/public-display", api.UpdatePublicDisplay)
	group.GET("/plans", api.ListPlans)
	group.POST("/plans", api.CreatePlan)
	group.PUT("/plans/order", api.SaveOrder)
	group.GET("/plans/schedule-status", api.ScheduleStatus)
	group.PUT("/plans/enabled", api.SetPlansEnabled)
	group.PUT("/plans/:id", api.UpdatePlan)
	group.DELETE("/plans/:id", api.DeletePlan)
	group.DELETE("/plans/:id/permanent", api.DeleteOAuthPlanPermanently)
	group.POST("/plans/:id/run", api.Run)
	group.POST("/plans/:id/candy/run", api.RunCandy)
	group.GET("/runs", api.ListRuns)
	group.GET("/runs/:id", api.GetRun)
	group.DELETE("/runs/:id", api.DeletePelicanRun)
}

// Register only beneath the authenticated user router, never public routes.
func registerPublicPelicanRoutes(authenticated *gin.RouterGroup, h *handler.Handlers) {
	if h == nil || h.Admin == nil || h.Admin.IntelligenceMonitor == nil {
		return
	}
	api := h.Admin.IntelligenceMonitor
	group := authenticated.Group("/pelican-monitor")
	group.GET("/config", api.PublicPelicanConfig)
	group.GET("", api.ListPublicPelican)
	group.GET("/runs/:id", api.GetPublicPelicanRun)
}
