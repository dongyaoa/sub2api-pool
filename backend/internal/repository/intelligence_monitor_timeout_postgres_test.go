package repository

import (
	"fmt"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/Wei-Shaw/sub2api/migrations"
	"github.com/stretchr/testify/require"
)

func TestIntelligenceMonitorPostgresGenerationTimeoutMigrationPreservesSnapshotsAndSchedules(t *testing.T) {
	db, ctx := intelligenceMonitorTestDB(t, true)
	type snapshot struct {
		planID, runID int64
		timeout       int
		plan, run     string
	}
	var snapshots []snapshot
	for sourceIndex, source := range []string{"external", "upstream", "local_group", "openai_oauth"} {
		for i, timeout := range []int{180, 240, 300} {
			var row snapshot
			row.timeout = timeout
			require.NoError(t, db.QueryRowContext(ctx, `INSERT INTO intelligence_monitor_plans(name,source_type,timeout_seconds,enabled,interval_seconds,created_by,next_run_at,last_run_at,updated_at,notes) VALUES($1,$2,$3,$4,3600,1,'2026-09-25T00:00:00Z','2026-09-23T00:00:00Z','2026-09-22T00:00:00Z','preserve settings') RETURNING id`, fmt.Sprintf("%s-%d", source, timeout), source, timeout, (sourceIndex+i)%2 == 0).Scan(&row.planID))
			status := []string{"succeeded", "pending", "running"}[i]
			require.NoError(t, db.QueryRowContext(ctx, `INSERT INTO intelligence_monitor_runs(plan_id,plan_name,status,trigger,model,reasoning_effort,prompt,source_type,source_name,source_endpoint,api_mode,timeout_seconds,request_key_encrypted,lease_token,lease_until) VALUES($1,'preserved snapshot',$2,'manual','gpt-6-astra','high','fixed prompt',$3,'source','','responses',$4,'private snapshot credential','original lease',NOW()+INTERVAL '8 minutes') RETURNING id`, row.planID, status, source, timeout).Scan(&row.runID))
			require.NoError(t, db.QueryRowContext(ctx, `SELECT (to_jsonb(p)-'timeout_seconds')::text FROM intelligence_monitor_plans p WHERE id=$1`, row.planID).Scan(&row.plan))
			require.NoError(t, db.QueryRowContext(ctx, `SELECT to_jsonb(r)::text FROM intelligence_monitor_runs r WHERE id=$1`, row.runID).Scan(&row.run))
			snapshots = append(snapshots, row)
		}
	}
	migration, err := migrations.FS.ReadFile("250_intelligence_monitor_generation_timeout.sql")
	require.NoError(t, err)
	for range 2 {
		_, err = db.ExecContext(ctx, string(migration))
		require.NoError(t, err)
		for _, before := range snapshots {
			var plan, run string
			var timeout int
			require.NoError(t, db.QueryRowContext(ctx, `SELECT timeout_seconds,(to_jsonb(p)-'timeout_seconds')::text FROM intelligence_monitor_plans p WHERE id=$1`, before.planID).Scan(&timeout, &plan))
			require.Equal(t, 900, timeout)
			require.JSONEq(t, before.plan, plan, "all plan metadata and schedules must stay unchanged")
			require.NoError(t, db.QueryRowContext(ctx, `SELECT to_jsonb(r)::text FROM intelligence_monitor_runs r WHERE id=$1`, before.runID).Scan(&run))
			require.JSONEq(t, before.run, run, "terminal, pending and running snapshots must not be rewritten")
		}
	}
	var id int64
	var timeout int
	require.NoError(t, db.QueryRowContext(ctx, `INSERT INTO intelligence_monitor_plans(name,source_type,created_by) VALUES('Default timeout','external',1) RETURNING id,timeout_seconds`).Scan(&id, &timeout))
	require.Equal(t, 900, timeout)
	for _, timeout := range []int{179, 901} {
		_, err = db.ExecContext(ctx, `UPDATE intelligence_monitor_plans SET timeout_seconds=$2 WHERE id=$1`, id, timeout)
		require.Error(t, err)
	}
	for _, timeout := range []int{180, 240, 300, 600, 900} {
		_, err = db.ExecContext(ctx, `UPDATE intelligence_monitor_plans SET timeout_seconds=$2 WHERE id=$1`, id, timeout)
		require.NoError(t, err)
	}
}

func TestIntelligenceMonitorPostgresLeaseCoversFifteenMinuteGeneration(t *testing.T) {
	db, ctx := intelligenceMonitorTestDB(t)
	repo := &intelligenceMonitorRepository{db: db}
	for i := range 3 {
		plan := &service.IntelligenceMonitorPlan{Name: fmt.Sprintf("lease-%d", i), SourceType: "external", APIMode: "responses", IntervalSeconds: 3600, TimeoutSeconds: 900, CreatedBy: 1}
		require.NoError(t, repo.SavePlan(ctx, plan))
		runTimeout := 900
		if i == 2 {
			runTimeout = 240 // Legacy snapshot queued before a plan timeout upgrade.
		}
		run := &service.IntelligenceMonitorRun{PlanID: plan.ID, PlanUpdatedAt: plan.UpdatedAt, PlanName: plan.Name, SourceType: plan.SourceType, Trigger: "manual", Model: service.IntelligenceMonitorModel, ReasoningEffort: service.IntelligenceMonitorReasoning, Prompt: service.IntelligenceMonitorPrompt, APIMode: "responses", TimeoutSeconds: runTimeout, RequestKeyEncrypted: "encrypted fixture", SourceSnapshot: map[string]any{}, NotesSnapshot: map[string]string{}}
		require.NoError(t, repo.Enqueue(ctx, run, false))
	}
	first, err := repo.ClaimNext(ctx, "long-generation-1")
	require.NoError(t, err)
	require.NotNil(t, first)
	second, err := repo.ClaimNext(ctx, "long-generation-2")
	require.NoError(t, err)
	require.NotNil(t, second)
	var leaseSeconds float64
	require.NoError(t, db.QueryRowContext(ctx, `SELECT EXTRACT(EPOCH FROM lease_until-started_at) FROM intelligence_monitor_runs WHERE id=$1`, first.ID).Scan(&leaseSeconds))
	require.Equal(t, float64(900+service.IntelligenceMonitorLeaseGraceSeconds), leaseSeconds)
	// Move the claimed timestamps nine minutes back without waiting or issuing
	// any upstream request. The old eight-minute lease would already be expired.
	_, err = db.ExecContext(ctx, `UPDATE intelligence_monitor_runs SET started_at=started_at-INTERVAL '9 minutes',lease_until=lease_until-INTERVAL '9 minutes' WHERE id=$1`, first.ID)
	require.NoError(t, err)
	require.NoError(t, repo.ExpireRuns(ctx))
	active, err := repo.GetRun(ctx, first.ID)
	require.NoError(t, err)
	require.Equal(t, "running", active.Status)
	require.Less(t, time.Until(*active.StartedAt), -8*time.Minute)
	third, err := repo.ClaimNext(ctx, "must-stay-queued")
	require.NoError(t, err)
	require.Nil(t, third, "the two live long-running leases must still consume both worker slots")
	_, err = db.ExecContext(ctx, `UPDATE intelligence_monitor_runs SET lease_until=NOW()-INTERVAL '1 second' WHERE id=$1`, first.ID)
	require.NoError(t, err)
	require.NoError(t, repo.ExpireRuns(ctx))
	expired, err := repo.GetRun(ctx, first.ID)
	require.NoError(t, err)
	require.Equal(t, "failed", expired.Status)
	require.Empty(t, expired.RequestKeyEncrypted)
	require.Contains(t, expired.Error, "not automatically retried")
	var count int
	require.NoError(t, db.QueryRowContext(ctx, `SELECT COUNT(*) FROM intelligence_monitor_runs WHERE plan_id=$1`, first.PlanID).Scan(&count))
	require.Equal(t, 1, count, "expiration must never requeue or duplicate the generation")
	third, err = repo.ClaimNext(ctx, "released-slot")
	require.NoError(t, err)
	require.NotNil(t, third)
	require.NotEqual(t, first.PlanID, third.PlanID)
	require.Equal(t, 240, third.TimeoutSeconds, "claiming must preserve a legacy queued snapshot despite the 900-second plan")
	require.NoError(t, db.QueryRowContext(ctx, `SELECT EXTRACT(EPOCH FROM lease_until-started_at) FROM intelligence_monitor_runs WHERE id=$1`, third.ID).Scan(&leaseSeconds))
	require.Equal(t, float64(240+service.IntelligenceMonitorLeaseGraceSeconds), leaseSeconds)
}
