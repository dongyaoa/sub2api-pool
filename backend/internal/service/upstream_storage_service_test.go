package service

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type storagePolicyTestRepo struct {
	UpstreamCenterRepository
	policy             UpstreamStoragePolicy
	cleanupCalls       int
	saveCalls          int
	args               [2]int
	result             *UpstreamStorageCleanupResult
	recorded           *UpstreamStorageCleanupResult
	recordContextError error
	onCleanup          func()
}

func (r *storagePolicyTestRepo) GetStoragePolicy(context.Context) (*UpstreamStoragePolicy, error) {
	return &r.policy, nil
}
func (r *storagePolicyTestRepo) SaveStoragePolicy(_ context.Context, in UpstreamStoragePolicyInput) (*UpstreamStoragePolicy, error) {
	r.saveCalls++
	r.policy.Enabled, r.policy.HistoryRetentionDays, r.policy.SnapshotRetentionDays = *in.Enabled, *in.HistoryRetentionDays, *in.SnapshotRetentionDays
	return &r.policy, nil
}
func (r *storagePolicyTestRepo) RecordStorageCleanup(ctx context.Context, at time.Time, result *UpstreamStorageCleanupResult) error {
	r.recorded, r.recordContextError = result, ctx.Err()
	r.policy.LastCleanupAt, r.policy.LastResult = &at, result
	return nil
}
func (r *storagePolicyTestRepo) CleanupStorage(_ context.Context, history, snapshots int, _ time.Time) (*UpstreamStorageCleanupResult, error) {
	r.cleanupCalls++
	r.args = [2]int{history, snapshots}
	if r.onCleanup != nil {
		r.onCleanup()
	}
	return r.result, nil
}

func TestUpstreamStoragePolicyRequiresCompleteBoundedInput(t *testing.T) {
	repo := &storagePolicyTestRepo{}
	svc := NewUpstreamCenterService(repo, nil, nil, nil)
	enabled, history, snapshots := true, 30, 7
	valid := UpstreamStoragePolicyInput{Enabled: &enabled, HistoryRetentionDays: &history, SnapshotRetentionDays: &snapshots}
	for _, in := range []UpstreamStoragePolicyInput{{}, {Enabled: &enabled}, {HistoryRetentionDays: &history, SnapshotRetentionDays: &snapshots}} {
		_, err := svc.SaveStoragePolicy(context.Background(), in)
		require.ErrorIs(t, err, ErrUpstreamStoragePolicy)
	}
	for _, days := range []int{0, 29, 366} {
		in := valid
		in.HistoryRetentionDays = &days
		_, err := svc.SaveStoragePolicy(context.Background(), in)
		require.ErrorIs(t, err, ErrUpstreamStoragePolicy)
	}
	for _, days := range []int{0, 91} {
		in := valid
		in.SnapshotRetentionDays = &days
		_, err := svc.SaveStoragePolicy(context.Background(), in)
		require.ErrorIs(t, err, ErrUpstreamStoragePolicy)
	}
	require.Zero(t, repo.saveCalls)
	got, err := svc.SaveStoragePolicy(context.Background(), valid)
	require.NoError(t, err)
	require.Equal(t, history, got.HistoryRetentionDays)
	require.Zero(t, repo.cleanupCalls, "saving policy must not perform a synchronous destructive sweep")
}

func TestUpstreamStorageScheduledAndManualCleanup(t *testing.T) {
	now := time.Now().UTC()
	for _, tc := range []struct {
		name                                string
		enabled, recent, backlog, scheduled bool
		calls                               int
	}{
		{"disabled timer", false, false, false, true, 0},
		{"manual while disabled", false, false, false, false, 1},
		{"already caught up", true, true, false, true, 0},
		{"backlog continues", true, true, true, true, 1},
		{"first scheduled pass", true, false, false, true, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &storagePolicyTestRepo{policy: UpstreamStoragePolicy{Enabled: tc.enabled, HistoryRetentionDays: 30, SnapshotRetentionDays: 7}, result: &UpstreamStorageCleanupResult{HistoryDeleted: 10}}
			if tc.recent {
				repo.policy.LastCleanupAt = &now
				repo.policy.LastResult = &UpstreamStorageCleanupResult{HasMore: tc.backlog}
			}
			svc := NewUpstreamCenterService(repo, nil, nil, nil)
			_, err := svc.cleanupStorage(context.Background(), tc.scheduled)
			require.NoError(t, err)
			require.Equal(t, tc.calls, repo.cleanupCalls)
			if tc.calls > 0 {
				require.Equal(t, [2]int{30, 7}, repo.args)
				require.Same(t, repo.result, repo.recorded)
			}
		})
	}
}

func TestUpstreamStorageCleanupRecordsCommittedBatchAfterDisconnect(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	repo := &storagePolicyTestRepo{policy: UpstreamStoragePolicy{HistoryRetentionDays: 30, SnapshotRetentionDays: 7}, result: &UpstreamStorageCleanupResult{HistoryDeleted: 100}, onCleanup: cancel}
	svc := NewUpstreamCenterService(repo, nil, nil, nil)
	_, err := svc.CleanupStorage(ctx)
	require.NoError(t, err)
	require.Same(t, repo.result, repo.recorded)
	require.NoError(t, repo.recordContextError)
	svc.storageCleanupMu.Lock()
	_, err = svc.CleanupStorage(context.Background())
	svc.storageCleanupMu.Unlock()
	require.ErrorIs(t, err, ErrUpstreamStorageCleanupBusy)
	require.Equal(t, 1, repo.cleanupCalls)
}
