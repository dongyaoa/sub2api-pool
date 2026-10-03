//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type intelligenceBulkOAuthTestRepo struct {
	*intelligenceOAuthStatusRepository
	IntelligenceMonitorLifecycleRepository
}

func (r *intelligenceBulkOAuthTestRepo) SetPlansEnabled(_ context.Context, enabled bool) (*IntelligenceScheduleUpdate, error) {
	r.plan.Enabled = enabled
	return &IntelligenceScheduleUpdate{Updated: 1}, nil
}

func TestIntelligenceLifecycleBulkEnablePreservesWeeklyCooldown(t *testing.T) {
	for _, model := range []string{IntelligenceMonitorModel, IntelligenceMonitorSolModel} {
		t.Run(model, func(t *testing.T) {
			svc, accounts, _ := intelligenceOAuthFixture()
			reset := time.Now().Add(time.Hour)
			accounts.account.Extra = map[string]any{"codex_7d_used_percent": 100, "codex_7d_reset_at": reset.Format(time.RFC3339Nano)}
			accountID := accounts.account.ID
			repo := &intelligenceBulkOAuthTestRepo{intelligenceOAuthStatusRepository: &intelligenceOAuthStatusRepository{
				intelligenceIndependentScheduleRepository: intelligenceIndependentScheduleRepository{
					intelligenceTestRepository: intelligenceTestRepository{plan: &IntelligenceMonitorPlan{ID: 3, SourceType: "openai_oauth", Model: model, AccountID: &accountID, Enabled: false, CandyEnabled: true, IntervalSeconds: 300, CandyIntervalSeconds: 180}},
					artworkDue:                 []int64{3}, candyDue: []int64{3},
				},
			}}
			svc.repo = repo
			_, err := svc.SetPlansEnabled(context.Background(), true)
			require.NoError(t, err)
			svc.schedule()
			require.Empty(t, repo.queuedRuns, "bulk resume never bypasses weekly cooldown for pelican or candy")
			require.Equal(t, 2, repo.deferred)
			require.True(t, repo.plan.Enabled, "desired schedule resumes only after account recovery")
			accounts.account.Extra["codex_7d_used_percent"] = 10
			svc.schedule()
			require.Len(t, repo.queuedRuns, 2)
			require.Equal(t, 300, repo.plan.IntervalSeconds)
			require.Equal(t, 180, repo.plan.CandyIntervalSeconds)
		})
	}
}
