package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type storagePurgeRepoStub struct {
	UpstreamCenterRepository
	input       UpstreamStoragePurgeInput
	called      bool
	err         error
	keys        []string
	afterCommit func()
	context     context.Context
	awaitCancel bool
}

func (r *storagePurgeRepoStub) ListStorageArchives(context.Context) (*UpstreamStorageArchivePage, error) {
	return &UpstreamStorageArchivePage{Items: []*UpstreamStorageArchiveItem{}, Total: 0}, r.err
}

func (r *storagePurgeRepoStub) PurgeStorage(ctx context.Context, in UpstreamStoragePurgeInput) ([]string, error) {
	r.called, r.input = true, in
	r.context = ctx
	if r.awaitCancel {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	if r.afterCommit != nil {
		r.afterCommit()
	}
	return r.keys, r.err
}

type storagePurgeAuthCacheStub struct {
	APIKeyCache
	deleted, published []string
	contextError       error
	contextDeadline    time.Time
}

func (c *storagePurgeAuthCacheStub) DeleteAuthCache(ctx context.Context, key string) error {
	c.deleted = append(c.deleted, key)
	c.contextError = ctx.Err()
	c.contextDeadline, _ = ctx.Deadline()
	return nil
}

func (c *storagePurgeAuthCacheStub) PublishAuthCacheInvalidation(ctx context.Context, key string) error {
	c.published = append(c.published, key)
	c.contextError = ctx.Err()
	c.contextDeadline, _ = ctx.Deadline()
	return nil
}

func TestUpstreamStoragePurgeValidatesBeforeRepositoryMutation(t *testing.T) {
	for _, in := range []UpstreamStoragePurgeInput{
		{Kind: "supplier", ID: 0, ConfirmName: "supplier"},
		{Kind: "other", ID: 1, ConfirmName: "supplier"},
		{Kind: "target", ID: 1, ConfirmName: "  "},
		{Kind: "intelligence", ID: 1, ConfirmName: strings.Repeat("名", 101)},
	} {
		repo := &storagePurgeRepoStub{}
		svc := &UpstreamCenterService{repo: repo}
		require.ErrorIs(t, svc.PurgeStorage(context.Background(), in), ErrUpstreamStorageInvalid)
		require.False(t, repo.called)
	}
}

func TestUpstreamStoragePurgeRequiresCacheInvalidationCapabilityForPlans(t *testing.T) {
	repo := &storagePurgeRepoStub{}
	svc := &UpstreamCenterService{repo: repo}
	require.ErrorIs(t, svc.PurgeStorage(context.Background(), UpstreamStoragePurgeInput{Kind: "intelligence", ID: 1, ConfirmName: "plan"}), ErrUpstreamStorageUnavailable)
	require.False(t, repo.called)
}

func TestUpstreamStoragePurgePreservesExactConfirmationAndRepositoryError(t *testing.T) {
	repo := &storagePurgeRepoStub{err: ErrUpstreamStorageConfirm}
	svc := &UpstreamCenterService{repo: repo}
	input := UpstreamStoragePurgeInput{Kind: "supplier", ID: 1, ConfirmName: " exact name "}
	require.ErrorIs(t, svc.PurgeStorage(context.Background(), input), ErrUpstreamStorageConfirm)
	require.Equal(t, input, repo.input, "confirmation must never be silently trimmed into a match")
	_, err := svc.ListStorageArchives(context.Background())
	require.ErrorIs(t, err, ErrUpstreamStorageConfirm)
}

func TestUpstreamStoragePurgeBoundsRepositoryDeadline(t *testing.T) {
	repo := &storagePurgeRepoStub{}
	svc := &UpstreamCenterService{repo: repo}
	started := time.Now()
	require.NoError(t, svc.PurgeStorage(context.Background(), UpstreamStoragePurgeInput{Kind: "target", ID: 1, ConfirmName: "target"}))
	deadline, ok := repo.context.Deadline()
	require.True(t, ok, "permanent deletion must have a bounded transaction context")
	require.WithinRange(t, deadline, started.Add(45*time.Second), time.Now().Add(45*time.Second))
	require.ErrorIs(t, repo.context.Err(), context.Canceled, "release the repository timer once the transaction returns")
}

func TestUpstreamStoragePurgeAcceptsMaximumLengthAccountName(t *testing.T) {
	repo := &storagePurgeRepoStub{}
	svc := &UpstreamCenterService{repo: repo, storageKeys: &APIKeyService{}}
	input := UpstreamStoragePurgeInput{Kind: "intelligence", ID: 1, ConfirmName: strings.Repeat("名", 100)}
	require.NoError(t, svc.PurgeStorage(context.Background(), input))
	require.Equal(t, input, repo.input, "match the account schema's 100-character name limit")
}

func TestUpstreamStoragePurgePreservesShorterRequestDeadline(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Millisecond)
	defer cancel()
	repo := &storagePurgeRepoStub{awaitCancel: true}
	svc := &UpstreamCenterService{repo: repo}
	require.ErrorIs(t, svc.PurgeStorage(ctx, UpstreamStoragePurgeInput{Kind: "target", ID: 1, ConfirmName: "target"}), context.DeadlineExceeded)
	requestDeadline, _ := ctx.Deadline()
	purgeDeadline, ok := repo.context.Deadline()
	require.True(t, ok)
	require.Equal(t, requestDeadline, purgeDeadline, "the service must never extend a caller's deadline")
}

func TestUpstreamStoragePurgeInvalidatesAfterCommitDespiteCanceledRequest(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	cache := &storagePurgeAuthCacheStub{}
	keys := &APIKeyService{cache: cache}
	repo := &storagePurgeRepoStub{keys: []string{"dedicated-private-key"}, afterCommit: cancel}
	svc := &UpstreamCenterService{repo: repo}
	svc.SetStorageAPIKeys(keys)
	require.NoError(t, svc.PurgeStorage(ctx, UpstreamStoragePurgeInput{Kind: "intelligence", ID: 1, ConfirmName: "plan"}))
	expected := keys.authCacheKey("dedicated-private-key")
	require.Equal(t, []string{expected}, cache.deleted)
	require.Equal(t, []string{expected}, cache.published)
	require.NoError(t, cache.contextError)
	require.WithinRange(t, cache.contextDeadline, time.Now().Add(9*time.Second), time.Now().Add(10*time.Second), "committed key invalidation has a separate bounded context")
	cache.deleted, cache.published = nil, nil
	repo.err = errors.New("transaction rolled back")
	require.Error(t, svc.PurgeStorage(context.Background(), UpstreamStoragePurgeInput{Kind: "intelligence", ID: 1, ConfirmName: "plan"}))
	require.Empty(t, cache.deleted)
	require.Empty(t, cache.published)
}
