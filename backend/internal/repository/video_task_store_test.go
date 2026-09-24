package repository

import (
	"context"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

func TestVideoTaskStorePreservesRecordsAndExpiry(t *testing.T) {
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	store := NewVideoTaskStore(rdb)
	ctx := context.Background()
	record := &service.VideoTaskRecord{ID: "video-1", UserID: 7, APIKeyID: 9, AccountID: 12, Status: service.VideoTaskStatusProcessing, CreatedAt: 100}
	require.NoError(t, store.Save(ctx, record, 48*time.Hour))
	got, err := store.Get(ctx, record.ID)
	require.NoError(t, err)
	require.Equal(t, record, got)
	require.Equal(t, 48*time.Hour, mr.TTL(videoTaskKey(record.ID)))
	mr.FastForward(48 * time.Hour)
	_, err = store.Get(ctx, record.ID)
	require.ErrorIs(t, err, service.ErrVideoTaskNotFound)
}
