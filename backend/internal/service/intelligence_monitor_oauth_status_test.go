//go:build unit

package service

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestIntelligenceOAuthAccountStatusOnlyPausesWeeklyQuota(t *testing.T) {
	now := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	future, past := now.Add(time.Hour), now.Add(-time.Hour)
	weekly := func(a *Account) {
		a.Extra = map[string]any{"codex_7d_used_percent": 100, "codex_7d_reset_at": future.Format(time.RFC3339)}
	}
	threshold := func(window string, until time.Time) func(*Account) {
		return func(a *Account) {
			a.TempUnschedulableUntil = &until
			a.TempUnschedulableReason = BuildDetailedAccountSchedulingThresholdReason(AccountSchedulingThresholdReasonInput{Platform: PlatformOpenAI, Window: window, UsedPercent: 96, ThresholdPercent: 95, Until: until, Now: now})
		}
	}
	for _, tc := range []struct {
		name   string
		change func(*Account)
		status string
		reset  bool
	}{
		{"normal", func(*Account) {}, "normal", false},
		{"transient 429", func(a *Account) { a.RateLimitResetAt = &future }, "normal", false},
		{"transient rule", func(a *Account) {
			a.TempUnschedulableUntil = &future
			a.TempUnschedulableReason = `{"source":"error_rule","error_message":"429"}`
		}, "normal", false},
		{"overloaded", func(a *Account) { a.OverloadUntil = &future }, "normal", false},
		{"five hour quota", func(a *Account) {
			a.Extra = map[string]any{"codex_5h_used_percent": 100, "codex_5h_reset_at": future.Format(time.RFC3339)}
		}, "normal", false},
		{"five hour threshold", threshold("5h", future), "normal", false},
		{"weekly quota", weekly, "weekly_limited", true},
		{"weekly overrides disabled", func(a *Account) { weekly(a); a.Status = StatusError; a.Schedulable = false }, "weekly_limited", true},
		{"weekly threshold", threshold("7d", future), "weekly_limited", true},
		{"expired weekly threshold", threshold("7d", past), "normal", false},
		{"reset weekly snapshot", func(a *Account) { weekly(a); a.Extra["codex_7d_reset_at"] = past.Format(time.RFC3339) }, "normal", false},
		{"relative weekly reset", func(a *Account) {
			a.Extra = map[string]any{"codex_7d_used_percent": 100, "codex_7d_reset_after_seconds": 3600, "codex_usage_updated_at": now.Format(time.RFC3339)}
		}, "weekly_limited", true},
		{"expired relative reset", func(a *Account) {
			a.Extra = map[string]any{"codex_7d_used_percent": 100, "codex_7d_reset_after_seconds": 3600, "codex_usage_updated_at": now.Add(-2 * time.Hour).Format(time.RFC3339)}
		}, "normal", false},
		{"raw secondary weekly", func(a *Account) {
			a.Extra = map[string]any{"codex_primary_used_percent": 25, "codex_primary_window_minutes": 300, "codex_secondary_used_percent": 100, "codex_secondary_window_minutes": 10080, "codex_secondary_reset_at": future.Format(time.RFC3339)}
		}, "weekly_limited", true},
		{"flipped raw primary weekly", func(a *Account) {
			a.Extra = map[string]any{"codex_primary_used_percent": 100, "codex_primary_window_minutes": 10080, "codex_primary_reset_at": future.Format(time.RFC3339), "codex_secondary_used_percent": 25, "codex_secondary_window_minutes": 300}
		}, "weekly_limited", true},
		{"raw five hour exhaustion", func(a *Account) {
			a.Extra = map[string]any{"codex_primary_used_percent": 100, "codex_primary_window_minutes": 300, "codex_secondary_used_percent": 25, "codex_secondary_window_minutes": 10080}
		}, "normal", false},
		{"canonical overrides old raw", func(a *Account) {
			a.Extra = map[string]any{"codex_7d_used_percent": 25, "codex_secondary_used_percent": 100, "codex_secondary_window_minutes": 10080}
		}, "normal", false},
		{"generic reset with weekly proof", func(a *Account) { a.Extra = map[string]any{"codex_7d_used_percent": 100}; a.RateLimitResetAt = &future }, "weekly_limited", true},
		{"generic deadline recovered", func(a *Account) { a.Extra = map[string]any{"codex_7d_used_percent": 100}; a.RateLimitResetAt = &past }, "normal", false},
		{"stale incomplete weekly snapshot", func(a *Account) {
			a.Extra = map[string]any{"codex_7d_used_percent": 100, "codex_usage_updated_at": now.Add(-8 * 24 * time.Hour).Format(time.RFC3339)}
		}, "normal", false},
		{"replaced identity", func(a *Account) {
			weekly(a)
			a.Credentials["email"] = "current@example.com"
			a.Extra["email"] = "previous@example.com"
		}, "normal", false},
		{"nonfinite usage", func(a *Account) { a.Extra = map[string]any{"codex_7d_used_percent": math.Inf(1)} }, "normal", false},
		{"disabled", func(a *Account) { a.Schedulable = false }, "unavailable", false},
		{"expired account", func(a *Account) { a.AutoPauseOnExpired = true; a.ExpiresAt = &past }, "unavailable", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, accounts, _ := intelligenceOAuthFixture()
			tc.change(accounts.account)
			status := intelligenceOAuthAccountStatus(accounts.account, now)
			require.Equal(t, tc.status, status.Status)
			require.Equal(t, tc.status != "normal", status.MonitoringPaused)
			if tc.reset {
				require.NotNil(t, status.ResetAt)
				require.Equal(t, future, *status.ResetAt)
			} else {
				require.Nil(t, status.ResetAt)
			}
		})
	}
	require.Equal(t, "unavailable", intelligenceOAuthAccountStatus(nil, now).Status)
}

type intelligenceOAuthStatusRepository struct {
	intelligenceIndependentScheduleRepository
	accounts          map[int64]*Account
	loadIDs           []int64
	loads, deferred   int
	deferredUntil     time.Time
	loadErr, deferErr error
}

func (r *intelligenceOAuthStatusRepository) LoadOAuthMonitorAccounts(_ context.Context, ids []int64) (map[int64]*Account, error) {
	r.loads++
	r.loadIDs = append([]int64(nil), ids...)
	return r.accounts, r.loadErr
}
func (r *intelligenceOAuthStatusRepository) DeferOAuthMonitor(_ context.Context, planID, accountID int64, until time.Time) error {
	r.deferred++
	r.deferredUntil = until
	return r.deferErr
}

func TestIntelligenceOAuthCooldownSkipsBothSchedulesAndManualEnqueueThenRecovers(t *testing.T) {
	svc, accounts, _ := intelligenceOAuthFixture()
	reset := time.Now().Add(3 * time.Hour)
	accounts.account.Extra = map[string]any{"codex_7d_used_percent": 100, "codex_7d_reset_at": reset.Format(time.RFC3339Nano)}
	accountID := accounts.account.ID
	repo := &intelligenceOAuthStatusRepository{intelligenceIndependentScheduleRepository: intelligenceIndependentScheduleRepository{
		intelligenceTestRepository: intelligenceTestRepository{plan: &IntelligenceMonitorPlan{ID: 3, SourceType: "openai_oauth", AccountID: &accountID, Enabled: true, CandyEnabled: true, IntervalSeconds: 300, CandyIntervalSeconds: 180}},
		artworkDue:                 []int64{3}, candyDue: []int64{3},
	}}
	svc.repo = repo
	before := time.Now()
	svc.schedule()
	require.Empty(t, repo.queuedRuns, "cooldown must not generate failed records on every schedule tick")
	require.Equal(t, 2, repo.deferred)
	require.WithinDuration(t, before.Add(30*time.Second), repo.deferredUntil, 2*time.Second)
	require.True(t, repo.plan.Enabled, "automatic suspension preserves the schedule switch")
	for _, kind := range []string{IntelligenceMonitorTestPelican, IntelligenceMonitorTestCandy} {
		_, err := svc.enqueueTest(context.Background(), 3, false, kind)
		require.ErrorIs(t, err, ErrIntelligenceOAuthCoolingDown)
	}
	require.Empty(t, repo.queuedRuns)
	// A simultaneous model configuration error must not override quota suspension.
	accounts.account.Credentials["model_mapping"] = map[string]any{IntelligenceMonitorModel: "different-model"}
	for _, kind := range []string{IntelligenceMonitorTestPelican, IntelligenceMonitorTestCandy} {
		_, err := svc.enqueueTest(context.Background(), 3, false, kind)
		require.ErrorIs(t, err, ErrIntelligenceOAuthCoolingDown)
	}
	require.Empty(t, repo.queuedRuns)
	delete(accounts.account.Credentials, "model_mapping")
	// A refreshed snapshot can recover before the original reset date.
	accounts.account.Extra["codex_7d_used_percent"] = 12
	svc.schedule()
	require.Len(t, repo.queuedRuns, 2)
	require.Equal(t, IntelligenceMonitorTestPelican, repo.queuedRuns[0].TestKind)
	require.Equal(t, IntelligenceMonitorTestCandy, repo.queuedRuns[1].TestKind)
	require.Equal(t, 300, repo.plan.IntervalSeconds)
	require.Equal(t, 180, repo.plan.CandyIntervalSeconds)
}

func TestIntelligenceOAuthCooldownRechecksQueuedRunBeforeAccountSlot(t *testing.T) {
	svc, accounts, slots := intelligenceOAuthFixture()
	reset := time.Now().Add(time.Hour)
	accounts.account.Extra = map[string]any{"codex_7d_used_percent": 100, "codex_7d_reset_at": reset.Format(time.RFC3339Nano)}
	called := false
	svc.oauthForward = intelligenceOAuthForwardFunc(func(context.Context, *gin.Context, *Account, []byte) (*OpenAIForwardResult, error) {
		called = true
		return nil, nil
	})
	for _, kind := range []string{IntelligenceMonitorTestPelican, IntelligenceMonitorTestCandy} {
		run := intelligenceOAuthRun()
		run.TestKind = kind
		_, _, message := svc.generateOpenAIOAuth(context.Background(), run)
		require.Contains(t, message, "weekly quota is cooling down")
	}
	require.False(t, called)
	require.Zero(t, slots.accountID, "do not claim slots or forward a previously queued weekly-limited account")
}

func TestIntelligenceOAuthUnavailableAccountPausesWithoutRepeatedFailureRuns(t *testing.T) {
	for _, state := range []string{"disabled", "inactive", "missing", "expired", "apikey", "shadow"} {
		t.Run(state, func(t *testing.T) {
			svc, accounts, _ := intelligenceOAuthFixture()
			id := accounts.account.ID
			switch state {
			case "disabled":
				accounts.account.Schedulable = false
			case "inactive":
				accounts.account.Status = StatusError
			case "missing":
				accounts.account = nil
			case "expired":
				past := time.Now().Add(-time.Hour)
				accounts.account.AutoPauseOnExpired = true
				accounts.account.ExpiresAt = &past
			case "apikey":
				accounts.account.Type = AccountTypeAPIKey
			case "shadow":
				parent := int64(9)
				accounts.account.ParentAccountID = &parent
			}
			repo := &intelligenceOAuthStatusRepository{intelligenceIndependentScheduleRepository: intelligenceIndependentScheduleRepository{
				intelligenceTestRepository: intelligenceTestRepository{plan: &IntelligenceMonitorPlan{ID: 3, SourceType: "openai_oauth", AccountID: &id, Enabled: true, CandyEnabled: true}},
				artworkDue:                 []int64{3}, candyDue: []int64{3},
			}}
			svc.repo = repo
			svc.schedule()
			require.Empty(t, repo.queuedRuns)
			require.Equal(t, 2, repo.deferred)
			for _, kind := range []string{IntelligenceMonitorTestPelican, IntelligenceMonitorTestCandy} {
				_, err := svc.enqueueTest(context.Background(), 3, false, kind)
				require.ErrorIs(t, err, ErrIntelligenceOAuthUnavailable)
			}
		})
	}
}

func TestIntelligenceOAuthCooldownCanBeManuallyPausedAndCannotBeEnabled(t *testing.T) {
	svc, accounts, _ := intelligenceOAuthFixture()
	reset := time.Now().Add(time.Hour)
	accounts.account.Extra = map[string]any{"codex_7d_used_percent": 100, "codex_7d_reset_at": reset.Format(time.RFC3339Nano)}
	id := accounts.account.ID
	repo := &intelligenceTestRepository{plan: &IntelligenceMonitorPlan{ID: 3, Name: "OAuth", SourceType: "openai_oauth", AccountID: &id, Enabled: true}}
	svc.repo = repo
	enabled := false
	plan, err := svc.SavePlan(context.Background(), 3, 1, IntelligenceMonitorInput{Enabled: &enabled})
	require.NoError(t, err)
	require.False(t, plan.Enabled)
	enabled = true
	_, err = svc.SavePlan(context.Background(), 3, 1, IntelligenceMonitorInput{Enabled: &enabled})
	require.ErrorIs(t, err, ErrIntelligenceOAuthCoolingDown)
}

func TestIntelligenceOAuthStatusUsesOneBatchAndExposesOnlySafeFields(t *testing.T) {
	svc, accounts, _ := intelligenceOAuthFixture()
	accounts.account.Groups = []*Group{{ID: 7, Name: "Primary", Description: "private group configuration"}, nil, {ID: 9, Name: "Secondary", Status: "inactive"}}
	id, missing := accounts.account.ID, int64(999)
	repo := &intelligenceOAuthStatusRepository{accounts: map[int64]*Account{id: accounts.account}}
	svc.repo = repo
	plans := []*IntelligenceMonitorPlan{
		{ID: 1, SourceType: "openai_oauth", AccountID: &id},
		{ID: 2, SourceType: "openai_oauth", AccountID: &id},
		{ID: 3, SourceType: "openai_oauth", AccountID: &missing},
		{ID: 4, SourceType: "external"},
		{ID: 5, SourceType: "openai_oauth"},
	}
	require.NoError(t, svc.populateOAuthMonitorStatus(context.Background(), plans))
	require.Equal(t, 1, repo.loads)
	require.Equal(t, []int64{id, missing}, repo.loadIDs)
	require.Equal(t, "normal", plans[0].OAuthAccountStatus.Status)
	require.Equal(t, []IntelligenceOAuthAccountGroup{{ID: 7, Name: "Primary"}, {ID: 9, Name: "Secondary"}}, plans[0].OAuthAccountStatus.Groups)
	require.Equal(t, "unavailable", plans[2].OAuthAccountStatus.Status)
	require.NotNil(t, plans[2].OAuthAccountStatus.Groups)
	require.Empty(t, plans[2].OAuthAccountStatus.Groups)
	require.Nil(t, plans[3].OAuthAccountStatus)
	require.Equal(t, "unavailable", plans[4].OAuthAccountStatus.Status)
	encoded, err := json.Marshal(plans)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "private-oauth-token")
	require.NotContains(t, string(encoded), "credentials")
	require.NotContains(t, string(encoded), "private group configuration")
	require.Contains(t, string(encoded), `"groups":[]`)
	reset := time.Now().Add(time.Hour)
	accounts.account.Extra = map[string]any{"codex_7d_used_percent": 100, "codex_7d_reset_at": reset.Format(time.RFC3339Nano)}
	accounts.account.Groups = []*Group{{ID: 10, Name: "Rebound"}}
	require.NoError(t, svc.populateOAuthMonitorStatus(context.Background(), plans))
	require.Equal(t, "weekly_limited", plans[0].OAuthAccountStatus.Status)
	require.True(t, plans[0].OAuthAccountStatus.MonitoringPaused)
	require.Equal(t, []IntelligenceOAuthAccountGroup{{ID: 10, Name: "Rebound"}}, plans[0].OAuthAccountStatus.Groups, "quota suspension must not freeze the account's group display")
	accounts.account.Groups = nil
	require.NoError(t, svc.populateOAuthMonitorStatus(context.Background(), plans))
	require.NotNil(t, plans[0].OAuthAccountStatus.Groups)
	require.Empty(t, plans[0].OAuthAccountStatus.Groups)
	repo.loadErr = errors.New("status snapshot unavailable")
	require.ErrorIs(t, svc.populateOAuthMonitorStatus(context.Background(), plans), repo.loadErr, "a failed status query must not present accounts as normal")
}

func TestIntelligenceOAuthCooldownDefersOnlyUntilSoonerResetAndPropagatesStorageError(t *testing.T) {
	svc, accounts, _ := intelligenceOAuthFixture()
	id := accounts.account.ID
	reset := time.Now().Add(3 * time.Second)
	accounts.account.Extra = map[string]any{"codex_7d_used_percent": 100, "codex_7d_reset_at": reset.Format(time.RFC3339Nano)}
	repo := &intelligenceOAuthStatusRepository{intelligenceIndependentScheduleRepository: intelligenceIndependentScheduleRepository{intelligenceTestRepository: intelligenceTestRepository{plan: &IntelligenceMonitorPlan{ID: 3, SourceType: "openai_oauth", AccountID: &id}}}}
	svc.repo = repo
	_, err := svc.Enqueue(context.Background(), 3)
	require.ErrorIs(t, err, ErrIntelligenceOAuthCoolingDown)
	require.True(t, reset.Equal(repo.deferredUntil))
	repo.deferErr = errors.New("store unavailable")
	_, err = svc.Enqueue(context.Background(), 3)
	require.ErrorIs(t, err, repo.deferErr)
	require.Empty(t, repo.queuedRuns)
}
