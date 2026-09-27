package repository

import (
	"context"
	"fmt"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestIntelligenceQueueRejectsInvalidPoolBeforeClaim(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	repo := &intelligenceMonitorRepository{db: db}
	for _, tc := range []struct {
		kind  string
		limit int
	}{{"", 8}, {"unknown", 8}, {"pelican", 0}, {"pelican", -1}, {"pelican", 257}, {"candy", 0}, {"candy", -1}, {"candy", 129}} {
		t.Run(fmt.Sprintf("%s-%d", tc.kind, tc.limit), func(t *testing.T) {
			run, err := repo.ClaimNextForKind(context.Background(), "worker", tc.kind, tc.limit)
			require.ErrorIs(t, err, service.ErrIntelligenceInvalid)
			require.Nil(t, run)
		})
	}
	require.NoError(t, mock.ExpectationsWereMet(), "invalid parameters must not open a transaction or consume a task")
}
