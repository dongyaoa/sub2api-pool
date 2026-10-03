package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type accountMonitorHandlerAccounts struct{ service.AccountRepository }

func (accountMonitorHandlerAccounts) GetByID(context.Context, int64) (*service.Account, error) {
	return &service.Account{ID: 9, Name: "Linked account", Platform: "openai", Type: "apikey", Credentials: map[string]any{"api_key": "secret-monitor-key", "base_url": "https://8.8.8.8"}}, nil
}

type accountMonitorHandlerRepo struct {
	service.UpstreamCenterRepository
	target      *service.UpstreamTarget
	findErr     error
	ensureCount int
	readCount   int
}

func (r *accountMonitorHandlerRepo) FindAccountMonitor(ctx context.Context, identity service.UpstreamAccountMonitorIdentity) (*service.UpstreamTarget, error) {
	r.readCount++
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return r.target, r.findErr
}
func (r *accountMonitorHandlerRepo) EnsureAccountMonitor(_ context.Context, _ service.UpstreamAccountMonitorIdentity, candidate *service.UpstreamTarget) (*service.UpstreamTarget, error) {
	r.ensureCount++
	if r.target == nil {
		candidate.ID = 17
		r.target = candidate
	}
	return r.target, nil
}
func (r *accountMonitorHandlerRepo) PopulateStatistics(context.Context, []*service.UpstreamTarget, time.Time) error {
	return nil
}

type accountMonitorHandlerEncryptor struct{}

func (accountMonitorHandlerEncryptor) Encrypt(string) (string, error) { return "hidden-cipher", nil }
func (accountMonitorHandlerEncryptor) Decrypt(string) (string, error) {
	return "secret-monitor-key", nil
}

func TestUpstreamAccountMonitorHandlerReadAndEnsureContracts(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &accountMonitorHandlerRepo{}
	svc := service.NewUpstreamCenterService(repo, accountMonitorHandlerEncryptor{}, accountMonitorHandlerAccounts{}, nil)
	h := NewUpstreamCenterHandler(svc, nil)
	router := gin.New()
	router.GET("/accounts/:id/monitor", h.AccountMonitor)
	router.POST("/accounts/:id/monitor", h.EnsureAccountMonitor)
	for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodGet} {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, httptest.NewRequest(method, "/accounts/9/monitor", nil))
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		require.Equal(t, "no-store", w.Header().Get("Cache-Control"))
		require.NotContains(t, w.Body.String(), "secret-monitor-key")
		require.NotContains(t, w.Body.String(), "hidden-cipher")
		var envelope struct {
			Data service.UpstreamAccountMonitor `json:"data"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &envelope))
		require.Equal(t, int64(9), envelope.Data.AccountID)
		require.True(t, envelope.Data.PelicanSupported)
		if method == http.MethodPost || repo.ensureCount > 0 {
			require.Equal(t, int64(17), envelope.Data.Target.ID)
			require.False(t, envelope.Data.Target.Enabled)
		} else {
			require.Nil(t, envelope.Data.Target)
		}
	}
	require.Equal(t, 1, repo.ensureCount)
	for _, id := range []string{"0", "-1", "not-an-id"} {
		for _, method := range []string{http.MethodGet, http.MethodPost} {
			w := httptest.NewRecorder()
			router.ServeHTTP(w, httptest.NewRequest(method, "/accounts/"+id+"/monitor", nil))
			require.Equal(t, http.StatusBadRequest, w.Code)
		}
	}
	require.Equal(t, 3, repo.readCount, "invalid IDs never reach the repository")
	repo.findErr = service.ErrUpstreamAccountMonitorAmbiguous
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/accounts/9/monitor", nil))
	require.Equal(t, http.StatusConflict, w.Code)
	require.Contains(t, w.Body.String(), "UPSTREAM_ACCOUNT_MONITOR_AMBIGUOUS")
	require.Equal(t, 1, repo.ensureCount)
}
