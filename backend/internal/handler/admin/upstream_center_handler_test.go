package admin

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestUpstreamHandlerRejectsInvalidRangesBeforeQuery(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := NewUpstreamCenterHandler(nil, nil)
	router := gin.New()
	router.GET("/targets/:id/history", handler.History)
	router.GET("/finance", handler.Finance)
	for _, path := range []string{"/targets/no/history", "/targets/1/history?from=not-a-date", "/targets/1/history?from=2026-09-23T00:00:00Z&to=2026-09-22T00:00:00Z", "/finance?supplier_id=-1", "/finance?from=2026-09-23T00:00:00Z&to=2026-09-23T00:00:00Z"} {
		t.Run(path, func(t *testing.T) {
			response := httptest.NewRecorder()
			router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
			require.Equal(t, http.StatusBadRequest, response.Code)
		})
	}
}

func TestUpstreamHandlerMalformedJSONDoesNotEchoAPIKey(t *testing.T) {
	gin.SetMode(gin.TestMode)
	handler := NewUpstreamCenterHandler(nil, nil)
	router := gin.New()
	router.POST("/targets", handler.CreateTarget)
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/targets", strings.NewReader(`{"api_key":"sensitive-test-value","enabled":"invalid"}`))
	request.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(response, request)
	require.Equal(t, http.StatusBadRequest, response.Code)
	require.NotContains(t, response.Body.String(), "sensitive-test-value")
}
