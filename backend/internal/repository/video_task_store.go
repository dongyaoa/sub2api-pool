package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/redis/go-redis/v9"
)

const (
	videoTaskKeyPrefix = "video_task:"
)

type videoTaskStore struct {
	rdb *redis.Client
	db  *sql.DB
}

func NewVideoTaskStore(rdb *redis.Client) service.VideoTaskStore {
	return &videoTaskStore{rdb: rdb}
}

func ProvideVideoTaskStore(rdb *redis.Client, db *sql.DB) service.VideoTaskStore {
	return &videoTaskStore{rdb: rdb, db: db}
}

func (s *videoTaskStore) Save(ctx context.Context, task *service.VideoTaskRecord, ttl time.Duration) error {
	durableSaved := false
	if s.db != nil {
		if err := s.saveDurable(ctx, task); err != nil {
			return err
		}
		durableSaved = true
	}
	if s.rdb == nil {
		if durableSaved {
			return nil
		}
		return errors.New("video task storage is unavailable")
	}
	data, err := json.Marshal(task)
	if err != nil {
		if durableSaved {
			return nil
		}
		return err
	}
	err = s.rdb.Set(ctx, videoTaskKey(task.ID), data, ttl).Err()
	if durableSaved {
		return nil
	}
	return err
}

func (s *videoTaskStore) Get(ctx context.Context, id string) (*service.VideoTaskRecord, error) {
	if s.rdb == nil {
		if s.db != nil {
			return s.getDurable(ctx, id)
		}
		return nil, errors.New("video task storage is unavailable")
	}
	data, err := s.rdb.Get(ctx, videoTaskKey(id)).Bytes()
	if err != nil {
		if s.db != nil {
			return s.getDurable(ctx, id)
		}
		if err == redis.Nil {
			return nil, service.ErrVideoTaskNotFound
		}
		return nil, err
	}
	var task service.VideoTaskRecord
	if err := json.Unmarshal(data, &task); err != nil {
		return nil, err
	}
	return &task, nil
}

func videoTaskKey(id string) string {
	return videoTaskKeyPrefix + strings.TrimSpace(id)
}
