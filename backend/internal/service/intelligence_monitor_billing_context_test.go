//go:build unit

package service

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type intelligenceQuotaWrite struct {
	ctx      context.Context
	userID   int64
	platform string
	cost     float64
}

type intelligenceQuotaWriteRepository struct {
	UserPlatformQuotaRepository
	started chan intelligenceQuotaWrite
	release chan struct{}
	result  chan error
}

func (r *intelligenceQuotaWriteRepository) IncrementUsageWithReset(ctx context.Context, userID int64, platform string, cost float64, _ time.Time) error {
	r.started <- intelligenceQuotaWrite{ctx, userID, platform, cost}
	select {
	case <-r.release:
		err := ctx.Err()
		r.result <- err
		return err
	case <-ctx.Done():
		r.result <- ctx.Err()
		return ctx.Err()
	}
}

func TestIntelligenceQuotaPersistenceSurvivesCallerCancellationWithIndependentDeadline(t *testing.T) {
	for _, intelligence := range []bool{false, true} {
		t.Run(map[bool]string{false: "ordinary", true: "intelligence"}[intelligence], func(t *testing.T) {
			parent, cancel := context.WithCancel(context.Background())
			defer cancel()
			if intelligence {
				parent = context.WithValue(parent, boundUpstreamLifecycleContextKey{}, true)
			}
			repo := &intelligenceQuotaWriteRepository{started: make(chan intelligenceQuotaWrite, 1), release: make(chan struct{}), result: make(chan error, 1)}
			defer close(repo.release)
			cfg := &config.Config{RunMode: config.RunModeStandard}
			deps := &billingDeps{cfg: cfg, billingCacheService: &BillingCacheService{cfg: cfg}, deferredService: &DeferredService{}, userPlatformQuotaRepo: repo}
			params := &postUsageBillingParams{Cost: &CostBreakdown{TotalCost: 0.25, ActualCost: 0.25}, User: &User{ID: 7}, APIKey: &APIKey{ID: 11}, Account: &Account{ID: 55}, Platform: PlatformOpenAI}
			callerBilling, cancelCallerBilling := detachedBillingContext(parent)
			finalizePostUsageBilling(callerBilling, params, deps, nil)
			var write intelligenceQuotaWrite
			select {
			case write = <-repo.started:
			case <-time.After(time.Second):
				t.Fatal("asynchronous quota write did not start")
			}
			cancelCallerBilling()
			cancel()
			require.ErrorIs(t, callerBilling.Err(), context.Canceled)
			require.NoError(t, write.ctx.Err(), "quota persistence must survive both the request and billing caller finishing")
			deadline, bound := write.ctx.Deadline()
			require.True(t, bound, "quota persistence must have its own finite budget")
			require.InDelta(t, postUsageBillingTimeout.Seconds(), time.Until(deadline).Seconds(), 1)
			require.Equal(t, int64(7), write.userID)
			require.Equal(t, PlatformOpenAI, write.platform)
			require.Equal(t, 0.25, write.cost)
			repo.release <- struct{}{}
			select {
			case err := <-repo.result:
				require.NoError(t, err)
			case <-time.After(time.Second):
				t.Fatal("asynchronous quota write did not complete")
			}
			select {
			case <-write.ctx.Done():
				require.ErrorIs(t, write.ctx.Err(), context.Canceled, "completion must release the independent deadline")
			case <-time.After(time.Second):
				t.Fatal("completed quota write retained its context")
			}
		})
	}
}
