package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

type videoTaskScanner interface {
	Scan(dest ...any) error
}

func (s *videoTaskStore) saveDurable(ctx context.Context, task *service.VideoTaskRecord) error {
	if task == nil {
		return errors.New("video task is nil")
	}
	billingStatus := strings.TrimSpace(task.BillingStatus)
	if billingStatus == "" {
		billingStatus = service.VideoTaskBillingPending
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO grok_video_generation_tasks (
			request_id, user_id, api_key_id, group_id, account_id, subscription_id,
			operation, model, upstream_model, prompt, resolution, aspect_ratio,
			duration_seconds, request_payload_hash, status, http_status, task_error,
			last_upstream_error, last_checked_at, video_url, content_type, byte_size,
			browser_playable, playback_format_version, delivery_error, delivered_at,
			billing_status, billing_error, billed_at, completed_at, expires_at,
			created_at, updated_at
		) VALUES (
			$1, $2, $3, $4, $5, $6,
			$7, $8, $9, $10, $11, $12,
			$13, $14, $15, $16, NULLIF($17, '')::jsonb,
			$18, $19, $20, $21, $22,
			$23, $24, $25, $26,
			$27, $28, $29, $30, $31,
			$32, NOW()
		)
		ON CONFLICT (request_id) DO UPDATE SET
			user_id = EXCLUDED.user_id,
			api_key_id = EXCLUDED.api_key_id,
			group_id = EXCLUDED.group_id,
			account_id = EXCLUDED.account_id,
			subscription_id = EXCLUDED.subscription_id,
			operation = EXCLUDED.operation,
			model = EXCLUDED.model,
			upstream_model = EXCLUDED.upstream_model,
			prompt = EXCLUDED.prompt,
			resolution = EXCLUDED.resolution,
			aspect_ratio = EXCLUDED.aspect_ratio,
			duration_seconds = EXCLUDED.duration_seconds,
			request_payload_hash = EXCLUDED.request_payload_hash,
			status = EXCLUDED.status,
			http_status = EXCLUDED.http_status,
			task_error = EXCLUDED.task_error,
			last_upstream_error = EXCLUDED.last_upstream_error,
			last_checked_at = EXCLUDED.last_checked_at,
			video_url = EXCLUDED.video_url,
			content_type = EXCLUDED.content_type,
			byte_size = EXCLUDED.byte_size,
			browser_playable = EXCLUDED.browser_playable,
			playback_format_version = EXCLUDED.playback_format_version,
			delivery_error = EXCLUDED.delivery_error,
			delivered_at = EXCLUDED.delivered_at,
			billing_status = EXCLUDED.billing_status,
			billing_error = EXCLUDED.billing_error,
			billed_at = EXCLUDED.billed_at,
			completed_at = EXCLUDED.completed_at,
			expires_at = EXCLUDED.expires_at,
			updated_at = NOW()
	`,
		task.ID, task.UserID, task.APIKeyID, nullablePositiveInt64(task.GroupID), task.AccountID, nullablePositiveInt64(task.SubscriptionID),
		task.Metadata.Operation, task.Metadata.Model, nullableString(task.UpstreamModel), task.Metadata.Prompt,
		nullableString(task.Metadata.Resolution), nullableString(task.Metadata.AspectRatio), task.Metadata.Duration,
		nullableString(task.RequestPayloadHash), task.Status, nullablePositiveInt(task.HTTPStatus), string(task.Error),
		nullableString(task.LastUpstreamError), nullableUnixPtr(task.LastCheckedAt), nullableString(task.VideoURL), nullableString(task.ContentType), task.ByteSize,
		task.BrowserPlayable, task.PlaybackFormatVersion, nullableString(task.DeliveryError), nullableUnixPtr(task.DeliveredAt),
		billingStatus, nullableString(task.BillingError), nullableUnixPtr(task.BilledAt), nullableUnixPtr(task.CompletedAt), nullableUnix(task.ExpiresAt),
		time.Unix(task.CreatedAt, 0).UTC(),
	)
	return err
}

func (s *videoTaskStore) getDurable(ctx context.Context, id string) (*service.VideoTaskRecord, error) {
	row := s.db.QueryRowContext(ctx, durableVideoTaskSelect+` WHERE request_id = $1`, strings.TrimSpace(id))
	record, err := scanDurableVideoTask(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, service.ErrVideoTaskNotFound
	}
	return record, err
}

const durableVideoTaskSelect = `
	SELECT
		request_id, user_id, api_key_id, COALESCE(group_id, 0), account_id,
		COALESCE(subscription_id, 0), COALESCE(upstream_model, ''), COALESCE(request_payload_hash, ''),
		status, COALESCE(http_status, 0), task_error, COALESCE(last_upstream_error, ''),
		CASE WHEN last_checked_at IS NULL THEN NULL ELSE EXTRACT(EPOCH FROM last_checked_at)::bigint END,
		EXTRACT(EPOCH FROM created_at)::bigint,
		CASE WHEN completed_at IS NULL THEN NULL ELSE EXTRACT(EPOCH FROM completed_at)::bigint END,
		CASE WHEN expires_at IS NULL THEN 0 ELSE EXTRACT(EPOCH FROM expires_at)::bigint END,
		COALESCE(operation, 'text'), model, prompt, COALESCE(resolution, ''), COALESCE(aspect_ratio, ''), duration_seconds,
		COALESCE(video_url, ''), COALESCE(content_type, ''), byte_size, browser_playable,
		playback_format_version, COALESCE(delivery_error, ''),
		CASE WHEN delivered_at IS NULL THEN NULL ELSE EXTRACT(EPOCH FROM delivered_at)::bigint END,
		billing_status, COALESCE(billing_error, ''),
		CASE WHEN billed_at IS NULL THEN NULL ELSE EXTRACT(EPOCH FROM billed_at)::bigint END
	FROM grok_video_generation_tasks`

func scanDurableVideoTask(scanner videoTaskScanner) (*service.VideoTaskRecord, error) {
	var record service.VideoTaskRecord
	var taskError []byte
	var lastCheckedAt, completedAt, deliveredAt, billedAt sql.NullInt64
	err := scanner.Scan(
		&record.ID, &record.UserID, &record.APIKeyID, &record.GroupID, &record.AccountID,
		&record.SubscriptionID, &record.UpstreamModel, &record.RequestPayloadHash,
		&record.Status, &record.HTTPStatus, &taskError, &record.LastUpstreamError,
		&lastCheckedAt, &record.CreatedAt, &completedAt, &record.ExpiresAt,
		&record.Metadata.Operation, &record.Metadata.Model, &record.Metadata.Prompt,
		&record.Metadata.Resolution, &record.Metadata.AspectRatio, &record.Metadata.Duration,
		&record.VideoURL, &record.ContentType, &record.ByteSize, &record.BrowserPlayable,
		&record.PlaybackFormatVersion, &record.DeliveryError, &deliveredAt,
		&record.BillingStatus, &record.BillingError, &billedAt,
	)
	if err != nil {
		return nil, err
	}
	if len(taskError) > 0 {
		record.Error = append(json.RawMessage(nil), taskError...)
	}
	record.LastCheckedAt = nullUnixPtr(lastCheckedAt)
	record.CompletedAt = nullUnixPtr(completedAt)
	record.DeliveredAt = nullUnixPtr(deliveredAt)
	record.BilledAt = nullUnixPtr(billedAt)
	return &record, nil
}

func nullableString(value string) any {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return value
}

func nullablePositiveInt64(value int64) any {
	if value <= 0 {
		return nil
	}
	return value
}

func nullablePositiveInt(value int) any {
	if value <= 0 {
		return nil
	}
	return value
}

func nullableUnix(value int64) any {
	if value <= 0 {
		return nil
	}
	return time.Unix(value, 0).UTC()
}

func nullableUnixPtr(value *int64) any {
	if value == nil {
		return nil
	}
	return nullableUnix(*value)
}

func nullUnixPtr(value sql.NullInt64) *int64 {
	if !value.Valid {
		return nil
	}
	result := value.Int64
	return &result
}
