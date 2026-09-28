package service

import (
	"context"
	"log/slog"
	"time"
)

const IntelligenceMonitorCandyGradeVersion = 3
const IntelligenceMonitorCandyMaxGradeBytes = intelligenceCandyMaxInput
const intelligenceCandyRegradeBatchSize = 32

// The repository locks a bounded batch of completed responses. This keeps
// historical repair independent of model requests, dispatch and list polling.
type IntelligenceMonitorCandyRegradeRepository interface {
	RegradeCandyRuns(context.Context, int, int, func(string) (string, bool)) (int, error)
}

func (s *IntelligenceMonitorService) candyRegradeLoop() {
	defer s.wg.Done()
	repo, ok := s.repo.(IntelligenceMonitorCandyRegradeRepository)
	if !ok {
		return
	}
	for s.ctx.Err() == nil {
		ctx, cancel := context.WithTimeout(s.ctx, 5*time.Second)
		count, err := repo.RegradeCandyRuns(ctx, intelligenceCandyRegradeBatchSize, IntelligenceMonitorCandyGradeVersion, gradeIntelligenceCandyAnswer)
		cancel()
		delay := 30 * time.Second
		if err != nil {
			if s.ctx.Err() != nil {
				return
			}
			slog.Warn("intelligence candy historical grading failed", "error", err)
			delay = 5 * time.Second
		} else if count > 0 {
			slog.Info("intelligence candy historical grading repaired", "runs", count, "version", IntelligenceMonitorCandyGradeVersion)
			// Yield between full batches so a large history cannot monopolize
			// a database connection. Other instances use SKIP LOCKED batches.
			if count == intelligenceCandyRegradeBatchSize {
				delay = 50 * time.Millisecond
			}
		}
		timer := time.NewTimer(delay)
		select {
		case <-s.ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}
