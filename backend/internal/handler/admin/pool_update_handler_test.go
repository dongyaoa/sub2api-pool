//go:build unit

package admin

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/poolupdate"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type poolHandlerStub struct {
	systemHandlerUpdateServiceStub
	startCalls        int
	request           poolupdate.UpdateRequest
	startContextError error
	hasDeadline       bool
	statusError       error
}

func (*poolHandlerStub) PoolUpdatesEnabled() bool { return true }
func (s *poolHandlerStub) PoolUpdateStatus(context.Context) (*poolupdate.Status, error) {
	return &poolupdate.Status{Available: true, Job: &poolupdate.Job{ID: "job-1", State: "pulling"}}, s.statusError
}
func (s *poolHandlerStub) StartPoolUpdate(ctx context.Context, request poolupdate.UpdateRequest) (*poolupdate.Job, error) {
	s.startCalls++
	s.request, s.startContextError = request, ctx.Err()
	_, s.hasDeadline = ctx.Deadline()
	return &poolupdate.Job{ID: "job-1", State: "queued", Version: request.Version, Digest: request.Digest}, nil
}
func (*poolHandlerStub) GetCurrentBuild() (string, string) {
	return "0.2.7-pool.5", strings.Repeat("a", 40)
}

func poolHandlerRouter(t *testing.T, svc *poolHandlerStub) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	service.SetDefaultIdempotencyCoordinator(nil)
	t.Cleanup(func() { service.SetDefaultIdempotencyCoordinator(nil) })
	lock := service.NewSystemOperationLockService(newMemoryIdempotencyRepoStub(), service.IdempotencyConfig{ProcessingTimeout: time.Second, SystemOperationTTL: time.Minute})
	h := NewSystemHandler(svc, lock)
	router := gin.New()
	router.POST("/update", h.PerformUpdate)
	router.GET("/status", h.UpdateStatus)
	router.GET("/version", h.GetVersion)
	return router
}

func TestPoolImageUpdateHandlerRejectsUnconfirmedOrInvalidPayload(t *testing.T) {
	valid := `{"version":"0.2.7-pool.5","digest":"sha256:` + strings.Repeat("c", 64) + `"}`
	for _, payload := range []string{"", "null", "{}", `{"version":"0.2.7-pool.5"}`, valid + "{}", strings.TrimSuffix(valid, "}") + `,"command":"docker rm"}`, `{"version":"latest","digest":"anything"}`, strings.Repeat("a", 1025)} {
		t.Run(payload[:min(len(payload), 40)], func(t *testing.T) {
			svc := &poolHandlerStub{}
			router := poolHandlerRouter(t, svc)
			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPost, "/update", strings.NewReader(payload))
			if payload == "" {
				req.Body = nil
			}
			router.ServeHTTP(rec, req)
			require.Equal(t, http.StatusBadRequest, rec.Code)
			require.Zero(t, svc.startCalls)
			require.Zero(t, svc.performCall)
		})
	}
}

func TestPoolImageUpdateSubmissionSurvivesDisconnect(t *testing.T) {
	svc := &poolHandlerStub{}
	router := poolHandlerRouter(t, svc)
	digest := "sha256:" + strings.Repeat("c", 64)
	req := httptest.NewRequest(http.MethodPost, "/update", strings.NewReader(`{"version":"0.2.7-pool.5","digest":"`+digest+`"}`))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	req = req.WithContext(ctx)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, 1, svc.startCalls)
	require.NoError(t, svc.startContextError)
	require.True(t, svc.hasDeadline)
	require.Zero(t, svc.performCall)
	require.Equal(t, digest, svc.request.Digest)
	var body struct {
		Data struct {
			NeedRestart bool           `json:"need_restart"`
			Job         poolupdate.Job `json:"job"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.False(t, body.Data.NeedRestart)
	require.Equal(t, "queued", body.Data.Job.State)
}

func TestPoolImageUpdateStatusAndVersion(t *testing.T) {
	svc := &poolHandlerStub{}
	router := poolHandlerRouter(t, svc)
	for _, path := range []string{"/status", "/version"} {
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		require.Equal(t, http.StatusOK, rec.Code)
		if path == "/status" {
			require.Contains(t, rec.Body.String(), `"state":"pulling"`)
		} else {
			require.Contains(t, rec.Body.String(), `"revision":"`+strings.Repeat("a", 40)+`"`)
		}
	}
	require.Empty(t, svc.checkForces, "getting the running build must not contact the registry")
	svc.statusError = errors.New("helper socket not found")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/status", nil))
	require.Equal(t, http.StatusServiceUnavailable, rec.Code)
	require.NotContains(t, rec.Body.String(), "socket")
}
