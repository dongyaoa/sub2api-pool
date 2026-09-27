package admin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type intelligenceUpstreamHandlerRepo struct {
	service.IntelligenceMonitorRepository
	filteredID int64
	allCalls   int
}

func (r *intelligenceUpstreamHandlerRepo) ListPlans(context.Context) ([]*service.IntelligenceMonitorPlan, error) {
	r.allCalls++
	return []*service.IntelligenceMonitorPlan{}, nil
}

func (r *intelligenceUpstreamHandlerRepo) ListPlansForUpstream(_ context.Context, id int64) ([]*service.IntelligenceMonitorPlan, error) {
	r.filteredID = id
	return []*service.IntelligenceMonitorPlan{}, nil
}

func (r *intelligenceUpstreamHandlerRepo) SavePlan(context.Context, *service.IntelligenceMonitorPlan) error {
	return service.ErrIntelligenceUpstreamPlanExists
}

type intelligenceUpstreamHandlerSources struct {
	service.UpstreamCenterRepository
}

func (intelligenceUpstreamHandlerSources) GetTarget(context.Context, int64) (*service.UpstreamTarget, error) {
	return &service.UpstreamTarget{ID: 27, Name: "Selected group", Provider: service.MonitorProviderOpenAI}, nil
}

func TestIntelligenceUpstreamPlansHandlerFilterValidation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &intelligenceUpstreamHandlerRepo{}
	h := NewIntelligenceMonitorHandler(service.NewIntelligenceMonitorService(repo, nil, nil, nil, nil, nil, nil))
	r := gin.New()
	r.GET("/plans", h.ListPlans)
	for _, query := range []string{"", "0", "-1", "1.2", "bad", "9223372036854775808", "1&upstream_target_id=2"} {
		out := httptest.NewRecorder()
		r.ServeHTTP(out, httptest.NewRequest(http.MethodGet, "/plans?upstream_target_id="+query, nil))
		require.Equal(t, http.StatusBadRequest, out.Code, query)
		require.Equal(t, "no-store", out.Header().Get("Cache-Control"))
	}
	require.Zero(t, repo.filteredID)
	require.Zero(t, repo.allCalls)
	out := httptest.NewRecorder()
	r.ServeHTTP(out, httptest.NewRequest(http.MethodGet, "/plans?upstream_target_id=27", nil))
	require.Equal(t, http.StatusOK, out.Code)
	require.Contains(t, out.Body.String(), `"items":[]`)
	require.Equal(t, int64(27), repo.filteredID)
	require.Zero(t, repo.allCalls)
	out = httptest.NewRecorder()
	r.ServeHTTP(out, httptest.NewRequest(http.MethodGet, "/plans", nil))
	require.Equal(t, http.StatusOK, out.Code)
	require.Equal(t, 1, repo.allCalls, "existing unfiltered API stays compatible")
}

func TestIntelligenceUpstreamPlansHandlerConflictCode(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewIntelligenceMonitorHandler(service.NewIntelligenceMonitorService(&intelligenceUpstreamHandlerRepo{}, nil, intelligenceUpstreamHandlerSources{}, nil, nil, nil, nil))
	r := gin.New()
	r.POST("/plans", func(c *gin.Context) {
		c.Set(string(middleware2.ContextKeyUser), middleware2.AuthSubject{UserID: 1})
		h.CreatePlan(c)
	})
	request := httptest.NewRequest(http.MethodPost, "/plans", strings.NewReader(`{"name":"Selected group","source_type":"upstream","upstream_target_id":27}`))
	request.Header.Set("Content-Type", "application/json")
	out := httptest.NewRecorder()
	r.ServeHTTP(out, request)
	require.Equal(t, http.StatusConflict, out.Code)
	require.Contains(t, out.Body.String(), "INTELLIGENCE_UPSTREAM_PLAN_EXISTS")
}
