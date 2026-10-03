package service

import (
	"context"
	"log/slog"
)

type IntelligenceScheduleStatus struct {
	Total   int64 `json:"total"`
	Enabled int64 `json:"enabled"`
}

type IntelligenceScheduleUpdate struct {
	IntelligenceScheduleStatus
	Updated int64 `json:"updated"`
}

type IntelligenceMonitorLifecycleRepository interface {
	DeletePelicanRun(context.Context, int64) error
	DeleteOAuthPlanPermanently(context.Context, int64) error
	ScheduleStatus(context.Context) (*IntelligenceScheduleStatus, error)
	SetPlansEnabled(context.Context, bool) (*IntelligenceScheduleUpdate, error)
	LiveOAuthExecutionIDs(context.Context, []int64) ([]int64, error)
}

func (s *IntelligenceMonitorService) DeletePelicanRun(ctx context.Context, id int64) error {
	if id <= 0 {
		return ErrIntelligenceInvalid
	}
	repo, ok := s.repo.(IntelligenceMonitorLifecycleRepository)
	if !ok {
		return ErrIntelligenceFilterUnavailable
	}
	return repo.DeletePelicanRun(ctx, id)
}

func (s *IntelligenceMonitorService) DeleteOAuthPlanPermanently(ctx context.Context, id int64) error {
	if id <= 0 {
		return ErrIntelligenceInvalid
	}
	repo, ok := s.repo.(IntelligenceMonitorLifecycleRepository)
	if !ok {
		return ErrIntelligenceFilterUnavailable
	}
	if err := repo.DeleteOAuthPlanPermanently(ctx, id); err != nil {
		return err
	}
	s.cancelDeletedOAuthExecutions(ctx)
	s.notify()
	return nil
}

func (s *IntelligenceMonitorService) ScheduleStatus(ctx context.Context) (*IntelligenceScheduleStatus, error) {
	repo, ok := s.repo.(IntelligenceMonitorLifecycleRepository)
	if !ok {
		return nil, ErrIntelligenceFilterUnavailable
	}
	return repo.ScheduleStatus(ctx)
}

func (s *IntelligenceMonitorService) SetPlansEnabled(ctx context.Context, enabled bool) (*IntelligenceScheduleUpdate, error) {
	repo, ok := s.repo.(IntelligenceMonitorLifecycleRepository)
	if !ok {
		return nil, ErrIntelligenceFilterUnavailable
	}
	status, err := repo.SetPlansEnabled(ctx, enabled)
	if err == nil {
		s.notifySchedule()
	}
	return status, err
}

// A single batched scheduler check cancels removed OAuth executions across app
// instances. Bulk schedule pause preserves running requests.
func (s *IntelligenceMonitorService) registerOAuthExecution(ctx context.Context, run *IntelligenceMonitorRun, cancel context.CancelFunc) func() {
	if run.SourceType != "openai_oauth" {
		return func() {}
	}
	repo, ok := s.repo.(IntelligenceMonitorLifecycleRepository)
	if !ok {
		return func() {}
	}
	s.mu.Lock()
	if s.oauthExecutions == nil {
		s.oauthExecutions = map[int64]context.CancelFunc{}
	}
	s.oauthExecutions[run.ID] = cancel
	s.mu.Unlock()
	live, err := repo.LiveOAuthExecutionIDs(ctx, []int64{run.ID})
	if err == nil && len(live) == 0 {
		cancel()
	}
	return func() {
		s.mu.Lock()
		delete(s.oauthExecutions, run.ID)
		s.mu.Unlock()
	}
}

func (s *IntelligenceMonitorService) cancelDeletedOAuthExecutions(ctx context.Context) {
	repo, ok := s.repo.(IntelligenceMonitorLifecycleRepository)
	if !ok {
		return
	}
	s.mu.Lock()
	cancels := make(map[int64]context.CancelFunc, len(s.oauthExecutions))
	ids := make([]int64, 0, len(s.oauthExecutions))
	for id, cancel := range s.oauthExecutions {
		ids = append(ids, id)
		cancels[id] = cancel
	}
	s.mu.Unlock()
	if len(ids) == 0 {
		return
	}
	live, err := repo.LiveOAuthExecutionIDs(ctx, ids)
	if err != nil {
		if ctx.Err() == nil {
			slog.Warn("intelligence OAuth lifecycle check failed", "error", err)
		}
		return
	}
	for _, id := range live {
		delete(cancels, id)
	}
	for _, cancel := range cancels {
		cancel()
	}
}
