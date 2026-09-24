//go:build unit && embed

package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/Wei-Shaw/sub2api/internal/web"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestEmbeddedFrontend_InjectsBuildVersionFromSettingProvider(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const version = "9.8.7-custom.42"
	svc := newSettingServiceForVersionTest(t, service.BuildInfo{Version: version, BuildType: "release"})
	frontend, err := web.NewFrontendServer(svc)
	require.NoError(t, err)

	router := gin.New()
	router.Use(frontend.Middleware())

	// Verify the initial render and the cached HTML response both retain the
	// version used by the running binary, including direct SPA navigation.
	for _, path := range []string{"/", "/admin/dashboard"} {
		t.Run(path, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			router.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
			require.Equal(t, http.StatusOK, recorder.Code)

			_, script, found := strings.Cut(recorder.Body.String(), "window.__APP_CONFIG__=")
			require.True(t, found, "embedded HTML must include public settings")
			settingsJSON, _, found := strings.Cut(script, ";</script>")
			require.True(t, found, "injected settings script must be complete")
			var settings struct {
				Version string `json:"version"`
			}
			require.NoError(t, json.Unmarshal([]byte(settingsJSON), &settings))
			require.Equal(t, version, settings.Version)
		})
	}
}
