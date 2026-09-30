package repository

import (
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestIntelligenceOAuthPlansPostgresDeduplicatesAccountsAndPreservesLegacyEdits(t *testing.T) {
	db, ctx, _ := manualOrderTestDB(t)
	applyManualOrderMigration(t, db, ctx)
	_, err := db.ExecContext(ctx, `INSERT INTO accounts(id,type) VALUES(901,'oauth'),(902,'oauth'),(903,'oauth');
INSERT INTO intelligence_monitor_plans(id,name,source_type,account_id,created_by,enabled,deleted_at) VALUES
(401,'Legacy first','openai_oauth',901,1,TRUE,NULL),
(402,'Legacy paused duplicate','openai_oauth',901,1,FALSE,NULL),
(403,'Other account','openai_oauth',902,1,FALSE,NULL),
(404,'Archived','openai_oauth',903,1,FALSE,NOW()),
(405,'Other source stale account','external',901,1,FALSE,NULL)`)
	require.NoError(t, err)
	repo := &intelligenceMonitorRepository{db: db}
	accountID := int64(901)
	newPlan := func() *service.IntelligenceMonitorPlan {
		return &service.IntelligenceMonitorPlan{Name: "New account monitor", SourceType: "openai_oauth", AccountID: &accountID, APIMode: "responses", IntervalSeconds: 300, TimeoutSeconds: 600, CreatedBy: 1}
	}
	require.ErrorIs(t, repo.SavePlan(ctx, newPlan()), service.ErrIntelligenceOAuthPlanExists)
	// Ordinary edits and pause toggles remain possible for old duplicate plans.
	first, err := repo.GetPlan(ctx, 401)
	require.NoError(t, err)
	first.Enabled = false
	first.Notes = "Edited without rebinding"
	require.NoError(t, repo.SavePlan(ctx, first))
	second, err := repo.GetPlan(ctx, 402)
	require.NoError(t, err)
	second.Enabled = true
	require.NoError(t, repo.SavePlan(ctx, second))
	second.Enabled = false
	require.NoError(t, repo.SavePlan(ctx, second))
	require.ErrorIs(t, repo.SavePlan(ctx, newPlan()), service.ErrIntelligenceOAuthPlanExists, "paused plans still reserve the account")
	// Rebinding a different account or changing source cannot enter an occupied account.
	other, err := repo.GetPlan(ctx, 403)
	require.NoError(t, err)
	other.AccountID = &accountID
	require.ErrorIs(t, repo.SavePlan(ctx, other), service.ErrIntelligenceOAuthPlanExists)
	external, err := repo.GetPlan(ctx, 405)
	require.NoError(t, err)
	external.SourceType = "openai_oauth"
	require.ErrorIs(t, repo.SavePlan(ctx, external), service.ErrIntelligenceOAuthPlanExists)
	var count int
	require.NoError(t, db.QueryRowContext(ctx, `SELECT COUNT(*) FROM intelligence_monitor_plans WHERE source_type='openai_oauth' AND account_id=901 AND deleted_at IS NULL`).Scan(&count))
	require.Equal(t, 2, count, "rejected writes leave original membership intact")
	// A stale account pointer on another source must not reserve the account.
	require.NoError(t, repo.ArchivePlan(ctx, first.ID))
	require.ErrorIs(t, repo.SavePlan(ctx, newPlan()), service.ErrIntelligenceOAuthPlanExists, "one remaining legacy plan still reserves the account")
	require.NoError(t, repo.ArchivePlan(ctx, second.ID))
	require.NoError(t, repo.SavePlan(ctx, newPlan()), "archiving all account monitors permits recreation")
	// An archived account monitor also does not prevent another plan rebinding.
	accountID = 903
	other, err = repo.GetPlan(ctx, 403)
	require.NoError(t, err)
	other.AccountID = &accountID
	require.NoError(t, repo.SavePlan(ctx, other))
}

func TestIntelligenceOAuthPlansPostgresConcurrentCreation(t *testing.T) {
	db, ctx, newDB := manualOrderTestDB(t)
	applyManualOrderMigration(t, db, ctx)
	_, err := db.ExecContext(ctx, `INSERT INTO accounts(id,type) VALUES(901,'oauth')`)
	require.NoError(t, err)
	firstRepo := &intelligenceMonitorRepository{db: newDB()}
	secondRepo := &intelligenceMonitorRepository{db: newDB()}
	accountID := int64(901)
	newPlan := func(name string) *service.IntelligenceMonitorPlan {
		return &service.IntelligenceMonitorPlan{Name: name, SourceType: "openai_oauth", AccountID: &accountID, APIMode: "responses", IntervalSeconds: 300, TimeoutSeconds: 600, CreatedBy: 1}
	}
	// Independent writers block on the same membership lock. Once released,
	// one commits and the next observes that live account reservation.
	lock, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer func() { _ = lock.Rollback() }()
	_, err = lock.ExecContext(ctx, `SELECT pg_advisory_xact_lock(251,0)`)
	require.NoError(t, err)
	results := make(chan error, 2)
	started := make(chan struct{}, 2)
	for i, repo := range []*intelligenceMonitorRepository{firstRepo, secondRepo} {
		plan := newPlan([]string{"First contender", "Second contender"}[i])
		go func() {
			started <- struct{}{}
			results <- repo.SavePlan(ctx, plan)
		}()
	}
	<-started
	<-started
	select {
	case err := <-results:
		t.Fatalf("save bypassed the held membership lock: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	require.NoError(t, lock.Commit())
	var success, conflicts int
	for range 2 {
		select {
		case err := <-results:
			if err == nil {
				success++
			} else {
				require.ErrorIs(t, err, service.ErrIntelligenceOAuthPlanExists)
				conflicts++
			}
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	require.Equal(t, 1, success)
	require.Equal(t, 1, conflicts)
	var count int
	require.NoError(t, db.QueryRowContext(ctx, `SELECT COUNT(*) FROM intelligence_monitor_plans WHERE source_type='openai_oauth' AND account_id=$1 AND deleted_at IS NULL`, accountID).Scan(&count))
	require.Equal(t, 1, count)
}
