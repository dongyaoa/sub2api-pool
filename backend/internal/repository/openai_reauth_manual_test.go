package repository

import (
	"context"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestOpenAIReauthManualCompleteRollsBackWhenConfigChanges(t *testing.T) {
	repo, mock := reauthMock(t)
	_, account := reauthTestJob()
	account.Proxy = &service.Proxy{ID: *account.ProxyID, Protocol: "http", Host: "proxy.test", Port: 8080}
	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE accounts a SET`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`UPDATE openai_auto_reauth SET`).WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectRollback()
	ok, err := repo.CompleteManual(context.Background(), account, 3, map[string]any{"access_token": "new"})
	require.NoError(t, err)
	require.False(t, ok)
}

func TestOpenAIReauthManualCompleteRollsBackWhenOutboxFails(t *testing.T) {
	repo, mock := reauthMock(t)
	_, account := reauthTestJob()
	account.Proxy = &service.Proxy{ID: *account.ProxyID, Protocol: "http", Host: "proxy.test", Port: 8080}
	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE accounts a SET`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`UPDATE openai_auto_reauth SET`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`INSERT INTO scheduler_outbox`).WillReturnError(errors.New("unavailable"))
	mock.ExpectRollback()
	ok, err := repo.CompleteManual(context.Background(), account, 3, map[string]any{"access_token": "new"})
	require.Error(t, err)
	require.False(t, ok)
}
