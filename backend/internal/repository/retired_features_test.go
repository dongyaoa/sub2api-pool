package repository

import (
	"context"
	"errors"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/require"
)

const retiredBatchImageActivitySQL = `(?s)SELECT\s+\(SELECT COUNT\(\*\) FROM users WHERE COALESCE\(frozen_balance, 0\) > 0\),\s+\(SELECT COUNT\(\*\) FROM batch_image_jobs\s+WHERE status NOT IN \('completed', 'failed', 'cancelled', 'output_deleted'\)\)`

func TestEnsureRetiredBatchImageJobsDrained(t *testing.T) {
	for _, tt := range []struct {
		name        string
		frozenUsers int64
		pendingJobs int64
		wantError   bool
	}{
		{name: "no outstanding activity"},
		{name: "terminal jobs with frozen balances", frozenUsers: 1, wantError: true},
		{name: "pending jobs without frozen balances", pendingJobs: 2, wantError: true},
		{name: "both frozen balances and pending jobs", frozenUsers: 1, pendingJobs: 2, wantError: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			defer func() { _ = db.Close() }()
			mock.ExpectQuery(retiredBatchImageActivitySQL).
				WillReturnRows(sqlmock.NewRows([]string{"frozen_users", "pending_jobs"}).AddRow(tt.frozenUsers, tt.pendingJobs))

			err = ensureRetiredBatchImageJobsDrained(context.Background(), db)
			if tt.wantError {
				require.ErrorContains(t, err, "cannot start with batch image generation removed")
				require.ErrorContains(t, err, "use the previous version")
				require.ErrorContains(t, err, "complete or cancel")
			} else {
				require.NoError(t, err)
			}
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestEnsureRetiredBatchImageJobsDrainedQueryFailure(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	queryErr := errors.New("database query failed")
	mock.ExpectQuery(retiredBatchImageActivitySQL).WillReturnError(queryErr)

	err = ensureRetiredBatchImageJobsDrained(context.Background(), db)
	require.ErrorIs(t, err, queryErr)
	require.ErrorContains(t, err, "check retired batch image activity")
	require.NoError(t, mock.ExpectationsWereMet())
}
