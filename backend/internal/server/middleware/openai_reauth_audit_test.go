package middleware

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestOpenAIReauthImportAuditOmitsSecretsEvenMalformedJSON(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, route := range []string{"/api/v1/admin/openai/auto-reauth/import", "/api/v1/admin/openai/accounts/:id/auto-reauth/credentials"} {
		for _, body := range []string{`{"content":"fixture@example.test----password-canary----JBSWY3DPEHPK3PXP"}`, `{"password":"password-canary","totp_secret":"JBSWY3DPEHPK3PXP"}`, `{"content":"password-canary`} {
			repository := &auditCaptureRepository{}
			audit := service.NewAuditLogService(repository, nil)
			audit.Start()
			router := gin.New()
			router.Use(func(c *gin.Context) {
				c.Set(string(ContextKeyUser), AuthSubject{UserID: 77})
				c.Set(string(ContextKeyUserRole), "admin")
				c.Next()
			})
			router.Use(gin.HandlerFunc(NewAuditLogMiddleware(audit)))
			router.POST(route, func(c *gin.Context) { c.Status(http.StatusBadRequest) })
			req := httptest.NewRequest(http.MethodPost, strings.ReplaceAll(route, ":id", "7"), bytes.NewBufferString(body))
			req.Header.Set("Content-Type", "application/json")
			router.ServeHTTP(httptest.NewRecorder(), req)
			audit.Stop()
			require.Len(t, repository.logs, 1)
			require.Equal(t, "<credential-bearing body omitted>", repository.logs[0].RequestBody)
		}
	}
}
