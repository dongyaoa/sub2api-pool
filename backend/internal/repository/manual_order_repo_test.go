package repository

import (
	"context"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

func TestManualOrderRepositoryRollsBackFailedBulkWrite(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	repo := &upstreamCenterRepository{db: db}
	mock.ExpectBegin()
	mock.ExpectExec(`SELECT pg_advisory_xact_lock_shared\(251,0\)`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`SELECT pg_advisory_xact_lock\(2511,hashtext\(\$1\)\)`).WithArgs("suppliers").WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(`SELECT id FROM upstream_suppliers WHERE deleted_at IS NULL ORDER BY id FOR UPDATE`).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(1).AddRow(2))
	wantErr := errors.New("write failed")
	mock.ExpectExec(`UPDATE upstream_suppliers AS item SET sort_order=ordered.position FROM unnest`).WithArgs(pq.Array([]int64{2, 1})).WillReturnError(wantErr)
	mock.ExpectRollback()
	require.ErrorIs(t, repo.SaveOrder(context.Background(), service.UpstreamOrderInput{Scope: "suppliers", IDs: []int64{2, 1}}), wantErr)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestManualOrderRepositoryRejectsInvalidInputBeforeTransaction(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	require.ErrorIs(t, (&upstreamCenterRepository{db: db}).SaveOrder(context.Background(), service.UpstreamOrderInput{Scope: "suppliers", IDs: []int64{1, 1}}), service.ErrManualOrderInvalid)
	require.ErrorIs(t, (&intelligenceMonitorRepository{db: db}).SaveOrder(context.Background(), service.IntelligenceOrderInput{Scope: "oauth", IDs: []int64{0}}), service.ErrManualOrderInvalid)
	require.NoError(t, mock.ExpectationsWereMet())
}
