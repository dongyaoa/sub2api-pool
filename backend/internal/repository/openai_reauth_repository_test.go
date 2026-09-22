package repository

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func reauthMock(t *testing.T) (service.OpenAIReauthRepository, sqlmock.Sqlmock) {
	t.Helper()
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, mock.ExpectationsWereMet()); db.Close() })
	return NewOpenAIReauthRepository(db), mock
}

func reauthTestJob() (*service.OpenAIReauthJob, *service.Account) {
	proxyID := int64(12)
	return &service.OpenAIReauthJob{OpenAIReauthConfig: service.OpenAIReauthConfig{
		OpenAIReauthStatus: service.OpenAIReauthStatus{AccountID: 7, Generation: 3, Attempts: 1},
	}, LeaseID: "worker-a"}, &service.Account{ID: 7, ProxyID: &proxyID,
		Credentials: map[string]any{"access_token": "old", "model_mapping": map[string]any{"a": "b"}}}
}

func TestOpenAIReauthCompleteRollsBackWhenJobLost(t *testing.T) {
	repo, mock := reauthMock(t)
	job, account := reauthTestJob()
	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE accounts a SET`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`UPDATE openai_auto_reauth SET status = 'succeeded'`).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectRollback()
	applied, err := repo.Complete(context.Background(), job, account, map[string]any{"access_token": "new"})
	require.NoError(t, err)
	require.False(t, applied, "a replaced lease must never leave updated credentials committed")
}

func TestOpenAIReauthCompleteStaleAccountDoesNotCompleteJob(t *testing.T) {
	repo, mock := reauthMock(t)
	job, account := reauthTestJob()
	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE accounts a SET`).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectRollback()
	applied, err := repo.Complete(context.Background(), job, account, map[string]any{"access_token": "new"})
	require.NoError(t, err)
	require.False(t, applied)
}

func TestOpenAIReauthCompleteRollsBackWhenOutboxFails(t *testing.T) {
	repo, mock := reauthMock(t)
	job, account := reauthTestJob()
	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE accounts a SET`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`UPDATE openai_auto_reauth SET status = 'succeeded'`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`INSERT INTO scheduler_outbox`).WillReturnError(errors.New("outbox unavailable"))
	mock.ExpectRollback()
	applied, err := repo.Complete(context.Background(), job, account, map[string]any{"access_token": "new"})
	require.EqualError(t, err, "outbox unavailable")
	require.False(t, applied)
}

func TestOpenAIReauthCompleteRejectsSettingsInTokenPatch(t *testing.T) {
	repo, _ := reauthMock(t)
	job, account := reauthTestJob()
	for _, key := range []string{"password", "totp_secret", "proxy_id", "model_mapping", "custom_headers"} {
		t.Run(key, func(t *testing.T) {
			applied, err := repo.Complete(context.Background(), job, account, map[string]any{"access_token": "new", key: "changed"})
			require.Error(t, err)
			require.False(t, applied)
		})
	}
}

func TestOpenAIReauthFailSanitizesErrorsAndBoundsRetries(t *testing.T) {
	repo, mock := reauthMock(t)
	job, _ := reauthTestJob()
	job.Attempts = service.OpenAIReauthMaxAttempts
	mock.ExpectExec(`UPDATE openai_auto_reauth SET status`).
		WithArgs(int64(7), int64(3), "worker-a", "failed", "authorization_failed", float64(60)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	applied, err := repo.Fail(context.Background(), job, "HTTP response containing secret", time.Minute, false)
	require.NoError(t, err)
	require.True(t, applied)
}

func TestOpenAIReauthClaimRecoversExpiredLeaseAndReturnsNoWork(t *testing.T) {
	repo, mock := reauthMock(t)
	mock.ExpectExec(`UPDATE openai_auto_reauth SET status = 'failed'`).
		WithArgs(service.OpenAIReauthMaxAttempts).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(`WITH candidate AS`).WithArgs("worker-b", float64(120), service.OpenAIReauthMaxAttempts).
		WillReturnError(sql.ErrNoRows)
	job, err := repo.Claim(context.Background(), "worker-b", 2*time.Minute)
	require.NoError(t, err)
	require.Nil(t, job)
}

func TestOpenAIReauthSaveRejectsRotatingPool(t *testing.T) {
	repo, mock := reauthMock(t)
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT a.platform, a.type`).WithArgs(int64(7)).WillReturnRows(
		sqlmock.NewRows([]string{"platform", "type", "parent_account_id", "proxy_id", "extra", "status", "deleted_at", "expires_at", "encrypted_secret", "credentials_hash", "proxy_hash"}).
			AddRow("openai", "oauth", nil, int64(12), `{"proxy_pool":[{"proxy_id":12,"concurrency":1},{"proxy_id":13,"concurrency":1}]}`, "active", nil, nil, "encrypted", "credential-hash", "proxy-hash"))
	mock.ExpectRollback()
	require.ErrorIs(t, repo.Save(context.Background(), 7, "", true), service.ErrOpenAIReauthInvalidAccount)
}

func TestOpenAIReauthSavePreservesCipherOnToggle(t *testing.T) {
	repo, mock := reauthMock(t)
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT a.platform, a.type`).WithArgs(int64(7)).WillReturnRows(
		sqlmock.NewRows([]string{"platform", "type", "parent_account_id", "proxy_id", "extra", "status", "deleted_at", "expires_at", "encrypted_secret", "credentials_hash", "proxy_hash"}).
			AddRow("openai", "oauth", nil, int64(12), `{}`, "active", nil, nil, "ciphertext-only", "credential-hash", "proxy-hash"))
	mock.ExpectExec(`INSERT INTO openai_auto_reauth`).WithArgs(int64(7), "ciphertext-only", false, "credential-hash", "proxy-hash").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`UPDATE accounts SET`).WithArgs(int64(7), false).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`INSERT INTO scheduler_outbox`).WithArgs(service.SchedulerOutboxEventAccountChanged, int64(7)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	require.NoError(t, repo.Save(context.Background(), 7, "", false))
}

func TestOpenAIReauthManagedMarkersCannotBeForgedOrCleared(t *testing.T) {
	current := map[string]any{service.OpenAIReauthEnabledKey: true, service.OpenAIReauthPendingKey: true}
	request := map[string]any{service.OpenAIReauthEnabledKey: false, service.OpenAIReauthPendingKey: nil, "ordinary": "new"}
	merged := preserveOpenAIReauthExtra(request, current)
	require.Equal(t, true, merged[service.OpenAIReauthEnabledKey])
	require.Equal(t, true, merged[service.OpenAIReauthPendingKey])
	require.Equal(t, "new", merged["ordinary"])
	require.Equal(t, false, request[service.OpenAIReauthEnabledKey], "never mutate caller maps")
	forged := preserveOpenAIReauthExtra(request, nil)
	require.NotContains(t, forged, service.OpenAIReauthEnabledKey)
	require.NotContains(t, forged, service.OpenAIReauthPendingKey)
	require.Equal(t, map[string]any{"ordinary": "new"}, stripOpenAIReauthExtraUpdate(request))
}

func TestOpenAIReauthSchedulerProjectionRetainsManagedMarkers(t *testing.T) {
	extra := filterSchedulerExtra(map[string]any{service.OpenAIReauthEnabledKey: true, service.OpenAIReauthPendingKey: true})
	require.Equal(t, true, extra[service.OpenAIReauthEnabledKey])
	require.Equal(t, true, extra[service.OpenAIReauthPendingKey])
}

func TestOpenAIReauthSchedulerProjectionPreservesProxyHealthWithoutSecrets(t *testing.T) {
	proxyID := int64(7)
	account := service.Account{Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth,
		Status: service.StatusActive, Schedulable: true, ProxyID: &proxyID,
		Proxy: &service.Proxy{ID: proxyID, Protocol: "http", Status: service.StatusActive, Username: "private", Password: "private"},
		Extra: map[string]any{service.OpenAIReauthEnabledKey: true}}
	metadata := buildSchedulerMetadataAccount(account)
	require.True(t, metadata.IsSchedulable(), "healthy strict account must remain a scheduler candidate")
	require.Empty(t, metadata.Proxy.Username)
	require.Empty(t, metadata.Proxy.Password)
	account.Proxy.Status = service.StatusExpired
	metadata = buildSchedulerMetadataAccount(account)
	require.False(t, metadata.IsSchedulable())
}
