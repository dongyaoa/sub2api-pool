package repository

import (
	"context"
	"regexp"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/lib/pq"
	"github.com/stretchr/testify/require"
)

func TestIntelligenceRepositoryListAddsOneBatchOnlyForEnabledCandyPlans(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		t.Run(map[bool]string{false: "disabled", true: "enabled"}[enabled], func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			defer func() { _ = db.Close() }()
			repo := &intelligenceMonitorRepository{db: db}
			mock.ExpectQuery(regexp.QuoteMeta(intelligencePlanSourceNamesSQL)).WithArgs(pq.Array([]int64{3, 4})).WillReturnRows(sqlmock.NewRows([]string{"id", "name", "candy_enabled"}).AddRow(3, "Current source", enabled).AddRow(4, nil, false))
			mock.ExpectQuery(regexp.QuoteMeta(intelligencePlanRecentRunsSQL)).WithArgs(pq.Array([]int64{3, 4}), service.IntelligenceMonitorRetainedRuns, service.IntelligenceMonitorTestPelican).WillReturnRows(sqlmock.NewRows(strings.Split(intelligenceRunColumns, ",")))
			if enabled {
				mock.ExpectQuery(regexp.QuoteMeta(intelligencePlanRecentRunsSQL)).WithArgs(pq.Array([]int64{3}), service.IntelligenceMonitorCandyRetainedRuns, service.IntelligenceMonitorTestCandy).WillReturnRows(sqlmock.NewRows(strings.Split(intelligenceRunColumns, ",")))
			}
			data, err := repo.LoadPlanListData(context.Background(), []int64{3, 4, 3, 0, -1})
			require.NoError(t, err)
			require.Equal(t, "Current source", data.SourceNames[3])
			require.NotNil(t, data.CandyRuns)
			require.Empty(t, data.CandyRuns)
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}
