package service

import (
	"context"
	"strings"
	"time"
	"unicode/utf8"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

var (
	ErrUpstreamStorageInvalid  = infraerrors.BadRequest("UPSTREAM_STORAGE_INVALID", "invalid permanent deletion request")
	ErrUpstreamStorageConfirm  = infraerrors.Conflict("UPSTREAM_STORAGE_CONFIRMATION_MISMATCH", "the confirmation name does not match the current item name")
	ErrUpstreamStorageBusy     = infraerrors.Conflict("UPSTREAM_STORAGE_BUSY", "monitoring or balance synchronization is active; retry after it finishes")
	ErrUpstreamStorageNotFound = infraerrors.NotFound("UPSTREAM_STORAGE_NOT_FOUND", "the upstream storage item no longer exists")
)

const upstreamStoragePurgeTimeout = 45 * time.Second

// This optional repository interface keeps storage administration independent
// from the inventory/monitoring interface and its scheduler/test implementations.
// Keys are returned only to invalidate server-side authentication caches.
type UpstreamStoragePurgeRepository interface {
	ListStorageArchives(context.Context) (*UpstreamStorageArchivePage, error)
	PurgeStorage(context.Context, UpstreamStoragePurgeInput) ([]string, error)
}

func (s *UpstreamCenterService) ListStorageArchives(ctx context.Context) (*UpstreamStorageArchivePage, error) {
	repo, ok := s.repo.(UpstreamStoragePurgeRepository)
	if !ok {
		return nil, ErrUpstreamStorageUnavailable
	}
	return repo.ListStorageArchives(ctx)
}

func (s *UpstreamCenterService) PurgeStorage(ctx context.Context, in UpstreamStoragePurgeInput) error {
	if in.ID <= 0 || strings.TrimSpace(in.ConfirmName) == "" || utf8.RuneCountInString(in.ConfirmName) > 100 {
		return ErrUpstreamStorageInvalid
	}
	if in.Kind != "supplier" && in.Kind != "target" && in.Kind != "intelligence" {
		return ErrUpstreamStorageInvalid
	}
	repo, ok := s.repo.(UpstreamStoragePurgeRepository)
	if !ok || (in.Kind == "intelligence" && s.storageKeys == nil) {
		return ErrUpstreamStorageUnavailable
	}
	// Bound lock waits and the transaction itself below the administrator HTTP
	// timeout, reserving time for post-commit authentication cache invalidation.
	purgeCtx, cancelPurge := context.WithTimeout(ctx, upstreamStoragePurgeTimeout)
	keys, err := repo.PurgeStorage(purgeCtx, in)
	cancelPurge()
	if err != nil {
		return err
	}
	if s.storageKeys != nil && len(keys) > 0 {
		// The database transaction already disabled the dedicated keys. Ensure a
		// disconnected administrator request cannot skip cache invalidation.
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cancel()
		for _, key := range keys {
			s.storageKeys.InvalidateAuthCacheByKey(cleanupCtx, key)
		}
	}
	return nil
}

func (s *UpstreamCenterService) SetStorageAPIKeys(keys *APIKeyService) {
	s.storageKeys = keys
}
