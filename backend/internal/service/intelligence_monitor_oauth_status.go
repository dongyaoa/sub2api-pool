package service

import (
	"context"
	"math"
	"time"
)

// This DTO contains no account credentials or provider diagnostics.
type IntelligenceOAuthAccountStatus struct {
	Status            string                          `json:"status"`
	MonitoringPaused  bool                            `json:"monitoring_paused"`
	ResetAt           *time.Time                      `json:"reset_at,omitempty"`
	WeeklyUsedPercent *float64                        `json:"weekly_used_percent,omitempty"`
	Groups            []IntelligenceOAuthAccountGroup `json:"groups"`
}

type IntelligenceOAuthAccountGroup struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

type IntelligenceOAuthMonitorRepository interface {
	LoadOAuthMonitorAccounts(context.Context, []int64) (map[int64]*Account, error)
	DeferOAuthMonitor(context.Context, int64, int64, time.Time) error
}

func intelligenceOAuthAccountStatus(account *Account, now time.Time) *IntelligenceOAuthAccountStatus {
	status := &IntelligenceOAuthAccountStatus{Status: "normal", Groups: []IntelligenceOAuthAccountGroup{}}
	if account != nil {
		for _, group := range account.Groups {
			if group != nil {
				status.Groups = append(status.Groups, IntelligenceOAuthAccountGroup{ID: group.ID, Name: group.Name})
			}
		}
	}
	if account == nil || !account.IsOpenAIOAuth() || account.IsShadow() || account.IsSyntheticUITest() {
		status.Status, status.MonitoringPaused = "unavailable", true
		return status
	}
	unavailable := !account.IsActive() || !account.Schedulable ||
		(account.AutoPauseOnExpired && account.ExpiresAt != nil && !now.Before(*account.ExpiresAt))
	if !openAICodexSnapshotIdentityTrusted(account) {
		if unavailable {
			status.Status, status.MonitoringPaused = "unavailable", true
		}
		return status
	}
	_, weekly := openAICanonicalQuotaWindows(account.Extra, now)
	validUsage := weekly.hasUsed && !math.IsNaN(weekly.usedPercent) && !math.IsInf(weekly.usedPercent, 0)
	reset, hasReset := intelligenceOAuthWeeklyReset(account.Extra)
	if !hasReset && validUsage && weekly.usedPercent >= 100 {
		// Generic 429 deadlines alone never prove weekly exhaustion. When a
		// weekly snapshot does prove it, the stored deadline lets it recover.
		if account.RateLimitResetAt != nil {
			reset, hasReset = *account.RateLimitResetAt, true
		} else if observed := parseSchedulingResetAt(account.Extra["codex_usage_updated_at"]); observed != nil {
			// An incomplete old snapshot must not suspend monitoring forever.
			reset, hasReset = observed.Add(7*24*time.Hour), true
		}
	}
	recovered := weekly.reset || (hasReset && !now.Before(reset))
	if validUsage {
		used := weekly.usedPercent
		if recovered {
			used = 0
		}
		status.WeeklyUsedPercent = &used
	}
	if validUsage && weekly.usedPercent >= 100 && !recovered {
		status.Status, status.MonitoringPaused = "weekly_limited", true
		if hasReset {
			status.ResetAt = &reset
		}
	}
	// An explicit weekly threshold pause is distinct from a transient 429 or a
	// five-hour threshold. Respect the actual persisted cooldown, even below 100%.
	if account.TempUnschedulableUntil != nil && now.Before(*account.TempUnschedulableUntil) {
		if reason, ok := parseTempUnschedReasonPayload(account.TempUnschedulableReason); ok && reason.Source == AccountSchedulingThresholdReasonSource && reason.Platform == PlatformOpenAI && reason.Window == "7d" {
			status.Status, status.MonitoringPaused = "weekly_limited", true
			if status.ResetAt == nil || account.TempUnschedulableUntil.After(*status.ResetAt) {
				status.ResetAt = account.TempUnschedulableUntil
			}
		}
	}
	if status.Status == "normal" && unavailable {
		status.Status, status.MonitoringPaused = "unavailable", true
	}
	return status
}

func intelligenceOAuthWeeklyReset(extra map[string]any) (time.Time, bool) {
	if _, canonical := resolveAccountExtraNumber(extra, "codex_7d_used_percent"); canonical {
		return openAICodexWindowResetAt(extra, "7d")
	}
	// Keep raw primary/secondary classification identical to the existing gateway.
	snapshot := &OpenAICodexUsageSnapshot{}
	if used, ok := resolveAccountExtraNumber(extra, "codex_primary_used_percent"); ok {
		snapshot.PrimaryUsedPercent = &used
	}
	if used, ok := resolveAccountExtraNumber(extra, "codex_secondary_used_percent"); ok {
		snapshot.SecondaryUsedPercent = &used
	}
	if minutes := parseExtraInt(extra["codex_primary_window_minutes"]); minutes > 0 {
		snapshot.PrimaryWindowMinutes = &minutes
	}
	if minutes := parseExtraInt(extra["codex_secondary_window_minutes"]); minutes > 0 {
		snapshot.SecondaryWindowMinutes = &minutes
	}
	normalized := snapshot.Normalize()
	if normalized == nil || normalized.Used7dPercent == nil {
		return time.Time{}, false
	}
	window := "secondary"
	if normalized.Used7dPercent == snapshot.PrimaryUsedPercent {
		window = "primary"
	}
	return openAICodexWindowResetAt(extra, window)
}

func (s *IntelligenceMonitorService) populateOAuthMonitorStatus(ctx context.Context, plans []*IntelligenceMonitorPlan) error {
	ids, seen := []int64{}, map[int64]bool{}
	for _, plan := range plans {
		if plan.SourceType == "openai_oauth" && plan.AccountID != nil && !seen[*plan.AccountID] {
			seen[*plan.AccountID] = true
			ids = append(ids, *plan.AccountID)
		}
	}
	accounts := map[int64]*Account{}
	if repo, ok := s.repo.(IntelligenceOAuthMonitorRepository); ok && len(ids) > 0 {
		var err error
		accounts, err = repo.LoadOAuthMonitorAccounts(ctx, ids)
		if err != nil {
			return err
		}
	} else if s.accounts != nil {
		// Compatibility for in-memory stores; production uses the batched projection.
		for _, id := range ids {
			if account, err := s.accounts.GetByID(ctx, id); err == nil {
				accounts[id] = account
			}
		}
	}
	now := time.Now()
	for _, plan := range plans {
		if plan.SourceType != "openai_oauth" {
			continue
		}
		var account *Account
		if plan.AccountID != nil {
			account = accounts[*plan.AccountID]
		}
		plan.OAuthAccountStatus = intelligenceOAuthAccountStatus(account, now)
	}
	return nil
}

func (s *IntelligenceMonitorService) deferOAuthMonitor(ctx context.Context, plan *IntelligenceMonitorPlan, status *IntelligenceOAuthAccountStatus) error {
	if repo, ok := s.repo.(IntelligenceOAuthMonitorRepository); ok {
		// Recheck early recovery without creating failed runs or changing Enabled.
		until := time.Now().Add(30 * time.Second)
		if status.ResetAt != nil && status.ResetAt.Before(until) {
			until = *status.ResetAt
		}
		var accountID int64
		if plan.AccountID != nil {
			accountID = *plan.AccountID
		}
		return repo.DeferOAuthMonitor(ctx, plan.ID, accountID, until)
	}
	return nil
}
