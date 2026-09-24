//go:build unit

package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/antigravity"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// The actual provider runs setting migrations before returning the service.
// Keep those reads and writes in memory so this regression exercises production
// wiring rather than manually supplying a version to SetVersion.
type settingVersionRepoStub struct {
	settingHandlerPublicRepoStub
}

func (s *settingVersionRepoStub) GetValue(_ context.Context, key string) (string, error) {
	value, ok := s.values[key]
	if !ok {
		return "", service.ErrSettingNotFound
	}
	return value, nil
}

func (s *settingVersionRepoStub) Set(_ context.Context, key, value string) error {
	s.values[key] = value
	return nil
}

func (s *settingVersionRepoStub) SetMultiple(_ context.Context, values map[string]string) error {
	for key, value := range values {
		s.values[key] = value
	}
	return nil
}

func newSettingServiceForVersionTest(t *testing.T, buildInfo service.BuildInfo) *service.SettingService {
	t.Helper()
	repo := &settingVersionRepoStub{
		settingHandlerPublicRepoStub: settingHandlerPublicRepoStub{values: map[string]string{}},
	}
	svc := service.ProvideSettingService(repo, nil, nil, &config.Config{}, buildInfo)
	t.Cleanup(func() {
		antigravity.SetUserAgentVersionResolver(nil)
		service.SetCodexCanonicalUserAgentResolver(nil)
	})
	return svc
}

func TestProvideSettingService_PublicSettingsVersionMatchesBuildInfo(t *testing.T) {
	gin.SetMode(gin.TestMode)

	for _, tc := range []struct {
		name      string
		buildInfo service.BuildInfo
	}{
		{name: "source", buildInfo: service.BuildInfo{Version: "0.2.7-pool.2", BuildType: "source"}},
		{name: "release", buildInfo: service.BuildInfo{Version: "0.2.7-pool.3", BuildType: "release"}},
		// Build arguments may override cmd/server/VERSION. Both public paths
		// must use the supplied build information, not a hardcoded file value.
		{name: "custom_build_version", buildInfo: service.BuildInfo{Version: "9.8.7-custom.42", BuildType: "release"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc := newSettingServiceForVersionTest(t, tc.buildInfo)

			raw, err := svc.GetPublicSettingsForInjection(context.Background())
			require.NoError(t, err)
			payload, ok := raw.(*service.PublicSettingsInjectionPayload)
			require.True(t, ok)
			require.Equal(t, tc.buildInfo.Version, payload.Version)

			h := ProvideSettingHandler(svc, BuildInfo{
				Version: tc.buildInfo.Version, BuildType: tc.buildInfo.BuildType,
			}, nil)
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodGet, "/api/v1/settings/public", nil)
			h.GetPublicSettings(c)

			require.Equal(t, http.StatusOK, recorder.Code)
			var response struct {
				Code int `json:"code"`
				Data struct {
					Version string `json:"version"`
				} `json:"data"`
			}
			require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &response))
			require.Zero(t, response.Code)
			require.Equal(t, tc.buildInfo.Version, response.Data.Version)
			require.Equal(t, response.Data.Version, payload.Version)
		})
	}
}
