package admin

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestPendingOpenAIReauthRejectsArbitraryTokenWithoutClearingState(t *testing.T) {
	for _, body := range []string{
		`{"type":"oauth","credentials":{"access_token":"arbitrary-token"}}`,
		`{"type":"oauth","credentials":{},"openai_oauth_session":{"session_id":"fixture","code":"fixture","state":"fixture"}}`,
	} {
		t.Run("verified_service_required", func(t *testing.T) {
			gin.SetMode(gin.TestMode)
			stub := newStubAdminService()
			stub.getAccountResult = &service.Account{ID: 1, Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth,
				Extra: map[string]any{service.OpenAIReauthPendingKey: true}}
			handler := NewAccountHandler(stub, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil)
			router := gin.New()
			router.POST("/accounts/:id/apply-oauth-credentials", handler.ApplyOAuthCredentials)
			recorder := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodPost, "/accounts/1/apply-oauth-credentials", bytes.NewBufferString(body))
			request.Header.Set("Content-Type", "application/json")
			router.ServeHTTP(recorder, request)
			require.Equal(t, http.StatusBadRequest, recorder.Code)
			require.Contains(t, recorder.Body.String(), "manual_reauth_session_required")
			require.Zero(t, stub.updateAccountCalls)
			require.Zero(t, stub.updateAccountExtraCalls)
		})
	}
}
