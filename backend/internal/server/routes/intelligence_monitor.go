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
	group.GET("/plans", api.ListPlans)
	group.POST("/plans", api.CreatePlan)
	group.PUT("/plans/order", api.SaveOrder)
	group.PUT("/plans/:id", api.UpdatePlan)
	group.DELETE("/plans/:id", api.DeletePlan)
	group.POST("/plans/:id/run", api.Run)
	group.GET("/runs", api.ListRuns)
	group.GET("/runs/:id", api.GetRun)
}
