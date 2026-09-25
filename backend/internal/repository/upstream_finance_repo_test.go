package repository

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestUpstreamFinanceLoadNewAPICredentialsWithoutSerializing(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	mock.ExpectQuery(`SELECT t.id,t.supplier_id,t.provider,t.endpoint,t.api_key_encrypted,t.wallet_ref,t.newapi_user_id,t.newapi_access_token_encrypted`).
		WithArgs(int64(7)).WillReturnRows(sqlmock.NewRows([]string{"id", "supplier_id", "provider", "endpoint", "api_key_encrypted", "wallet_ref", "newapi_user_id", "newapi_access_token_encrypted"}).
		AddRow(7, nil, "openai", "https://example.com", "inference-cipher", "default", 42, "console-cipher"))
	target, err := (&upstreamFinanceRepository{db: db}).GetTarget(context.Background(), 7)
	require.NoError(t, err)
	require.Equal(t, int64(42), target.NewAPIUserID)
	require.Equal(t, "console-cipher", target.NewAPIAccessTokenEncrypted)
	encoded, err := json.Marshal(target)
	require.NoError(t, err)
	require.JSONEq(t, `{}`, string(encoded))
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUpstreamFinanceSnapshotRejectsChangedNewAPICredentials(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	target := &service.UpstreamFinanceTarget{ID: 7, Provider: "openai", Endpoint: "https://example.com", APIKeyEncrypted: "inference-cipher", WalletRef: "default", NewAPIUserID: 42, NewAPIAccessTokenEncrypted: "console-cipher"}
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT id FROM upstream_targets.*newapi_user_id=\$8 AND newapi_access_token_encrypted=\$9 FOR UPDATE`).
		WithArgs(target.ID, target.SupplierID, target.Provider, target.Endpoint, target.APIKeyEncrypted, target.WalletRef, "lease", target.NewAPIUserID, target.NewAPIAccessTokenEncrypted).
		WillReturnRows(sqlmock.NewRows([]string{"id"}))
	mock.ExpectRollback()
	err = (&upstreamFinanceRepository{db: db}).SaveBalance(context.Background(), target, "identity", &service.UpstreamBalanceSnapshot{}, "lease", time.Now())
	require.ErrorIs(t, err, service.ErrUpstreamFinanceIdentityChanged)
	require.NoError(t, mock.ExpectationsWereMet())
}
