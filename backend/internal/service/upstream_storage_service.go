package service

import (
	"context"
	"errors"
	"log/slog"
	"time"
)

func (s *UpstreamCenterService) StoragePolicy(ctx context.Context) (*UpstreamStoragePolicy, error) {
	repo, ok := s.repo.(UpstreamStorageRepository)
	if !ok {
		return nil, ErrUpstreamStorageUnavailable
	}
	return repo.GetStoragePolicy(ctx)
}

func (s *UpstreamCenterService) SaveStoragePolicy(ctx context.Context, in UpstreamStoragePolicyInput) (*UpstreamStoragePolicy, error) {
	if in.Enabled == nil || in.HistoryRetentionDays == nil || in.SnapshotRetentionDays == nil ||
		*in.HistoryRetentionDays < 30 || *in.HistoryRetentionDays > 365 ||
		*in.SnapshotRetentionDays < 1 || *in.SnapshotRetentionDays > 90 {
		return nil, ErrUpstreamStoragePolicy
	}
	repo, ok := s.repo.(UpstreamStorageRepository)
	if !ok {
		return nil, ErrUpstreamStorageUnavailable
	}
	return repo.SaveStoragePolicy(ctx, in)
}

// Manual cleanup is an explicit action and also works with the timer disabled.
func (s *UpstreamCenterService) CleanupStorage(ctx context.Context) (*UpstreamStorageCleanupResult, error) {
	return s.cleanupStorage(ctx, false)
}

func (s *UpstreamCenterService) cleanupStorage(ctx context.Context, scheduled bool) (*UpstreamStorageCleanupResult, error) {
	repo, ok := s.repo.(UpstreamStorageRepository)
	if !ok {
		return nil, ErrUpstreamStorageUnavailable
	}
	if !s.storageCleanupMu.TryLock() {
		return nil, ErrUpstreamStorageCleanupBusy
	}
	defer s.storageCleanupMu.Unlock()
	policy, err := repo.GetStoragePolicy(ctx)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	if scheduled && (!policy.Enabled || (policy.LastCleanupAt != nil && now.Sub(*policy.LastCleanupAt) < time.Hour && (policy.LastResult == nil || !policy.LastResult.HasMore))) {
		return nil, nil
	}
	cleanupCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	result, err := repo.CleanupStorage(cleanupCtx, policy.HistoryRetentionDays, policy.SnapshotRetentionDays, now)
	cancel()
	if err != nil {
		return nil, err
	}
	// A committed batch remains accounted for even if an HTTP caller disconnects.
	recordCtx, cancelRecord := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancelRecord()
	if err = repo.RecordStorageCleanup(recordCtx, time.Now().UTC(), result); err != nil {
		return nil, err
	}
	return result, nil
}

func (s *UpstreamCenterService) storageLoop() {
	defer s.wg.Done()
	// Bounded batches, one pass/minute while catching up and one pass/hour after.
	// No startup purge or exclusive VACUUM FULL on the application's database.
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			ctx, cancel := context.WithTimeout(s.ctx, 40*time.Second)
			_, err := s.cleanupStorage(ctx, true)
			cancel()
			if err != nil && s.ctx.Err() == nil && !errors.Is(err, ErrUpstreamStorageCleanupBusy) {
				slog.Warn("upstream storage cleanup failed", "error", err)
			}
		}
	}
}
