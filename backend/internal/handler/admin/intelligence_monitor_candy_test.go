package admin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type intelligenceCandyHandlerRepo struct {
	service.IntelligenceMonitorRepository
	enabled bool
	queued  *service.IntelligenceMonitorRun
	queries []service.IntelligenceMonitorRunQuery
}

func (r *intelligenceCandyHandlerRepo) GetPlan(context.Context, int64) (*service.IntelligenceMonitorPlan, error) {
	return &service.IntelligenceMonitorPlan{ID: 1, Name: "Test", SourceType: "external", APIKeyEncrypted: "encrypted-secret", CandyEnabled: r.enabled}, nil
}

func (r *intelligenceCandyHandlerRepo) Enqueue(_ context.Context, run *service.IntelligenceMonitorRun, _ bool) error {
	run.ID, run.Status = 10, "pending"
	r.queued = run
	return nil
}

func (r *intelligenceCandyHandlerRepo) ListRuns(_ context.Context, query service.IntelligenceMonitorRunQuery) (*service.IntelligenceMonitorRunPage, error) {
	r.queries = append(r.queries, query)
	return &service.IntelligenceMonitorRunPage{Items: []*service.IntelligenceMonitorRun{}, Page: query.Page, PageSize: query.PageSize}, nil
}

func TestIntelligenceCandyHandlerValidatesOptInAndNeverReturnsCredentials(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &intelligenceCandyHandlerRepo{}
	h := NewIntelligenceMonitorHandler(service.NewIntelligenceMonitorService(repo, nil, nil, nil, nil, nil, nil))
	r := gin.New()
	r.POST("/plans/:id/candy/run", h.RunCandy)
	for _, id := range []string{"0", "-1", "no"} {
		out := httptest.NewRecorder()
		r.ServeHTTP(out, httptest.NewRequest(http.MethodPost, "/plans/"+id+"/candy/run", nil))
		require.Equal(t, http.StatusBadRequest, out.Code)
	}
	out := httptest.NewRecorder()
	r.ServeHTTP(out, httptest.NewRequest(http.MethodPost, "/plans/1/candy/run", nil))
	require.Equal(t, http.StatusBadRequest, out.Code)
	require.Nil(t, repo.queued)
	repo.enabled = true
	out = httptest.NewRecorder()
	r.ServeHTTP(out, httptest.NewRequest(http.MethodPost, "/plans/1/candy/run", nil))
	require.Equal(t, http.StatusAccepted, out.Code)
	require.Equal(t, service.IntelligenceMonitorTestCandy, repo.queued.TestKind)
	require.Equal(t, service.IntelligenceMonitorCandyPrompt, repo.queued.Prompt)
	require.NotContains(t, out.Body.String(), "encrypted-secret")
	require.Contains(t, out.Body.String(), `"test_kind":"candy"`)
}

func TestIntelligenceCandyHistoryFilterKeepsOriginalArtworkAPI(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &intelligenceCandyHandlerRepo{}
	h := NewIntelligenceMonitorHandler(service.NewIntelligenceMonitorService(repo, nil, nil, nil, nil, nil, nil))
	r := gin.New()
	r.GET("/runs", h.ListRuns)
	for _, kind := range []string{"", "pelican", "candy", "arbitrary"} {
		out := httptest.NewRecorder()
		r.ServeHTTP(out, httptest.NewRequest(http.MethodGet, "/runs?plan_id=1&test_kind="+kind, nil))
		if kind == "arbitrary" {
			require.Equal(t, http.StatusBadRequest, out.Code)
			continue
		}
		require.Equal(t, http.StatusOK, out.Code)
		require.Equal(t, "no-store", out.Header().Get("Cache-Control"))
		expected := kind
		if expected == "" {
			expected = "pelican"
		}
		require.Equal(t, expected, repo.queries[len(repo.queries)-1].TestKind)
	}
	require.Len(t, repo.queries, 3)
}
