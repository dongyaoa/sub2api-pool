package admin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type intelligenceLifecycleHandlerRepo struct {
	service.IntelligenceMonitorRepository
	service.IntelligenceMonitorLifecycleRepository
	deletedRun, deletedPlan int64
	bulkCalls               int
	enabled                 bool
}

func (r *intelligenceLifecycleHandlerRepo) DeletePelicanRun(_ context.Context, id int64) error {
	r.deletedRun = id
	return nil
}
func (r *intelligenceLifecycleHandlerRepo) DeleteOAuthPlanPermanently(_ context.Context, id int64) error {
	r.deletedPlan = id
	return nil
}
func (r *intelligenceLifecycleHandlerRepo) ScheduleStatus(context.Context) (*service.IntelligenceScheduleStatus, error) {
	return &service.IntelligenceScheduleStatus{Total: 3, Enabled: 1}, nil
}
func (r *intelligenceLifecycleHandlerRepo) SetPlansEnabled(_ context.Context, enabled bool) (*service.IntelligenceScheduleUpdate, error) {
	r.bulkCalls++
	r.enabled = enabled
	return &service.IntelligenceScheduleUpdate{Updated: 2}, nil
}

func TestIntelligenceLifecycleHandlerRoutesAndValidation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &intelligenceLifecycleHandlerRepo{}
	h := NewIntelligenceMonitorHandler(service.NewIntelligenceMonitorService(repo, nil, nil, nil, nil, nil, nil))
	r := gin.New()
	r.DELETE("/runs/:id", h.DeletePelicanRun)
	r.DELETE("/plans/:id/permanent", h.DeleteOAuthPlanPermanently)
	r.GET("/plans/schedule-status", h.ScheduleStatus)
	r.PUT("/plans/enabled", h.SetPlansEnabled)
	for _, body := range []string{`{}`, `{"enabled":null}`, `{"enabled":"false"}`, `{"enabled":0}`} {
		out := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPut, "/plans/enabled", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(out, req)
		require.Equal(t, http.StatusBadRequest, out.Code)
	}
	require.Zero(t, repo.bulkCalls)
	for _, enabled := range []string{"false", "true"} {
		out := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPut, "/plans/enabled", strings.NewReader(`{"enabled":`+enabled+`}`))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(out, req)
		require.Equal(t, http.StatusOK, out.Code)
		require.Equal(t, enabled == "true", repo.enabled)
		require.Contains(t, out.Body.String(), `"updated":2`)
	}
	for _, path := range []string{"/runs/0", "/plans/-1/permanent"} {
		out := httptest.NewRecorder()
		r.ServeHTTP(out, httptest.NewRequest(http.MethodDelete, path, nil))
		require.Equal(t, http.StatusBadRequest, out.Code)
	}
	for _, path := range []string{"/runs/7", "/plans/9/permanent"} {
		out := httptest.NewRecorder()
		r.ServeHTTP(out, httptest.NewRequest(http.MethodDelete, path, nil))
		require.Equal(t, http.StatusOK, out.Code)
	}
	require.Equal(t, int64(7), repo.deletedRun)
	require.Equal(t, int64(9), repo.deletedPlan)
	out := httptest.NewRecorder()
	r.ServeHTTP(out, httptest.NewRequest(http.MethodGet, "/plans/schedule-status", nil))
	require.Equal(t, http.StatusOK, out.Code)
	require.Equal(t, "no-store", out.Header().Get("Cache-Control"))
	require.Contains(t, out.Body.String(), `"total":3`)
}
