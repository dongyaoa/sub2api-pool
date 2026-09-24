package repository

import (
	"context"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

func TestUpstreamClaimRespectsLeaseAndPausedSchedule(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	repo := &upstreamCenterRepository{db: db}
	mock.ExpectExec(`UPDATE upstream_targets SET lease_until=.*lease_until IS NULL OR lease_until < NOW\(\).*enabled AND next_check_at <= NOW\(\)`).WithArgs(int64(9), "attempt-1", false).WillReturnResult(sqlmock.NewResult(0, 0))
	claimed, err := repo.ClaimCheck(context.Background(), 9, "attempt-1", false)
	require.NoError(t, err)
	require.False(t, claimed)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUpstreamCompleteRequiresMatchingLeaseBeforeHistoryInsert(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	repo := &upstreamCenterRepository{db: db}
	mock.ExpectBegin()
	mock.ExpectQuery(`UPDATE upstream_targets SET last_checked_at=.*check_token=\$2`).WithArgs(int64(9), "stale-attempt").WillReturnRows(sqlmock.NewRows([]string{"supplier_id", "name", "supplier_name"}))
	mock.ExpectRollback()
	saved, err := repo.CompleteCheck(context.Background(), 9, "stale-attempt", []*service.UpstreamHistoryRecord{{Model: "test", Status: "operational"}})
	require.NoError(t, err)
	require.False(t, saved)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUpstreamArchiveRejectsInFlightSupplierProbe(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	repo := &upstreamCenterRepository{db: db}
	mock.ExpectBegin()
	mock.ExpectExec(`SELECT pg_advisory_xact_lock\(251,0\)`).WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectQuery(`SELECT id FROM upstream_suppliers .* FOR UPDATE`).WithArgs(int64(3)).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(3))
	mock.ExpectQuery(`SELECT lease_until > NOW\(\).*FOR UPDATE`).WithArgs(int64(3)).WillReturnRows(sqlmock.NewRows([]string{"busy"}).AddRow(true))
	mock.ExpectRollback()
	require.ErrorIs(t, repo.ArchiveSupplier(context.Background(), 3), service.ErrUpstreamBusy)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUpstreamStatisticsKeepModelsSeparateAndUnknownUnsampled(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	repo := &upstreamCenterRepository{db: db}
	from := time.Now().Add(-24 * time.Hour)
	old, newer := from.Add(time.Hour), from.Add(2*time.Hour)
	target := &service.UpstreamTarget{ID: 9, Models: []string{"claude", "gpt", "empty"}}
	mock.ExpectQuery(`SELECT target_id,model, COUNT\(\*\)`).WithArgs(pq.Array([]int64{9}), from).WillReturnRows(sqlmock.NewRows([]string{"target_id", "model", "count", "success", "avg", "p95", "samples7d", "success7d"}).AddRow(9, "claude", 2, 1, 150.0, 150.0, 4, 3).AddRow(9, "gpt", 1, 1, 200.0, 200.0, 1, 1))
	mock.ExpectQuery(`SELECT h.id,h.target_id.*LIMIT 60`).WithArgs(pq.Array([]int64{9})).WillReturnRows(sqlmock.NewRows([]string{"id", "target_id", "model", "status", "latency_ms", "ping_latency_ms", "http_status", "message", "checked_at", "cost", "cost_source"}).AddRow(1, 9, "claude", "operational", 150, nil, 200, "", old, nil, "unknown").AddRow(2, 9, "gpt", "operational", 200, nil, 200, "", old, nil, "unknown").AddRow(3, 9, "claude", "error", 300, nil, 500, "HTTP 500", newer, nil, "unknown"))
	require.NoError(t, repo.PopulateStatistics(context.Background(), []*service.UpstreamTarget{target}, from))
	require.Len(t, target.Statistics, 3)
	claude, gpt, empty := target.Statistics[0], target.Statistics[1], target.Statistics[2]
	require.Equal(t, 50.0, *claude.Availability)
	require.Equal(t, 75.0, *claude.Availability7d)
	require.Equal(t, 300, *claude.LatestLatencyMs)
	require.Equal(t, "error", claude.Status)
	require.Len(t, claude.Timeline, 2)
	require.True(t, claude.Timeline[0].CheckedAt.Before(claude.Timeline[1].CheckedAt))
	require.Len(t, gpt.Timeline, 1)
	require.Equal(t, "gpt", gpt.Timeline[0].Model)
	require.Nil(t, empty.Availability)
	require.Nil(t, empty.Availability7d)
	require.Nil(t, empty.LatestLatencyMs)
	require.Nil(t, empty.AvgLatencyMs)
	require.Equal(t, "unknown", empty.Status)
	require.Empty(t, empty.Timeline)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestUpstreamHistoryPreservesExactModelAndHalfOpenRange(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	repo := &upstreamCenterRepository{db: db}
	from, to := time.Now().Add(-time.Hour), time.Now()
	where := `target_id=$1 AND model=$2 AND checked_at >= $3 AND checked_at < $4`
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT COUNT(*) FROM upstream_monitor_history WHERE `+where)).WithArgs(int64(9), "vendor/model:latest", from, to).WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(0))
	mock.ExpectQuery(regexp.QuoteMeta(`SELECT `+upstreamHistoryColumns+` FROM upstream_monitor_history WHERE `+where+` ORDER BY checked_at DESC,id DESC LIMIT $5 OFFSET $6`)).WithArgs(int64(9), "vendor/model:latest", from, to, 50, 0).WillReturnRows(sqlmock.NewRows([]string{"id", "target_id", "model", "status", "latency_ms", "ping_latency_ms", "http_status", "message", "checked_at", "cost", "cost_source"}))
	page, err := repo.History(context.Background(), service.UpstreamHistoryQuery{TargetID: 9, Model: "vendor/model:latest", From: &from, To: &to, Page: 1, PageSize: 50})
	require.NoError(t, err)
	require.Zero(t, page.Total)
	require.NotNil(t, page.Items)
	require.NoError(t, mock.ExpectationsWereMet())
}
