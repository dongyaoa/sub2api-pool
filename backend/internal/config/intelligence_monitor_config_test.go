package config

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestLoadIntelligenceMonitorConcurrency(t *testing.T) {
	tests := []struct {
		name        string
		yaml        string
		pelicanEnv  string
		candyEnv    string
		wantPelican int
		wantCandy   int
	}{
		{name: "defaults", wantPelican: 8, wantCandy: 4},
		{
			name:        "config file",
			yaml:        "intelligence_monitor:\n  max_concurrency: 12\n  candy_max_concurrency: 6\n",
			wantPelican: 12, wantCandy: 6,
		},
		{
			name:       "environment overrides config file",
			yaml:       "intelligence_monitor:\n  max_concurrency: 12\n  candy_max_concurrency: 6\n",
			pelicanEnv: "16", candyEnv: "8", wantPelican: 16, wantCandy: 8,
		},
		{name: "environment without config file", pelicanEnv: "256", candyEnv: "128", wantPelican: 256, wantCandy: 128},
		{name: "minimum concurrency", pelicanEnv: "1", candyEnv: "1", wantPelican: 1, wantCandy: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resetViperWithJWTSecret(t)
			t.Setenv("INTELLIGENCE_MONITOR_MAX_CONCURRENCY", tt.pelicanEnv)
			t.Setenv("INTELLIGENCE_MONITOR_CANDY_MAX_CONCURRENCY", tt.candyEnv)
			if tt.yaml != "" {
				path := filepath.Join(t.TempDir(), "config.yaml")
				require.NoError(t, os.WriteFile(path, []byte(tt.yaml), 0o600))
				t.Setenv("CONFIG_FILE", path)
			}
			cfg, err := Load()
			require.NoError(t, err)
			require.Equal(t, tt.wantPelican, cfg.IntelligenceMonitor.MaxConcurrency)
			require.Equal(t, tt.wantCandy, cfg.IntelligenceMonitor.CandyMaxConcurrency)
		})
	}
}

func TestLoadRejectsInvalidIntelligenceMonitorConcurrency(t *testing.T) {
	for _, setting := range []struct {
		env string
		key string
		max int
	}{
		{env: "INTELLIGENCE_MONITOR_MAX_CONCURRENCY", key: "max_concurrency", max: 256},
		{env: "INTELLIGENCE_MONITOR_CANDY_MAX_CONCURRENCY", key: "candy_max_concurrency", max: 128},
	} {
		for _, value := range []int{-1, 0, setting.max + 1} {
			t.Run(fmt.Sprintf("%s/%d", setting.key, value), func(t *testing.T) {
				resetViperWithJWTSecret(t)
				t.Setenv("INTELLIGENCE_MONITOR_MAX_CONCURRENCY", "8")
				t.Setenv("INTELLIGENCE_MONITOR_CANDY_MAX_CONCURRENCY", "4")
				t.Setenv(setting.env, fmt.Sprint(value))
				_, err := Load()
				require.ErrorContains(t, err, "intelligence_monitor."+setting.key)
			})
		}
	}
}
