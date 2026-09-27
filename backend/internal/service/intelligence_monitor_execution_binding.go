package service

import (
	"context"
	"time"
)

type IntelligenceExecutionBindingRepository interface {
	IntelligenceExecutionBinding(context.Context, int64, time.Time) (map[string]any, error)
}

// Capture a historical binding once at completion, never re-resolve old artwork
// against an account's current upstream assignment when viewing its details.
func (s *IntelligenceMonitorService) captureIntelligenceExecutionBinding(ctx context.Context, run *IntelligenceMonitorRun) {
	if run.SourceType != "local_group" {
		return
	}
	id := intelligenceSnapshotID(run.SourceSnapshot["execution_account_id"])
	if id <= 0 {
		return
	}
	repo, ok := s.repo.(IntelligenceExecutionBindingRepository)
	if !ok {
		return
	}
	stamp, _ := run.SourceSnapshot["execution_started_at"].(string)
	at, err := time.Parse(time.RFC3339Nano, stamp)
	if err != nil {
		run.SourceSnapshot["execution_binding_status"] = "unknown"
		return
	}
	lookup, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	binding, err := repo.IntelligenceExecutionBinding(lookup, id, at)
	if err != nil {
		run.SourceSnapshot["execution_binding_status"] = "unavailable"
		return
	}
	for key, value := range binding {
		run.SourceSnapshot[key] = value
	}
}
