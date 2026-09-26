package admin

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestUpstreamStorageHandlerRejectsInvalidMutationBeforeRepository(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h := NewUpstreamCenterHandler(service.NewUpstreamCenterService(nil, nil, nil, nil), nil)
	r := gin.New()
	r.PUT("/storage", h.SaveStoragePolicy)
	r.POST("/purge", h.PurgeStorage)
	for _, tc := range []struct{ method, path, body string }{
		{http.MethodPut, "/storage", `{}`},
		{http.MethodPut, "/storage", `{"enabled":true,"history_retention_days":7,"snapshot_retention_days":7}`},
		{http.MethodPut, "/storage", `{"enabled":false,"history_retention_days":30,"snapshot_retention_days":0}`},
		{http.MethodPut, "/storage", `{"enabled":"secret-invalid"}`},
		{http.MethodPost, "/purge", `{"kind":"supplier","id":1}`},
		{http.MethodPost, "/purge", `{"kind":"all","id":1,"confirm_name":"secret-invalid"}`},
		{http.MethodPost, "/purge", `{"kind":"target","id":0,"confirm_name":"secret-invalid"}`},
	} {
		req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
		req.Header.Set("Content-Type", "application/json")
		out := httptest.NewRecorder()
		r.ServeHTTP(out, req)
		require.Equal(t, http.StatusBadRequest, out.Code, tc.body)
		require.NotContains(t, out.Body.String(), "secret-invalid")
	}
}
