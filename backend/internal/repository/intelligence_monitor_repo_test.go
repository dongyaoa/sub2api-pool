package repository

import (
	"context"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestIntelligenceRepositoryArchiveDisablesDedicatedKeyTransactionally(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	repo := &intelligenceMonitorRepository{db: db}
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT local_api_key_id FROM intelligence_monitor_plans .*FOR UPDATE`).WithArgs(int64(3)).WillReturnRows(sqlmock.NewRows([]string{"local_api_key_id"}).AddRow(22))
	mock.ExpectQuery(`SELECT EXISTS.*intelligence_monitor_runs`).WithArgs(int64(3)).WillReturnRows(sqlmock.NewRows([]string{"busy"}).AddRow(false))
	mock.ExpectExec(`UPDATE api_keys SET status='disabled'`).WithArgs(int64(22)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`UPDATE intelligence_monitor_plans SET deleted_at=.*api_key_encrypted=''`).WithArgs(int64(3)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	require.NoError(t, repo.ArchivePlan(context.Background(), 3))
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestIntelligenceRepositoryArchivePreservesRunningGeneration(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	repo := &intelligenceMonitorRepository{db: db}
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT local_api_key_id FROM intelligence_monitor_plans .*FOR UPDATE`).WithArgs(int64(3)).WillReturnRows(sqlmock.NewRows([]string{"local_api_key_id"}).AddRow(22))
	mock.ExpectQuery(`SELECT EXISTS.*intelligence_monitor_runs`).WithArgs(int64(3)).WillReturnRows(sqlmock.NewRows([]string{"busy"}).AddRow(true))
	mock.ExpectRollback()
	require.ErrorIs(t, repo.ArchivePlan(context.Background(), 3), service.ErrIntelligenceBusy)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestIntelligenceRepositoryClaimEnforcesGlobalTwoWorkerLimit(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	repo := &intelligenceMonitorRepository{db: db}
	mock.ExpectBegin()
	mock.ExpectExec(`SELECT pg_advisory_xact_lock\(245,1\)`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(`SELECT COUNT\(\*\) FROM intelligence_monitor_runs WHERE status='running'`).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(2))
	mock.ExpectRollback()
	run, err := repo.ClaimNext(context.Background(), "worker-token")
	require.NoError(t, err)
	require.Nil(t, run)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestIntelligenceRepositoryCompletionLocksPlanFirstAndClearsCredential(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	repo := &intelligenceMonitorRepository{db: db}
	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT id FROM intelligence_monitor_plans WHERE id=\$1 FOR UPDATE`).WithArgs(int64(3)).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(3))
	mock.ExpectQuery(`UPDATE intelligence_monitor_runs SET status=.*request_key_encrypted=''`).WithArgs(int64(11), "worker-token", "failed", nil, "cancelled", "", "", "null", "{}").WillReturnRows(sqlmock.NewRows([]string{"plan_id"}).AddRow(3))
	mock.ExpectExec(`UPDATE intelligence_monitor_plans SET last_run_at=`).WithArgs(int64(3)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`DELETE FROM intelligence_monitor_runs .*status IN \('succeeded','failed'\).*OFFSET \$2`).WithArgs(int64(3), 20).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	run := &service.IntelligenceMonitorRun{ID: 11, PlanID: 3, LeaseToken: "worker-token", Status: "failed", Error: "cancelled", SourceSnapshot: map[string]any{}}
	require.NoError(t, repo.CompleteRun(context.Background(), run))
	require.NoError(t, mock.ExpectationsWereMet())
}
