//go:build unit

package middleware

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestAPIKeyAuthIntelligencePermitRequiresNormalAuthorization(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name, mode string
		permit     bool
		mutate     func(*service.APIKey, *http.Request)
		status     int
		trusted    bool
	}{
		{name: "standard_valid", mode: config.RunModeStandard, permit: true, status: http.StatusOK, trusted: true},
		{name: "simple_valid", mode: config.RunModeSimple, permit: true, status: http.StatusOK, trusted: true},
		{name: "forged_header", mode: config.RunModeStandard, status: http.StatusOK},
		{name: "forged_header_simple", mode: config.RunModeSimple, status: http.StatusOK},
		{name: "wrong_bearer", mode: config.RunModeStandard, permit: true, status: http.StatusUnauthorized, mutate: func(_ *service.APIKey, r *http.Request) { r.Header.Set("Authorization", "Bearer invalid-key") }},
		{name: "disabled_key", mode: config.RunModeStandard, permit: true, status: http.StatusUnauthorized, mutate: func(k *service.APIKey, _ *http.Request) { k.Status = service.StatusDisabled }},
		{name: "exhausted_balance", mode: config.RunModeStandard, permit: true, status: http.StatusForbidden, mutate: func(k *service.APIKey, _ *http.Request) { k.User.Balance = 0 }},
		{name: "exhausted_quota", mode: config.RunModeStandard, permit: true, status: http.StatusTooManyRequests, mutate: func(k *service.APIKey, _ *http.Request) { k.Status = service.StatusAPIKeyQuotaExhausted }},
		{name: "wrong_real_key", mode: config.RunModeStandard, permit: true, status: http.StatusOK, mutate: func(k *service.APIKey, r *http.Request) {
			k.Key = "changed-key"
			r.Header.Set("Authorization", "Bearer changed-key")
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			user := &service.User{ID: 10, Role: service.RoleUser, Status: service.StatusActive, Balance: 10, Concurrency: 3}
			group := &service.Group{ID: 8, Platform: service.PlatformOpenAI, Status: service.StatusActive, Hydrated: true}
			key := &service.APIKey{ID: 72, UserID: user.ID, Key: "monitor-key", Status: service.StatusActive, User: user, Group: group, GroupID: &group.ID}
			workerCtx, cancelWorker := context.WithTimeout(context.Background(), time.Minute)
			defer cancelWorker()
			outbound, err := http.NewRequestWithContext(workerCtx, http.MethodPost, "http://127.0.0.1:8081/v1/responses", nil)
			require.NoError(t, err)
			if tc.permit {
				cleanup, err := service.AuthorizeIntelligenceLocalRequest(outbound, key.ID, key.Key)
				require.NoError(t, err)
				defer cleanup()
			} else {
				outbound.Header.Set("X-Sub2api-Intelligence-Request", strings.Repeat("a", 64))
			}
			request := outbound.Clone(context.Background())
			request.RemoteAddr = "127.0.0.1:54321"
			request.Header.Set("Authorization", "Bearer "+key.Key)
			request.Header.Set("User-Agent", "Sub2API-IntelligenceMonitor/1")
			if tc.mutate != nil {
				tc.mutate(key, request)
			}
			repo := &stubApiKeyRepo{getByKey: func(_ context.Context, credential string) (*service.APIKey, error) {
				if credential != key.Key {
					return nil, service.ErrAPIKeyNotFound
				}
				return key, nil
			}}
			cfg := &config.Config{RunMode: tc.mode}
			router := gin.New()
			router.Use(gin.HandlerFunc(NewAPIKeyAuthMiddleware(service.NewAPIKeyService(repo, nil, nil, nil, nil, nil, cfg), nil, cfg)))
			var handlerCalled bool
			router.POST("/v1/responses", func(c *gin.Context) {
				handlerCalled = true
				require.Empty(t, c.GetHeader("X-Sub2api-Intelligence-Request"))
				got := service.HTTPUpstreamResponseHeaderTimeoutFromContext(c.Request.Context())
				_, bounded := c.Request.Context().Deadline()
				if tc.trusted {
					require.Equal(t, 15*time.Minute, got)
					require.True(t, bounded)
				} else {
					require.Zero(t, got)
					require.False(t, bounded)
				}
				c.Status(http.StatusOK)
			})
			w := httptest.NewRecorder()
			router.ServeHTTP(w, request)
			require.Equal(t, tc.status, w.Code, w.Body.String())
			require.Equal(t, tc.status == http.StatusOK, handlerCalled)
		})
	}
}
