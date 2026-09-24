package service

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type memoryVideoTaskStore struct {
	tasks map[string]*VideoTaskRecord
}

func newMemoryVideoTaskStore() *memoryVideoTaskStore {
	return &memoryVideoTaskStore{tasks: make(map[string]*VideoTaskRecord)}
}

func (s *memoryVideoTaskStore) Save(_ context.Context, task *VideoTaskRecord, _ time.Duration) error {
	data, _ := json.Marshal(task)
	var copied VideoTaskRecord
	_ = json.Unmarshal(data, &copied)
	s.tasks[task.ID] = &copied
	return nil
}

func (s *memoryVideoTaskStore) Get(_ context.Context, id string) (*VideoTaskRecord, error) {
	task, ok := s.tasks[id]
	if !ok {
		return nil, ErrVideoTaskNotFound
	}
	data, _ := json.Marshal(task)
	var copied VideoTaskRecord
	_ = json.Unmarshal(data, &copied)
	return &copied, nil
}

type recordingVideoStorage struct {
	key         string
	contentType string
	data        []byte
}

func (s *recordingVideoStorage) Save(_ context.Context, key, contentType string, data []byte) (string, error) {
	s.key = key
	s.contentType = contentType
	s.data = append([]byte(nil), data...)
	return "https://cdn.example/" + key, nil
}

func TestVideoTaskServicePersistsOwnershipAndStatus(t *testing.T) {
	store := newMemoryVideoTaskStore()
	svc := NewVideoTaskService(store, nil)
	svc.ttl = 48 * time.Hour
	owner := VideoTaskOwner{UserID: 7, APIKeyID: 9}
	metadata := VideoTaskMetadata{Operation: "text", Model: "grok-imagine-video", Prompt: "move", Resolution: "720p", AspectRatio: "3:2", Duration: 8}

	err := svc.RecordSubmission(context.Background(), owner, 3, 12, "task-1", metadata, []byte(`{"id":"task-1","status":"queued"}`))
	require.NoError(t, err)

	record, err := svc.GetRecord(context.Background(), owner, "task-1")
	require.NoError(t, err)
	require.Equal(t, int64(12), record.AccountID)
	require.Equal(t, VideoTaskStatusProcessing, record.Status)
	_, err = svc.GetRecord(context.Background(), VideoTaskOwner{UserID: 7, APIKeyID: 10}, "task-1")
	require.ErrorIs(t, err, ErrVideoTaskNotFound)

	err = svc.UpdateStatus(context.Background(), owner, "task-1", 200, []byte(`{"status":"completed"}`))
	require.NoError(t, err)
	record, err = svc.GetRecord(context.Background(), owner, "task-1")
	require.NoError(t, err)
	require.Equal(t, VideoTaskStatusCompleted, record.Status)
	require.Equal(t, "3:2", record.Metadata.AspectRatio)
	require.WithinDuration(t, time.Now().Add(48*time.Hour), time.Unix(record.ExpiresAt, 0), time.Second)
}

func TestVideoTaskServiceStoresMP4UsingExistingObjectStoragePrefix(t *testing.T) {
	store := newMemoryVideoTaskStore()
	storage := &recordingVideoStorage{}
	uploader := NewImageResultUploader(storage, "images/")
	svc := NewVideoTaskService(store, func() (*ImageResultUploader, bool) { return uploader, true })
	owner := VideoTaskOwner{UserID: 1, APIKeyID: 2}
	require.NoError(t, svc.RecordSubmission(
		context.Background(), owner, 3, 4, "task-video", VideoTaskMetadata{Model: "grok-imagine-video"}, nil,
	))
	mp4 := []byte("\x00\x00\x00\x18ftypisomavc1mp4avideo")

	err := svc.StoreContent(context.Background(), owner, "task-video", "application/octet-stream", mp4)
	require.NoError(t, err)
	require.Equal(t, "images/videos/task-video.mp4", storage.key)
	require.Equal(t, "video/mp4", storage.contentType)
	require.Equal(t, mp4, storage.data)

	record, err := svc.GetRecord(context.Background(), owner, "task-video")
	require.NoError(t, err)
	require.Equal(t, "https://cdn.example/images/videos/task-video.mp4", record.VideoURL)
	require.Equal(t, VideoTaskStatusCompleted, record.Status)
	require.Equal(t, int64(len(mp4)), record.ByteSize)
	require.True(t, record.BrowserPlayable)
	require.Equal(t, VideoPlaybackFormatVersion, record.PlaybackFormatVersion)
	require.False(t, record.NeedsBrowserPlaybackUpgrade())
}

func TestPrepareBrowserPlayableVideoCanonicalizesTaggedH264MP4(t *testing.T) {
	input := []byte("\x00\x00\x00\x18ftypisomavc1mp4avideo")
	converted := []byte("\x00\x00\x00\x18ftypisomavc1canonical")
	transcodeCalled := false

	prepared, contentType, err := prepareBrowserPlayableVideo(
		context.Background(), "application/octet-stream", input,
		func(context.Context, string, []byte) ([]byte, error) {
			transcodeCalled = true
			return converted, nil
		},
	)

	require.NoError(t, err)
	require.True(t, transcodeCalled)
	require.Equal(t, "video/mp4", contentType)
	require.Equal(t, converted, prepared)
}

func TestVideoTaskRecordRequiresUpgradeForLegacyPlayableFlag(t *testing.T) {
	legacy := &VideoTaskRecord{BrowserPlayable: true, PlaybackFormatVersion: 1}
	require.True(t, legacy.NeedsBrowserPlaybackUpgrade())

	current := &VideoTaskRecord{
		BrowserPlayable:       true,
		PlaybackFormatVersion: VideoPlaybackFormatVersion,
	}
	require.False(t, current.NeedsBrowserPlaybackUpgrade())
}

func TestPrepareBrowserPlayableVideoTranscodesHEVCToH264(t *testing.T) {
	input := []byte("\x00\x00\x00\x18ftypisomhvc1video")
	converted := []byte("\x00\x00\x00\x18ftypisomavc1mp4avideo")
	var receivedType string
	var receivedData []byte

	prepared, contentType, err := prepareBrowserPlayableVideo(
		context.Background(), "video/mp4", input,
		func(_ context.Context, inputType string, data []byte) ([]byte, error) {
			receivedType = inputType
			receivedData = append([]byte(nil), data...)
			return converted, nil
		},
	)

	require.NoError(t, err)
	require.Equal(t, "video/mp4", receivedType)
	require.Equal(t, input, receivedData)
	require.Equal(t, "video/mp4", contentType)
	require.Equal(t, converted, prepared)
}
