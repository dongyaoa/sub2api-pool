package admin

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type reauthStatusRepo struct {
	service.OpenAIReauthRepository
	err error
}

func (r *reauthStatusRepo) ListStatuses(context.Context, int) ([]service.OpenAIReauthStatus, error) {
	return nil, r.err
}

type reauthSchemaError struct{}

func (reauthSchemaError) Error() string    { return "synthetic private database diagnostic" }
func (reauthSchemaError) SQLState() string { return "42703" }

func TestOpenAIReauthStatusEmptyAndDiagnosticErrors(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name   string
		err    error
		status int
		body   string
	}{
		{"empty", nil, http.StatusOK, `"accounts":[]`},
		{"schema", reauthSchemaError{}, http.StatusServiceUnavailable, "auto_reauth_schema_unavailable"},
		{"database", errors.New("synthetic private database diagnostic"), http.StatusServiceUnavailable, "auto_reauth_status_unavailable"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reauth := service.NewOpenAIReauthService(&reauthStatusRepo{err: tc.err}, nil, nil, nil, nil, nil, nil, nil, "", "", false)
			h := &OpenAIOAuthHandler{reauth: reauth}
			router := gin.New()
			router.GET("/status", h.ListAutoReauth)
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/status", nil))
			require.Equal(t, tc.status, recorder.Code)
			require.Contains(t, recorder.Body.String(), tc.body)
			require.NotContains(t, recorder.Body.String(), "private database")
		})
	}
}
