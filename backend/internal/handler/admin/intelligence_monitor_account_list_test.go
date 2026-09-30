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

type intelligenceAccountHandlerRepo struct {
	intelligenceUpstreamHandlerRepo
	accountID int64
}

func (r *intelligenceAccountHandlerRepo) ListPlansForAccount(_ context.Context, id int64) ([]*service.IntelligenceMonitorPlan, error) {
	r.accountID = id
	return nil, nil
}

func TestIntelligenceAccountPlansHandlerValidatesExclusivePositiveFilter(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &intelligenceAccountHandlerRepo{}
	h := NewIntelligenceMonitorHandler(service.NewIntelligenceMonitorService(repo, nil, nil, nil, nil, nil, nil))
	r := gin.New()
	r.GET("/plans", h.ListPlans)
	for _, query := range []string{"", "0", "-1", "1.2", "bad", "9223372036854775808", "1&account_id=2", "1&upstream_target_id=27", "1&upstream_target_id=", "&upstream_target_id=27"} {
		out := httptest.NewRecorder()
		r.ServeHTTP(out, httptest.NewRequest(http.MethodGet, "/plans?account_id="+query, nil))
		require.Equal(t, http.StatusBadRequest, out.Code, query)
		require.Equal(t, "no-store", out.Header().Get("Cache-Control"))
	}
	require.Zero(t, repo.accountID)
	require.Zero(t, repo.filteredID)
	require.Zero(t, repo.allCalls)
	out := httptest.NewRecorder()
	r.ServeHTTP(out, httptest.NewRequest(http.MethodGet, "/plans?account_id=27", nil))
	require.Equal(t, http.StatusOK, out.Code)
	require.Contains(t, out.Body.String(), `"items":[]`)
	require.Equal(t, "no-store", out.Header().Get("Cache-Control"))
	require.Equal(t, int64(27), repo.accountID)
	require.Zero(t, repo.filteredID)
	require.Zero(t, repo.allCalls)
	for _, path := range []string{"/plans?upstream_target_id=37", "/plans"} {
		out = httptest.NewRecorder()
		r.ServeHTTP(out, httptest.NewRequest(http.MethodGet, path, nil))
		require.Equal(t, http.StatusOK, out.Code)
	}
	require.Equal(t, int64(37), repo.filteredID)
	require.Equal(t, 1, repo.allCalls)
}
