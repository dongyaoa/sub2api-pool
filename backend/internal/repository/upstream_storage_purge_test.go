package repository

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

func expectStoragePurgeLocks(mock sqlmock.Sqlmock) {
	mock.ExpectBegin()
	mock.ExpectExec(`SELECT pg_advisory_xact_lock\(251,0\)`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectExec(`SELECT pg_advisory_xact_lock\(254,0\)`).WillReturnResult(sqlmock.NewResult(0, 1))
}

func TestUpstreamStorageArchivesListIncludesScopesAndTotal(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	now := time.Now()
	rows := sqlmock.NewRows([]string{"kind", "id", "name", "deleted_at", "source_type", "supplier_name", "total"}).
		AddRow("supplier", 1, "Archive supplier", now, "", "", 203).
		AddRow("target", 2, "Archive group", now, "upstream", "Archive supplier", 203).
		AddRow("intelligence", 3, "Archive OAuth", now, "openai_oauth", "", 203)
	mock.ExpectQuery(`COUNT\(\*\) OVER\(\).*s.deleted_at IS NOT NULL.*UNION ALL.*t.deleted_at IS NOT NULL OR s.deleted_at IS NOT NULL.*UNION ALL.*p.deleted_at IS NOT NULL.*LIMIT 200`).WillReturnRows(rows)
	page, err := (&upstreamCenterRepository{db: db}).ListStorageArchives(context.Background())
	require.NoError(t, err)
	require.Equal(t, int64(203), page.Total)
	require.Len(t, page.Items, 3)
	require.Equal(t, "openai_oauth", page.Items[2].SourceType)
	require.Equal(t, "Archive supplier", page.Items[1].SupplierName)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUpstreamStoragePurgeRejectsIncorrectConfirmationAndUnknownItems(t *testing.T) {
	for _, tc := range []struct {
		name, kind string
		result     *sqlmock.Rows
		want       error
	}{
		{"supplier changed name", "supplier", sqlmock.NewRows([]string{"name"}).AddRow("new name"), service.ErrUpstreamStorageConfirm},
		{"supplier missing", "supplier", sqlmock.NewRows([]string{"name"}), service.ErrUpstreamStorageNotFound},
		{"target changed name", "target", sqlmock.NewRows([]string{"id", "name", "busy"}).AddRow(7, "new name", false), service.ErrUpstreamStorageConfirm},
		{"target missing", "target", sqlmock.NewRows([]string{"id", "name", "busy"}), service.ErrUpstreamStorageNotFound},
		{"plan changed name", "intelligence", sqlmock.NewRows([]string{"name", "key_id", "owner_id", "source_type", "account_id"}).AddRow("new name", nil, nil, "external", nil), service.ErrUpstreamStorageConfirm},
		{"plan missing", "intelligence", sqlmock.NewRows([]string{"name", "key_id", "owner_id", "source_type", "account_id"}), service.ErrUpstreamStorageNotFound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			defer func() { _ = db.Close() }()
			expectStoragePurgeLocks(mock)
			query := `SELECT name FROM upstream_suppliers WHERE id=\$1 FOR UPDATE`
			if tc.kind == "target" {
				query = `SELECT id,name,COALESCE.*FROM upstream_targets WHERE id=\$1 ORDER BY id FOR UPDATE`
			}
			if tc.kind == "intelligence" {
				query = `SELECT name,local_api_key_id,local_key_owner_id,source_type,account_id FROM intelligence_monitor_plans WHERE id=\$1 FOR UPDATE`
			}
			mock.ExpectQuery(query).WithArgs(int64(7)).WillReturnRows(tc.result)
			mock.ExpectRollback()
			_, err = (&upstreamCenterRepository{db: db}).PurgeStorage(context.Background(), service.UpstreamStoragePurgeInput{Kind: tc.kind, ID: 7, ConfirmName: "old name"})
			require.ErrorIs(t, err, tc.want)
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestUpstreamStoragePurgeRejectsLeasesAndDependentRuns(t *testing.T) {
	for _, tc := range []struct {
		name         string
		leased, runs bool
	}{
		{"monitor or balance lease", true, false},
		{"dependent paid generation", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			defer func() { _ = db.Close() }()
			expectStoragePurgeLocks(mock)
			mock.ExpectQuery(`SELECT id,name,COALESCE\(lease_until>NOW\(\),FALSE\) OR COALESCE\(balance_lease_until>NOW\(\),FALSE\).*FOR UPDATE`).WithArgs(int64(7)).WillReturnRows(sqlmock.NewRows([]string{"id", "name", "busy"}).AddRow(7, "target", tc.leased))
			if tc.runs {
				mock.ExpectQuery(`SELECT id FROM intelligence_monitor_plans WHERE upstream_target_id=ANY\(\$1\).*FOR UPDATE`).WithArgs(pq.Array([]int64{7})).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(8))
				mock.ExpectQuery(`SELECT EXISTS.*status IN \('pending','running'\)`).WithArgs(pq.Array([]int64{8})).WillReturnRows(sqlmock.NewRows([]string{"busy"}).AddRow(true))
			}
			mock.ExpectRollback()
			_, err = (&upstreamCenterRepository{db: db}).PurgeStorage(context.Background(), service.UpstreamStoragePurgeInput{Kind: "target", ID: 7, ConfirmName: "target"})
			require.ErrorIs(t, err, service.ErrUpstreamStorageBusy)
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}

func TestUpstreamStoragePurgeIntelligenceDisablesOnlyDedicatedOwnedKey(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	expectStoragePurgeLocks(mock)
	mock.ExpectQuery(`SELECT name,local_api_key_id,local_key_owner_id,source_type,account_id FROM intelligence_monitor_plans.*FOR UPDATE`).WithArgs(int64(7)).WillReturnRows(sqlmock.NewRows([]string{"name", "key_id", "owner_id", "source_type", "account_id"}).AddRow("Local plan", 23, 31, "local_group", nil))
	mock.ExpectQuery(`SELECT EXISTS.*status IN \('pending','running'\)`).WithArgs(pq.Array([]int64{7})).WillReturnRows(sqlmock.NewRows([]string{"busy"}).AddRow(false))
	mock.ExpectQuery(`UPDATE api_keys SET status='disabled'.*id=\$1 AND user_id=\$2 AND deleted_at IS NULL RETURNING key`).WithArgs(int64(23), int64(31)).WillReturnRows(sqlmock.NewRows([]string{"key"}).AddRow("dedicated-monitor-key"))
	mock.ExpectExec(`DELETE FROM intelligence_monitor_runs WHERE plan_id=\$1`).WithArgs(int64(7)).WillReturnResult(sqlmock.NewResult(0, 20))
	mock.ExpectExec(`DELETE FROM intelligence_monitor_plans WHERE id=\$1`).WithArgs(int64(7)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	keys, err := (&upstreamCenterRepository{db: db}).PurgeStorage(context.Background(), service.UpstreamStoragePurgeInput{Kind: "intelligence", ID: 7, ConfirmName: "Local plan"})
	require.NoError(t, err)
	require.Equal(t, []string{"dedicated-monitor-key"}, keys)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUpstreamStoragePurgeOAuthDoesNotTouchActualAccount(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	expectStoragePurgeLocks(mock)
	mock.ExpectQuery(`SELECT name,local_api_key_id,local_key_owner_id.*FOR UPDATE`).WithArgs(int64(7)).WillReturnRows(sqlmock.NewRows([]string{"name", "key_id", "owner_id", "source_type", "account_id"}).AddRow("Old OAuth account", nil, nil, "openai_oauth", 29))
	mock.ExpectQuery(`SELECT name FROM accounts WHERE id=\$1 AND deleted_at IS NULL FOR SHARE NOWAIT`).WithArgs(int64(29)).WillReturnRows(sqlmock.NewRows([]string{"name"}).AddRow("OAuth account"))
	mock.ExpectQuery(`SELECT EXISTS.*status IN \('pending','running'\)`).WithArgs(pq.Array([]int64{7})).WillReturnRows(sqlmock.NewRows([]string{"busy"}).AddRow(false))
	mock.ExpectExec(`DELETE FROM intelligence_monitor_runs WHERE plan_id=\$1`).WithArgs(int64(7)).WillReturnResult(sqlmock.NewResult(0, 20))
	mock.ExpectExec(`DELETE FROM intelligence_monitor_plans WHERE id=\$1`).WithArgs(int64(7)).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()
	keys, err := (&upstreamCenterRepository{db: db}).PurgeStorage(context.Background(), service.UpstreamStoragePurgeInput{Kind: "intelligence", ID: 7, ConfirmName: "OAuth account"})
	require.NoError(t, err)
	require.Empty(t, keys)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUpstreamStoragePurgePlanRejectsActiveWork(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	expectStoragePurgeLocks(mock)
	mock.ExpectQuery(`SELECT name,local_api_key_id,local_key_owner_id.*FOR UPDATE`).WithArgs(int64(7)).WillReturnRows(sqlmock.NewRows([]string{"name", "key_id", "owner_id", "source_type", "account_id"}).AddRow("Plan", 23, 31, "local_group", nil))
	mock.ExpectQuery(`SELECT EXISTS.*status IN \('pending','running'\)`).WithArgs(pq.Array([]int64{7})).WillReturnRows(sqlmock.NewRows([]string{"busy"}).AddRow(true))
	mock.ExpectRollback()
	_, err = (&upstreamCenterRepository{db: db}).PurgeStorage(context.Background(), service.UpstreamStoragePurgeInput{Kind: "intelligence", ID: 7, ConfirmName: "Plan"})
	require.ErrorIs(t, err, service.ErrUpstreamStorageBusy)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUpstreamStoragePurgePropagatesReadFailureWithoutMutations(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	expectStoragePurgeLocks(mock)
	mock.ExpectQuery(`SELECT name FROM upstream_suppliers.*FOR UPDATE`).WithArgs(int64(7)).WillReturnError(sql.ErrConnDone)
	mock.ExpectRollback()
	_, err = (&upstreamCenterRepository{db: db}).PurgeStorage(context.Background(), service.UpstreamStoragePurgeInput{Kind: "supplier", ID: 7, ConfirmName: "Supplier"})
	require.ErrorIs(t, err, sql.ErrConnDone)
	require.NoError(t, mock.ExpectationsWereMet())
}
