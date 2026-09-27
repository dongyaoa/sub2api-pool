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

type intelligenceConcurrencyHandlerRepo struct {
	service.IntelligenceMonitorRepository
	settings service.IntelligenceMonitorConcurrencySettings
	saves    int
}

func (r *intelligenceConcurrencyHandlerRepo) GetConcurrency(context.Context) (*service.IntelligenceMonitorConcurrencySettings, error) {
	settings := r.settings
	return &settings, nil
}

func (r *intelligenceConcurrencyHandlerRepo) SaveConcurrency(_ context.Context, limits service.IntelligenceMonitorConcurrency) error {
	r.settings.IntelligenceMonitorConcurrency, r.settings.Source = limits, "database"
	r.saves++
	return nil
}

func TestIntelligenceConcurrencyHandlerValidatesCompleteIntegerLimits(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &intelligenceConcurrencyHandlerRepo{settings: service.IntelligenceMonitorConcurrencySettings{Source: "deployment"}}
	h := NewIntelligenceMonitorHandler(service.NewIntelligenceMonitorService(repo, nil, nil, nil, nil, nil, nil))
	router := gin.New()
	router.GET("/concurrency", h.GetConcurrency)
	router.PUT("/concurrency", h.UpdateConcurrency)
	for _, body := range []string{
		`{}`, `null`, `{"max_concurrency":8}`, `{"candy_max_concurrency":4}`,
		`{"max_concurrency":null,"candy_max_concurrency":4}`, `{"max_concurrency":8,"candy_max_concurrency":null}`,
		`{"max_concurrency":0,"candy_max_concurrency":4}`, `{"max_concurrency":-1,"candy_max_concurrency":4}`,
		`{"max_concurrency":257,"candy_max_concurrency":4}`, `{"max_concurrency":8,"candy_max_concurrency":129}`,
		`{"max_concurrency":8.5,"candy_max_concurrency":4}`, `{"max_concurrency":"8","candy_max_concurrency":4}`,
	} {
		out := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPut, "/concurrency", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(out, req)
		require.Equal(t, http.StatusBadRequest, out.Code, body)
	}
	require.Zero(t, repo.saves, "invalid submissions must never be persisted")
	get := httptest.NewRecorder()
	router.ServeHTTP(get, httptest.NewRequest(http.MethodGet, "/concurrency", nil))
	require.Equal(t, http.StatusOK, get.Code)
	require.Equal(t, "no-store", get.Header().Get("Cache-Control"))
	require.Contains(t, get.Body.String(), `"max_concurrency":8`)
	require.Contains(t, get.Body.String(), `"candy_max_concurrency":4`)
	for _, body := range []string{`{"max_concurrency":256,"candy_max_concurrency":128}`, `{"max_concurrency":1,"candy_max_concurrency":1}`} {
		out := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPut, "/concurrency", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(out, req)
		require.Equal(t, http.StatusOK, out.Code, out.Body.String())
		require.Equal(t, "no-store", out.Header().Get("Cache-Control"))
		require.Contains(t, out.Body.String(), `"source":"database"`)
	}
	require.Equal(t, 2, repo.saves)
}
