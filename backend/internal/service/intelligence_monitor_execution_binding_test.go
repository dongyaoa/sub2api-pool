//go:build unit

package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type intelligenceExecutionBindingStub struct {
	intelligenceTestRepository
	accountID int64
	at        time.Time
	result    map[string]any
	err       error
}

func (r *intelligenceExecutionBindingStub) IntelligenceExecutionBinding(_ context.Context, id int64, at time.Time) (map[string]any, error) {
	r.accountID, r.at = id, at
	return r.result, r.err
}
func TestIntelligenceExecutionBindingUsesExactAttemptAndPersistsWithBothKinds(t *testing.T) {
	for _, kind := range []string{IntelligenceMonitorTestCandy, IntelligenceMonitorTestPelican} {
		t.Run(kind, func(t *testing.T) {
			at := time.Date(2026, 9, 27, 9, 0, 0, 123000000, time.UTC)
			repo := &intelligenceExecutionBindingStub{result: map[string]any{"execution_binding_status": "matched", "execution_supplier_name": "Historical supplier"}}
			svc := NewIntelligenceMonitorService(repo, nil, nil, nil, nil, nil, nil)
			run := &IntelligenceMonitorRun{TestKind: kind, SourceType: "local_group", RawText: "21", SourceSnapshot: map[string]any{"execution_account_id": int64(42), "execution_started_at": at.Format(time.RFC3339Nano), "execution_account_name": "Account used", "execution_source_status": "completed"}}
			if kind == IntelligenceMonitorTestPelican {
				run.RawText = "<html><svg></svg></html>"
			}
			svc.finishIntelligenceRun(run)
			require.Equal(t, int64(42), repo.accountID)
			require.True(t, at.Equal(repo.at))
			require.Equal(t, "Historical supplier", repo.completed.SourceSnapshot["execution_supplier_name"])
			require.Equal(t, "Account used", repo.completed.SourceSnapshot["execution_account_name"])
			require.Equal(t, "succeeded", repo.completed.Status)
		})
	}
}
func TestIntelligenceExecutionBindingFailureDoesNotLoseCompletedArtwork(t *testing.T) {
	repo := &intelligenceExecutionBindingStub{err: errors.New("database unavailable")}
	svc := NewIntelligenceMonitorService(repo, nil, nil, nil, nil, nil, nil)
	run := &IntelligenceMonitorRun{SourceType: "local_group", RawText: "<html><svg></svg></html>", SourceSnapshot: map[string]any{"execution_account_id": int64(42), "execution_started_at": time.Now().UTC().Format(time.RFC3339Nano)}}
	svc.finishIntelligenceRun(run)
	require.Equal(t, "succeeded", repo.completed.Status)
	require.Equal(t, "unavailable", repo.completed.SourceSnapshot["execution_binding_status"])
	// No attempt or historical records must not acquire an arbitrary current binding.
	repo.accountID = 0
	svc.captureIntelligenceExecutionBinding(context.Background(), &IntelligenceMonitorRun{SourceType: "local_group"})
	require.Zero(t, repo.accountID)
}
