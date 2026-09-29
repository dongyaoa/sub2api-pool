//go:build unit

package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

// The queue returns independent execution copies, as the SQL repository does.
// All fixtures use a local RoundTripper; none of these tests contacts an upstream.
type intelligenceSchedulerRepository struct {
	IntelligenceMonitorRepository
	mu        sync.Mutex
	nextID    int64
	pending   []*IntelligenceMonitorRun
	active    map[int64]*IntelligenceMonitorRun
	completed chan *IntelligenceMonitorRun
	due       func(context.Context) ([]int64, error)
	claimWait func(context.Context, string) error
}

func (r *intelligenceSchedulerRepository) GetPlan(_ context.Context, id int64) (*IntelligenceMonitorPlan, error) {
	return &IntelligenceMonitorPlan{
		ID: id, Name: fmt.Sprintf("Plan %d", id), SourceType: "external",
		Endpoint: "https://8.8.8.8", APIKeyEncrypted: "encrypted:fixture-secret",
		APIMode: MonitorAPIModeResponses, TimeoutSeconds: 600, CandyEnabled: true,
	}, nil
}

func (r *intelligenceSchedulerRepository) Enqueue(_ context.Context, run *IntelligenceMonitorRun, _ bool) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, active := range r.active {
		if active.PlanID == run.PlanID && active.TestKind == run.TestKind {
			return ErrIntelligenceBusy
		}
	}
	for _, pending := range r.pending {
		if pending.PlanID == run.PlanID && pending.TestKind == run.TestKind {
			return ErrIntelligenceBusy
		}
	}
	r.nextID++
	run.ID, run.Status = r.nextID, "pending"
	copy := *run
	r.pending = append(r.pending, &copy)
	return nil
}

func (r *intelligenceSchedulerRepository) ClaimNextForKind(ctx context.Context, token, kind string, limit int) (*IntelligenceMonitorRun, error) {
	if r.claimWait != nil {
		if err := r.claimWait(ctx, kind); err != nil {
			return nil, err
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	activeForKind := 0
	for _, run := range r.active {
		if run.TestKind == kind {
			activeForKind++
		}
	}
	if activeForKind >= limit {
		return nil, nil
	}
	for i, run := range r.pending {
		if run.TestKind != kind {
			continue
		}
		busy := false
		for _, active := range r.active {
			busy = busy || (active.PlanID == run.PlanID && active.TestKind == run.TestKind)
		}
		if busy {
			continue
		}
		r.pending = append(r.pending[:i], r.pending[i+1:]...)
		copy := *run
		copy.Status, copy.LeaseToken = "running", token
		r.active[copy.ID] = &copy
		result := copy
		return &result, nil
	}
	return nil, nil
}

func (r *intelligenceSchedulerRepository) CompleteRun(_ context.Context, run *IntelligenceMonitorRun) error {
	r.mu.Lock()
	delete(r.active, run.ID)
	r.mu.Unlock()
	copy := *run
	r.completed <- &copy
	return nil
}

func (r *intelligenceSchedulerRepository) DuePlanIDs(ctx context.Context, _ int) ([]int64, error) {
	if r.due != nil {
		return r.due(ctx)
	}
	return nil, nil
}

func (*intelligenceSchedulerRepository) DueCandyPlanIDs(context.Context, int) ([]int64, error) {
	return nil, nil
}

func (*intelligenceSchedulerRepository) PruneRuns(context.Context) error  { return nil }
func (*intelligenceSchedulerRepository) ExpireRuns(context.Context) error { return nil }

type intelligenceSchedulerRequest struct {
	kind    string
	release chan struct{}
}

type intelligenceSchedulerTransport struct {
	started chan *intelligenceSchedulerRequest
	active  atomic.Int64
}

func (tr *intelligenceSchedulerTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	var body struct {
		Input string `json:"input"`
	}
	if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
		return nil, err
	}
	kind := IntelligenceMonitorTestPelican
	response := `{"output_text":"<!doctype html><html><body><svg></svg></body></html>","status":"completed"}`
	if body.Input == IntelligenceMonitorCandyPrompt {
		kind = IntelligenceMonitorTestCandy
		response = `{"output_text":"21","status":"completed"}`
	}
	flight := &intelligenceSchedulerRequest{kind: kind, release: make(chan struct{})}
	tr.active.Add(1)
	defer tr.active.Add(-1)
	tr.started <- flight
	select {
	case <-request.Context().Done():
		return nil, request.Context().Err()
	case <-flight.release:
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(response))}, nil
	}
}

func newIntelligenceSchedulerFixture(t *testing.T, cfg *config.Config) (*IntelligenceMonitorService, *intelligenceSchedulerRepository, *intelligenceSchedulerTransport) {
	t.Helper()
	capacity := IntelligenceMonitorMaxConcurrency + IntelligenceMonitorCandyMaxConcurrency
	repo := &intelligenceSchedulerRepository{active: make(map[int64]*IntelligenceMonitorRun), completed: make(chan *IntelligenceMonitorRun, capacity)}
	transport := &intelligenceSchedulerTransport{started: make(chan *intelligenceSchedulerRequest, capacity)}
	svc := NewIntelligenceMonitorService(repo, upstreamTestEncryptor{}, nil, nil, nil, nil, cfg)
	svc.externalClient = &http.Client{Transport: transport}
	t.Cleanup(svc.Stop)
	return svc, repo, transport
}

func awaitIntelligenceSchedulerRequest(t *testing.T, transport *intelligenceSchedulerTransport, timeout time.Duration) *intelligenceSchedulerRequest {
	t.Helper()
	select {
	case request := <-transport.started:
		return request
	case <-time.After(timeout):
		t.Fatal("queued generation did not start before the deadline")
		return nil
	}
}

func requireNoIntelligenceSchedulerRequest(t *testing.T, transport *intelligenceSchedulerTransport) {
	t.Helper()
	select {
	case request := <-transport.started:
		t.Fatalf("unexpected extra %s request while its execution pool is full", request.kind)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestIntelligenceSchedulerRunsParallelWithinConfiguredLimit(t *testing.T) {
	for _, tc := range []struct {
		name  string
		limit int
		cfg   *config.Config
	}{
		{name: "default eight", limit: 8},
		{name: "forty eight exceeds former ceiling", limit: 48, cfg: &config.Config{IntelligenceMonitor: config.IntelligenceMonitorConfig{MaxConcurrency: 48}}},
		{name: "configured three", limit: 3, cfg: func() *config.Config {
			cfg := &config.Config{}
			cfg.IntelligenceMonitor.MaxConcurrency = 3
			return cfg
		}()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, repo, transport := newIntelligenceSchedulerFixture(t, tc.cfg)
			for id := 1; id <= tc.limit+1; id++ {
				_, err := svc.Enqueue(context.Background(), int64(id))
				require.NoError(t, err)
			}
			svc.Start()
			var first *intelligenceSchedulerRequest
			for i := 0; i < tc.limit; i++ {
				flight := awaitIntelligenceSchedulerRequest(t, transport, 2*time.Second)
				require.Equal(t, IntelligenceMonitorTestPelican, flight.kind)
				if first == nil {
					first = flight
				}
			}
			require.EqualValues(t, tc.limit, transport.active.Load())
			requireNoIntelligenceSchedulerRequest(t, transport)

			close(first.release)
			select {
			case run := <-repo.completed:
				require.Equal(t, "succeeded", run.Status)
			case <-time.After(time.Second):
				t.Fatal("generation result was not saved")
			}
			// Completion must refill the free slot without waiting for the
			// one-second dispatch poll or three-second scheduling poll.
			replacement := awaitIntelligenceSchedulerRequest(t, transport, 500*time.Millisecond)
			require.Equal(t, IntelligenceMonitorTestPelican, replacement.kind)
			require.EqualValues(t, tc.limit, transport.active.Load())
			requireNoIntelligenceSchedulerRequest(t, transport)
		})
	}
}

func TestIntelligenceSchedulerCandyStartsWhileArtworkPoolIsFull(t *testing.T) {
	svc, _, transport := newIntelligenceSchedulerFixture(t, nil)
	for id := int64(1); id <= 9; id++ {
		_, err := svc.Enqueue(context.Background(), id)
		require.NoError(t, err)
	}
	svc.Start()
	for i := 0; i < 8; i++ {
		require.Equal(t, IntelligenceMonitorTestPelican, awaitIntelligenceSchedulerRequest(t, transport, 2*time.Second).kind)
	}
	for id := int64(20); id <= 24; id++ {
		_, err := svc.EnqueueCandy(context.Background(), id)
		require.NoError(t, err)
	}
	var firstCandy *intelligenceSchedulerRequest
	for i := 0; i < 4; i++ {
		flight := awaitIntelligenceSchedulerRequest(t, transport, 500*time.Millisecond)
		require.Equal(t, IntelligenceMonitorTestCandy, flight.kind)
		if firstCandy == nil {
			firstCandy = flight
		}
	}
	require.EqualValues(t, 12, transport.active.Load())
	requireNoIntelligenceSchedulerRequest(t, transport)
	close(firstCandy.release)
	require.Equal(t, IntelligenceMonitorTestCandy, awaitIntelligenceSchedulerRequest(t, transport, 500*time.Millisecond).kind)
	require.EqualValues(t, 12, transport.active.Load())
	requireNoIntelligenceSchedulerRequest(t, transport)
}

func TestIntelligenceSchedulerStartsBothKindsForSamePlansAtSixtyFourConcurrency(t *testing.T) {
	cfg := &config.Config{IntelligenceMonitor: config.IntelligenceMonitorConfig{MaxConcurrency: 64, CandyMaxConcurrency: 64}}
	svc, repo, transport := newIntelligenceSchedulerFixture(t, cfg)
	for id := int64(1); id <= 64; id++ {
		_, err := svc.Enqueue(context.Background(), id)
		require.NoError(t, err)
	}
	svc.Start()
	deadline := time.Now().Add(2 * time.Second)
	for range 64 {
		flight := awaitIntelligenceSchedulerRequest(t, transport, time.Until(deadline))
		require.Equal(t, IntelligenceMonitorTestPelican, flight.kind)
	}
	// Every matching artwork request remains in flight. These companion tests
	// must start on enqueue, with no wait for a drawing or a scheduler tick.
	for id := int64(1); id <= 64; id++ {
		_, err := svc.EnqueueCandy(context.Background(), id)
		require.NoError(t, err)
	}
	deadline = time.Now().Add(time.Second)
	for range 64 {
		flight := awaitIntelligenceSchedulerRequest(t, transport, time.Until(deadline))
		require.Equal(t, IntelligenceMonitorTestCandy, flight.kind)
	}
	require.EqualValues(t, 128, transport.active.Load())
	repo.mu.Lock()
	active, pending := len(repo.active), len(repo.pending)
	repo.mu.Unlock()
	require.Equal(t, 128, active)
	require.Zero(t, pending, "idle candy capacity must not be blocked by the same plans' drawings")
	_, err := svc.Enqueue(context.Background(), 1)
	require.ErrorIs(t, err, ErrIntelligenceBusy, "the same plan and test kind is still deduplicated")
	_, err = svc.EnqueueCandy(context.Background(), 1)
	require.ErrorIs(t, err, ErrIntelligenceBusy)
}

func TestIntelligenceSchedulerSlowClaimCannotDelayOtherPoolOrRefill(t *testing.T) {
	for _, blockedKind := range []string{IntelligenceMonitorTestPelican, IntelligenceMonitorTestCandy} {
		t.Run(blockedKind, func(t *testing.T) {
			cfg := &config.Config{IntelligenceMonitor: config.IntelligenceMonitorConfig{MaxConcurrency: 1, CandyMaxConcurrency: 1}}
			svc, repo, transport := newIntelligenceSchedulerFixture(t, cfg)
			entered := make(chan struct{})
			var once sync.Once
			repo.claimWait = func(ctx context.Context, kind string) error {
				if kind != blockedKind {
					return nil
				}
				once.Do(func() { close(entered) })
				<-ctx.Done()
				return ctx.Err()
			}
			availableKind := IntelligenceMonitorTestPelican
			if blockedKind == availableKind {
				availableKind = IntelligenceMonitorTestCandy
			}
			for id := int64(1); id <= 2; id++ {
				_, err := svc.enqueueTest(context.Background(), id, false, availableKind)
				require.NoError(t, err)
			}
			svc.Start()
			select {
			case <-entered:
			case <-time.After(time.Second):
				t.Fatal("blocked pool never attempted its claim")
			}
			first := awaitIntelligenceSchedulerRequest(t, transport, 500*time.Millisecond)
			require.Equal(t, availableKind, first.kind)
			close(first.release)
			second := awaitIntelligenceSchedulerRequest(t, transport, 500*time.Millisecond)
			require.Equal(t, availableKind, second.kind, "completion must refill the available pool while the other claim remains blocked")
		})
	}
}

func TestIntelligenceSchedulerManualRunBypassesBlockedDueQuery(t *testing.T) {
	svc, repo, transport := newIntelligenceSchedulerFixture(t, nil)
	entered := make(chan struct{})
	var once sync.Once
	repo.due = func(ctx context.Context) ([]int64, error) {
		once.Do(func() { close(entered) })
		<-ctx.Done()
		return nil, ctx.Err()
	}
	svc.Start()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("scheduled-plan query did not start")
	}
	_, err := svc.Enqueue(context.Background(), 1)
	require.NoError(t, err)
	require.Equal(t, IntelligenceMonitorTestPelican, awaitIntelligenceSchedulerRequest(t, transport, 500*time.Millisecond).kind)
}

func TestIntelligenceSchedulerStopCancelsRequestsAndLeavesQueuePending(t *testing.T) {
	cfg := &config.Config{}
	cfg.IntelligenceMonitor.MaxConcurrency = 2
	cfg.IntelligenceMonitor.CandyMaxConcurrency = 1
	svc, repo, transport := newIntelligenceSchedulerFixture(t, cfg)
	for id := int64(1); id <= 3; id++ {
		_, err := svc.Enqueue(context.Background(), id)
		require.NoError(t, err)
	}
	for id := int64(10); id <= 11; id++ {
		_, err := svc.EnqueueCandy(context.Background(), id)
		require.NoError(t, err)
	}
	svc.Start()
	for i := 0; i < 3; i++ {
		awaitIntelligenceSchedulerRequest(t, transport, 2*time.Second)
	}
	requireNoIntelligenceSchedulerRequest(t, transport)
	stopped := make(chan struct{})
	go func() {
		svc.Stop()
		close(stopped)
	}()
	select {
	case <-stopped:
	case <-time.After(2 * time.Second):
		t.Fatal("stopping did not cancel generation and join its workers")
	}
	require.Zero(t, transport.active.Load(), "all HTTP requests must be cancelled before Stop returns")
	require.Len(t, repo.completed, 3, "each interrupted execution must save a terminal result")
	for i := 0; i < 3; i++ {
		run := <-repo.completed
		require.Equal(t, "failed", run.Status)
		require.NotEmpty(t, run.Error)
	}
	repo.mu.Lock()
	active, pending := len(repo.active), len(repo.pending)
	repo.mu.Unlock()
	require.Zero(t, active)
	require.Equal(t, 2, pending, "pending jobs must not start during shutdown")
}
