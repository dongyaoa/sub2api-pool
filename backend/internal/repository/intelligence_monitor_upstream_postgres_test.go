package repository

import (
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/Wei-Shaw/sub2api/migrations"
	"github.com/stretchr/testify/require"
)

func TestIntelligenceUpstreamPlansPostgresFilterAndLegacyEdits(t *testing.T) {
	db, ctx, _ := manualOrderTestDB(t)
	applyManualOrderMigration(t, db, ctx)
	_, err := db.ExecContext(ctx, `ALTER TABLE accounts ADD COLUMN name VARCHAR(100) NOT NULL DEFAULT '';
CREATE TABLE groups(id BIGINT PRIMARY KEY,name VARCHAR(100),deleted_at TIMESTAMPTZ);
INSERT INTO intelligence_monitor_plans(id,name,source_type,upstream_target_id,created_by,deleted_at,sort_order) VALUES
(301,'Legacy first','upstream',10,1,NULL,2),(302,'Legacy second','upstream',10,1,NULL,1),
(303,'Other group','upstream',11,1,NULL,1),(304,'Archived','upstream',10,1,NOW(),0),
(305,'Other source stale link','external',10,1,NULL,0);
INSERT INTO intelligence_monitor_runs(plan_id,plan_name,status,trigger,model,reasoning_effort,prompt,source_type,source_name,source_endpoint,api_mode,timeout_seconds,html,raw_text,request_key_encrypted,created_at)
SELECT p.id,p.name,'succeeded','manual','m','high','draw',p.source_type,'Historical source','','responses',600,'<html>large artwork</html>','large raw result','secret-request',NOW()+make_interval(secs=>n)
FROM intelligence_monitor_plans p CROSS JOIN generate_series(1,25) n WHERE p.id BETWEEN 301 AND 305;
INSERT INTO intelligence_monitor_runs(plan_id,plan_name,status,trigger,model,reasoning_effort,prompt,source_type,source_name,source_endpoint,api_mode,timeout_seconds,created_at)
VALUES(301,'Legacy first','running','manual','m','high','draw','upstream','Historical source','','responses',600,NOW()-INTERVAL '1 hour')`)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `UPDATE intelligence_monitor_runs SET rate_snapshot='"unrelated malformed billing"'::jsonb WHERE plan_id=303`)
	require.NoError(t, err, "unrelated plan metadata must never be decoded by this target's filtered read")
	migration, err := migrations.FS.ReadFile("256_intelligence_upstream_plan_lookup.sql")
	require.NoError(t, err)
	for range 2 {
		_, err = db.ExecContext(ctx, string(migration))
		require.NoError(t, err, "existing duplicates survive repeated migration")
	}
	repo := &intelligenceMonitorRepository{db: db}
	svc := service.NewIntelligenceMonitorService(repo, intelligencePGEncryptor{}, nil, nil, nil, nil, nil)
	plans, err := svc.ListPlansForUpstream(ctx, 10)
	require.NoError(t, err)
	require.Len(t, plans, 2)
	require.Equal(t, int64(302), plans[0].ID)
	require.Equal(t, int64(301), plans[1].ID)
	require.Equal(t, "running", plans[1].LatestRun.Status, "the older active run remains visible alongside twenty works")
	for _, plan := range plans {
		require.Equal(t, "upstream", plan.SourceType)
		require.Len(t, plan.RecentRuns, 20)
		for _, run := range append(plan.RecentRuns, plan.LatestRun) {
			require.Equal(t, plan.ID, run.PlanID)
			require.Empty(t, run.HTML)
			require.Empty(t, run.RawText)
			require.Empty(t, run.RequestKeyEncrypted)
		}
	}
	for _, id := range []int64{20, 999} {
		plans, err = svc.ListPlansForUpstream(ctx, id)
		require.NoError(t, err)
		require.NotNil(t, plans)
		require.Empty(t, plans)
	}
	// Legacy duplicate membership stays intact during editing or pausing.
	plan, err := repo.GetPlan(ctx, 302)
	require.NoError(t, err)
	plan.Notes = "ordinary edit"
	require.NoError(t, repo.SavePlan(ctx, plan))
	enabled := false
	_, err = svc.SavePlan(ctx, 301, 1, service.IntelligenceMonitorInput{Enabled: &enabled})
	require.NoError(t, err, "pausing remains possible while a legacy duplicate is generating")
	var count int
	require.NoError(t, db.QueryRowContext(ctx, `SELECT COUNT(*) FROM intelligence_monitor_plans WHERE source_type='upstream' AND upstream_target_id=10 AND deleted_at IS NULL`).Scan(&count))
	require.Equal(t, 2, count)
	plan, err = repo.GetPlan(ctx, 303)
	require.NoError(t, err)
	targetID := int64(10)
	plan.UpstreamTargetID = &targetID
	require.ErrorIs(t, repo.SavePlan(ctx, plan), service.ErrIntelligenceUpstreamPlanExists, "another upstream cannot move into an occupied group")
	plan, err = repo.GetPlan(ctx, 305)
	require.NoError(t, err)
	plan.SourceType = "upstream"
	require.ErrorIs(t, repo.SavePlan(ctx, plan), service.ErrIntelligenceUpstreamPlanExists, "another source cannot enter an occupied group")
}

func TestIntelligenceUpstreamPlansPostgresConcurrentCreation(t *testing.T) {
	db, ctx, newDB := manualOrderTestDB(t)
	applyManualOrderMigration(t, db, ctx)
	firstDB, secondDB := newDB(), newDB()
	firstRepo, secondRepo := &intelligenceMonitorRepository{db: firstDB}, &intelligenceMonitorRepository{db: secondDB}
	targetID := int64(20)
	newPlan := func(name string) *service.IntelligenceMonitorPlan {
		return &service.IntelligenceMonitorPlan{Name: name, SourceType: "upstream", UpstreamTargetID: &targetID, APIMode: "responses", IntervalSeconds: 300, TimeoutSeconds: 600, CreatedBy: 1}
	}
	// Block both independent connections at the existing membership lock, then
	// release together. Exactly one insert can commit; the other observes it.
	lock, err := db.BeginTx(ctx, nil)
	require.NoError(t, err)
	defer func() { _ = lock.Rollback() }()
	_, err = lock.ExecContext(ctx, `SELECT pg_advisory_xact_lock(251,0)`)
	require.NoError(t, err)
	first, second := newPlan("First contender"), newPlan("Second contender")
	results := make(chan error, 2)
	started := make(chan struct{}, 2)
	for i, repo := range []*intelligenceMonitorRepository{firstRepo, secondRepo} {
		plan := []*service.IntelligenceMonitorPlan{first, second}[i]
		go func() { started <- struct{}{}; results <- repo.SavePlan(ctx, plan) }()
	}
	<-started
	<-started
	select {
	case err := <-results:
		t.Fatalf("save bypassed held membership lock: %v", err)
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
				require.ErrorIs(t, err, service.ErrIntelligenceUpstreamPlanExists)
				conflicts++
			}
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
	require.Equal(t, 1, success)
	require.Equal(t, 1, conflicts)
	var winner int64
	require.NoError(t, db.QueryRowContext(ctx, `SELECT id FROM intelligence_monitor_plans WHERE source_type='upstream' AND upstream_target_id=$1 AND deleted_at IS NULL`, targetID).Scan(&winner))
	require.NoError(t, firstRepo.ArchivePlan(ctx, winner))
	require.NoError(t, secondRepo.SavePlan(ctx, newPlan("Replacement after archive")))
}
