package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type publicPelicanHandlerRepo struct {
	service.IntelligenceMonitorRepository
	cfg   service.IntelligencePublicDisplay
	saves int
}

func (r *publicPelicanHandlerRepo) GetPublicDisplay(context.Context) (*service.IntelligencePublicDisplay, error) {
	cfg := r.cfg
	return &cfg, nil
}
func (r *publicPelicanHandlerRepo) SavePublicDisplay(_ context.Context, cfg service.IntelligencePublicDisplay) error {
	r.cfg = cfg
	r.saves++
	return nil
}
func (r *publicPelicanHandlerRepo) ListPublicPelican(context.Context, []int64) (*service.PublicPelicanPage, error) {
	return nil, service.ErrPublicPelicanUnavailable
}
func (r *publicPelicanHandlerRepo) GetPublicPelicanRun(context.Context, int64, []int64) (*service.PublicPelicanRunDetail, error) {
	return nil, service.ErrIntelligenceNotFound
}

func TestIntelligencePublicDisplayHandlerValidatesBoundedSettings(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &publicPelicanHandlerRepo{cfg: service.DefaultIntelligencePublicDisplay()}
	h := NewIntelligenceMonitorHandler(service.NewIntelligenceMonitorService(repo, nil, nil, nil, nil, nil, nil))
	router := gin.New()
	router.GET("/settings", h.GetPublicDisplay)
	router.PUT("/settings", h.UpdatePublicDisplay)
	for _, body := range []string{`{}`, `null`, `{"enabled":false}`, `{"plan_ids":[]}`, `{"enabled":true,"plan_ids":null}`, `{"enabled":true,"plan_ids":[1.5]}`, `{"enabled":true,"plan_ids":[-1]}`, `{"enabled":"yes","plan_ids":[]}`, `{"enabled":true,"plan_ids":[],"title":"` + strings.Repeat("字", 61) + `"}`, `{"enabled":true,"plan_ids":[],"notice":"` + strings.Repeat("x", 33000) + `"}`} {
		out := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPut, "/settings", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		router.ServeHTTP(out, req)
		require.Equal(t, http.StatusBadRequest, out.Code, body)
	}
	require.Zero(t, repo.saves)
	out := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/settings", strings.NewReader(`{"enabled":false,"title":"新标题","description":"介绍","notice":"公告","plan_ids":[1,1,2]}`))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(out, req)
	require.Equal(t, http.StatusOK, out.Code, out.Body.String())
	require.Equal(t, "no-store", out.Header().Get("Cache-Control"))
	require.Equal(t, []int64{1, 2}, repo.cfg.PlanIDs)
	require.Equal(t, 1, repo.saves)
}

func TestIntelligencePublicPelicanHandlersRequireLoginAndHideSelection(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &publicPelicanHandlerRepo{cfg: service.IntelligencePublicDisplay{PublicPelicanConfig: service.PublicPelicanConfig{Title: "Gallery"}, PlanIDs: []int64{71, 72}}}
	h := NewIntelligenceMonitorHandler(service.NewIntelligenceMonitorService(repo, nil, nil, nil, nil, nil, nil))
	router := gin.New()
	router.Use(func(c *gin.Context) {
		if c.GetHeader("X-Test-Login") == "yes" {
			c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 5})
		}
	})
	router.GET("/config", h.PublicPelicanConfig)
	router.GET("/list", h.ListPublicPelican)
	router.GET("/runs/:id", h.GetPublicPelicanRun)
	for _, path := range []string{"/config", "/list", "/runs/4"} {
		out := httptest.NewRecorder()
		router.ServeHTTP(out, httptest.NewRequest(http.MethodGet, path, nil))
		require.Equal(t, http.StatusUnauthorized, out.Code, path)
	}
	for _, tc := range []struct {
		path   string
		status int
	}{{"/config", 200}, {"/list", 200}, {"/runs/4", 404}, {"/runs/not-an-id", 404}, {"/runs/0", 404}} {
		out := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, tc.path, nil)
		req.Header.Set("X-Test-Login", "yes")
		router.ServeHTTP(out, req)
		require.Equal(t, tc.status, out.Code, out.Body.String())
		require.Equal(t, "no-store", out.Header().Get("Cache-Control"))
		require.NotContains(t, out.Body.String(), "plan_ids")
		if tc.path == "/config" {
			var response struct {
				Data map[string]any `json:"data"`
			}
			require.NoError(t, json.Unmarshal(out.Body.Bytes(), &response))
			require.Len(t, response.Data, 4)
			for _, key := range []string{"enabled", "title", "description", "notice"} {
				require.Contains(t, response.Data, key)
			}
		}
		if tc.path == "/list" {
			require.Contains(t, out.Body.String(), `"items":[]`)
		}
	}
}
