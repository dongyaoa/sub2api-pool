package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type intelligenceCandyRegradeTestRepository struct {
	IntelligenceMonitorRepository
	regrade func(context.Context, int, int, func(string) (string, bool)) (int, error)
}

func (r *intelligenceCandyRegradeTestRepository) RegradeCandyRuns(ctx context.Context, limit, version int, grade func(string) (string, bool)) (int, error) {
	return r.regrade(ctx, limit, version, grade)
}

func TestIntelligenceCandyHistoricalRegradeUsesCurrentParserAndStops(t *testing.T) {
	type result struct {
		answer  string
		correct bool
		limit   int
		version int
	}
	graded := make(chan result, 1)
	repo := &intelligenceCandyRegradeTestRepository{regrade: func(ctx context.Context, limit, version int, grade func(string) (string, bool)) (int, error) {
		answer, correct := grade(`所以答案是 **\(\boxed{21\text{个}}\)**。`)
		graded <- result{answer, correct, limit, version}
		return 1, nil
	}}
	svc := NewIntelligenceMonitorService(repo, nil, nil, nil, nil, nil, nil)
	svc.wg.Add(1)
	go svc.candyRegradeLoop()
	t.Cleanup(svc.Stop)
	select {
	case got := <-graded:
		require.Equal(t, "21", got.answer)
		require.True(t, got.correct)
		require.Equal(t, intelligenceCandyRegradeBatchSize, got.limit)
		require.Equal(t, IntelligenceMonitorCandyGradeVersion, got.version)
	case <-time.After(2 * time.Second):
		t.Fatal("historical repair did not start")
	}
	done := make(chan struct{})
	go func() { svc.Stop(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("shutdown waited for the historical repair interval")
	}
}

func TestIntelligenceCandyHistoricalRegradeDrainsFullBatches(t *testing.T) {
	called := make(chan int, 3)
	calls := 0
	repo := &intelligenceCandyRegradeTestRepository{regrade: func(ctx context.Context, limit, version int, grade func(string) (string, bool)) (int, error) {
		calls++
		called <- calls
		if calls == 1 {
			return limit, nil
		}
		return 0, nil
	}}
	svc := NewIntelligenceMonitorService(repo, nil, nil, nil, nil, nil, nil)
	svc.wg.Add(1)
	go svc.candyRegradeLoop()
	t.Cleanup(svc.Stop)
	for _, want := range []int{1, 2} {
		select {
		case got := <-called:
			require.Equal(t, want, got)
		case <-time.After(time.Second):
			t.Fatal("full batches did not continue promptly")
		}
	}
}
