//go:build unit

package repository

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestSchedulerSnapshotRebuildsLegacyProxyPoolMetadata(t *testing.T) {
	ctx := context.Background()
	cache := newSchedulerCacheUnit(t)
	bucket := service.SchedulerBucket{GroupID: 9, Platform: service.PlatformOpenAI, Mode: service.SchedulerModeSingle}
	account := service.Account{ID: 9301, ProxyPool: []service.AccountProxyPoolEntry{
		{ProxyID: 81, Concurrency: 20, Proxy: &service.Proxy{ID: 81, Status: service.StatusActive}},
	}}
	token, err := cache.CaptureBucketWriteToken(ctx, bucket)
	require.NoError(t, err)
	require.NoError(t, cache.SetSnapshot(ctx, bucket, token, []service.Account{account}))

	snapshot, hit, err := cache.GetSnapshot(ctx, bucket)
	require.NoError(t, err)
	require.True(t, hit)
	require.Len(t, snapshot, 1)
	require.True(t, snapshot[0].ProxyPoolMetadata)
	service.SelectAccountProxy(snapshot[0])
	require.Equal(t, int64(81), *snapshot[0].ProxyID)

	legacy := buildSchedulerMetadataAccount(account)
	legacy.ProxyPoolMetadata = false
	legacy.ProxyPool[0].Proxy = nil
	payload, err := json.Marshal(legacy)
	require.NoError(t, err)
	require.NoError(t, cache.rdb.Set(ctx, schedulerAccountMetaKey("9301"), payload, 0).Err())
	_, hit, err = cache.GetSnapshot(ctx, bucket)
	require.NoError(t, err)
	require.False(t, hit, "legacy proxy metadata must rebuild rather than making the account unusable after upgrade")
}
