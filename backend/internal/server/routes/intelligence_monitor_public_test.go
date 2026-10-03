package routes

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/handler"
	adminhandler "github.com/Wei-Shaw/sub2api/internal/handler/admin"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestIntelligencePublicPelicanRoutesRequireJWTAndStayReadOnly(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	h := &handler.Handlers{Admin: &handler.AdminHandlers{IntelligenceMonitor: adminhandler.NewIntelligenceMonitorHandler(nil)}}
	jwtCalls := 0
	jwtAuth := middleware.JWTAuthMiddleware(func(c *gin.Context) {
		jwtCalls++
		c.AbortWithStatus(http.StatusUnauthorized)
	})
	auditLog := middleware.AuditLogMiddleware(func(c *gin.Context) { c.Next() })
	RegisterUserRoutes(router.Group("/api/v1"), h, jwtAuth, auditLog, nil, nil)

	for _, path := range []string{"/api/v1/pelican-monitor/config", "/api/v1/pelican-monitor", "/api/v1/pelican-monitor/runs/7"} {
		out := httptest.NewRecorder()
		router.ServeHTTP(out, httptest.NewRequest(http.MethodGet, path, nil))
		require.Equal(t, http.StatusUnauthorized, out.Code, path)
	}
	require.Equal(t, 3, jwtCalls)
	for _, tc := range []struct{ method, path string }{
		{http.MethodPost, "/api/v1/pelican-monitor"},
		{http.MethodPut, "/api/v1/pelican-monitor/config"},
		{http.MethodDelete, "/api/v1/pelican-monitor/runs/7"},
		{http.MethodGet, "/api/v1/public/pelican-monitor"},
		{http.MethodGet, "/api/v1/public/pelican-monitor/config"},
	} {
		out := httptest.NewRecorder()
		router.ServeHTTP(out, httptest.NewRequest(tc.method, tc.path, nil))
		require.Equal(t, http.StatusNotFound, out.Code, tc.method+" "+tc.path)
	}
}

func TestIntelligencePublicDisplayRoutesRequireAdmin(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	h := &handler.Handlers{Admin: &handler.AdminHandlers{IntelligenceMonitor: adminhandler.NewIntelligenceMonitorHandler(nil)}}
	adminAuth := middleware.AdminAuthMiddleware(func(c *gin.Context) {
		if c.GetHeader("Authorization") == "" {
			c.AbortWithStatus(http.StatusUnauthorized)
			return
		}
		c.AbortWithStatus(http.StatusForbidden)
	})
	auditLog := middleware.AuditLogMiddleware(func(c *gin.Context) { c.Next() })
	stepUp := middleware.StepUpAuthMiddleware(func(c *gin.Context) { c.Next() })
	RegisterAdminRoutes(router.Group("/api/v1"), h, adminAuth, auditLog, stepUp, nil, nil)

	for _, method := range []string{http.MethodGet, http.MethodPut} {
		for _, authenticated := range []bool{false, true} {
			out := httptest.NewRecorder()
			req := httptest.NewRequest(method, "/api/v1/admin/intelligence-monitors/public-display", nil)
			status := http.StatusUnauthorized
			if authenticated {
				req.Header.Set("Authorization", "Bearer ordinary-user")
				status = http.StatusForbidden
			}
			router.ServeHTTP(out, req)
			require.Equal(t, status, out.Code, method)
		}
	}
	for _, route := range []struct{ method, path string }{
		{http.MethodDelete, "/runs/7"},
		{http.MethodDelete, "/plans/7/permanent"},
		{http.MethodPut, "/plans/enabled"},
		{http.MethodGet, "/plans/schedule-status"},
	} {
		for _, authenticated := range []bool{false, true} {
			out := httptest.NewRecorder()
			req := httptest.NewRequest(route.method, "/api/v1/admin/intelligence-monitors"+route.path, nil)
			status := http.StatusUnauthorized
			if authenticated {
				req.Header.Set("Authorization", "Bearer ordinary-user")
				status = http.StatusForbidden
			}
			router.ServeHTTP(out, req)
			require.Equal(t, status, out.Code, route.method+" "+route.path)
		}
	}
}
