package repository

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/Wei-Shaw/sub2api/migrations"
	"github.com/stretchr/testify/require"
)

func candyRepositoryPlan(t *testing.T, ctx context.Context, repo *intelligenceMonitorRepository, name string, enabled, candy bool) *service.IntelligenceMonitorPlan {
	t.Helper()
	plan := &service.IntelligenceMonitorPlan{Name: name, SourceType: "external", Endpoint: "https://example.test", APIKeyEncrypted: "cipher:test-only", APIMode: "responses", Enabled: enabled, CandyEnabled: candy, IntervalSeconds: 97, TimeoutSeconds: 600, CreatedBy: 1}
	require.NoError(t, repo.SavePlan(ctx, plan))
	return plan
}

func candyRepositoryRun(plan *service.IntelligenceMonitorPlan, kind string) *service.IntelligenceMonitorRun {
	rate := 1.25
	run := &service.IntelligenceMonitorRun{PlanID: plan.ID, PlanUpdatedAt: plan.UpdatedAt, PlanName: plan.Name, TestKind: kind, Trigger: "manual", Model: service.IntelligenceMonitorModel, ReasoningEffort: service.IntelligenceMonitorReasoning, Prompt: service.IntelligenceMonitorPrompt, SourceType: plan.SourceType, SourceName: plan.Name, SourceEndpoint: plan.Endpoint, SourceSnapshot: map[string]any{"source": "private-schema-fixture"}, RateSnapshot: &service.UpstreamRemoteBillingSnapshot{GroupRateMultiplier: &rate, Source: "fixture", Status: "ok"}, NotesSnapshot: map[string]string{"group": "private group"}, APIMode: plan.APIMode, TimeoutSeconds: plan.TimeoutSeconds, RequestKeyEncrypted: plan.APIKeyEncrypted}
	if kind == service.IntelligenceMonitorTestCandy {
		run.Prompt = service.IntelligenceMonitorCandyPrompt
	}
	return run
}

func candyRepositoryConnection(t *testing.T, ctx context.Context, db *sql.DB) *sql.DB {
	t.Helper()
	var schema string
	require.NoError(t, db.QueryRowContext(ctx, `SELECT current_schema()`).Scan(&schema))
	other, err := sql.Open("postgres", os.Getenv("UPSTREAM_TEST_DATABASE_URL"))
	require.NoError(t, err)
	other.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = other.Close() })
	_, err = other.ExecContext(ctx, `SET search_path TO `+pqQuoteIdentifier(schema))
	require.NoError(t, err)
	return other
}

func TestIntelligenceCandyPostgresMigrationPreservesExistingWorks(t *testing.T) {
	db, ctx := intelligenceMonitorTestDB(t)
	// Roll back only the new schema elements inside this test's private schema
	// so the real migration is exercised against pre-existing paid artwork.
	_, err := db.ExecContext(ctx, `DROP INDEX idx_intelligence_runs_active_plan_kind;
DROP INDEX idx_intelligence_runs_kind_history;
ALTER TABLE intelligence_monitor_runs DROP COLUMN test_kind,DROP COLUMN correct,DROP COLUMN answer;
ALTER TABLE intelligence_monitor_plans DROP COLUMN candy_enabled;
CREATE UNIQUE INDEX idx_intelligence_runs_active_plan ON intelligence_monitor_runs(plan_id) WHERE status IN ('pending','running');
INSERT INTO intelligence_monitor_plans(id,name,source_type,created_by,enabled,next_run_at) VALUES(1,'Legacy','external',1,TRUE,'2026-10-01T00:00:00Z');
INSERT INTO intelligence_monitor_runs(plan_id,plan_name,status,trigger,model,reasoning_effort,prompt,source_type,source_name,source_endpoint,api_mode,timeout_seconds,html,raw_text)
VALUES(1,'Legacy','succeeded','manual','gpt-6-astra','high','old prompt','external','Legacy','','responses',600,'<html>preserved</html>','original raw');`)
	require.NoError(t, err)
	var before string
	require.NoError(t, db.QueryRowContext(ctx, `SELECT to_jsonb(p)::text FROM intelligence_monitor_plans p WHERE id=1`).Scan(&before))
	body, err := migrations.FS.ReadFile("257_intelligence_candy_monitor.sql")
	require.NoError(t, err)
	for range 2 {
		_, err = db.ExecContext(ctx, string(body))
		require.NoError(t, err, "candy migration is idempotent")
	}
	var after string
	require.NoError(t, db.QueryRowContext(ctx, `SELECT (to_jsonb(p)-'candy_enabled')::text FROM intelligence_monitor_plans p WHERE id=1`).Scan(&after))
	require.JSONEq(t, before, after)
	repo := &intelligenceMonitorRepository{db: db}
	plan, err := repo.GetPlan(ctx, 1)
	require.NoError(t, err)
	require.False(t, plan.CandyEnabled)
	run, err := repo.GetRun(ctx, 1)
	require.NoError(t, err)
	require.Equal(t, service.IntelligenceMonitorTestPelican, run.TestKind)
	require.Nil(t, run.Correct)
	require.Empty(t, run.Answer)
	require.Equal(t, "<html>preserved</html>", run.HTML)
	require.Equal(t, "original raw", run.RawText)

	// Only the same kind conflicts; the two independent tasks may coexist.
	insert := `INSERT INTO intelligence_monitor_runs(plan_id,plan_name,status,trigger,model,reasoning_effort,prompt,source_type,source_name,source_endpoint,api_mode,timeout_seconds,test_kind)
SELECT plan_id,plan_name,'pending',trigger,model,reasoning_effort,prompt,source_type,source_name,source_endpoint,api_mode,timeout_seconds,$1 FROM intelligence_monitor_runs WHERE id=1`
	_, err = db.ExecContext(ctx, insert, "pelican")
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, insert, "candy")
	require.NoError(t, err)
	for _, kind := range []string{"pelican", "candy"} {
		_, err = db.ExecContext(ctx, insert, kind)
		require.ErrorIs(t, intelligenceDBError(err), service.ErrIntelligenceBusy)
	}
	_, err = db.ExecContext(ctx, insert, "unsupported")
	require.Error(t, err, "database rejects unrecognized test kinds")
}

func TestIntelligenceCandyPostgresEnqueueKindChecksAndSnapshots(t *testing.T) {
	db, ctx := intelligenceMonitorTestDB(t)
	repo := &intelligenceMonitorRepository{db: db}
	plan := candyRepositoryPlan(t, ctx, repo, "Scheduled pair", true, false)
	candy := candyRepositoryRun(plan, service.IntelligenceMonitorTestCandy)
	require.ErrorIs(t, repo.Enqueue(ctx, candy, false), service.ErrIntelligenceInvalid)
	invalid := candyRepositoryRun(plan, "unknown")
	require.ErrorIs(t, repo.Enqueue(ctx, invalid, false), service.ErrIntelligenceInvalid)
	plan.CandyEnabled = true
	require.NoError(t, repo.SavePlan(ctx, plan))
	stored, err := repo.GetPlan(ctx, plan.ID)
	require.NoError(t, err)
	require.True(t, stored.CandyEnabled)
	// Capturing a plan before its configuration changed cannot queue a stale job.
	require.ErrorIs(t, repo.Enqueue(ctx, candy, false), service.ErrIntelligenceNotFound)
	candy = candyRepositoryRun(plan, service.IntelligenceMonitorTestCandy)
	pelican := candyRepositoryRun(plan, "")
	pelican.Trigger = "scheduled"
	require.NoError(t, repo.Enqueue(ctx, pelican, true))
	require.Equal(t, service.IntelligenceMonitorTestPelican, pelican.TestKind)
	page, err := repo.ListRuns(ctx, service.IntelligenceMonitorRunQuery{PlanID: &plan.ID, TestKind: "candy", Page: 1, PageSize: 60})
	require.NoError(t, err)
	require.Zero(t, page.Total, "a pelican schedule never creates a companion")
	stored, err = repo.GetPlan(ctx, plan.ID)
	require.NoError(t, err)
	require.NotNil(t, stored.CandyNextRunAt, "queuing artwork does not consume the candy countdown")
	candy.Trigger = "scheduled"
	require.NoError(t, repo.Enqueue(ctx, candy, true))
	page, err = repo.ListRuns(ctx, service.IntelligenceMonitorRunQuery{PlanID: &plan.ID, TestKind: "candy", Page: 1, PageSize: 60})
	require.NoError(t, err)
	require.Equal(t, int64(1), page.Total)
	companion, err := repo.GetRun(ctx, page.Items[0].ID)
	require.NoError(t, err)
	require.Equal(t, service.IntelligenceMonitorCandyPrompt, companion.Prompt)
	require.NotEqual(t, pelican.Prompt, companion.Prompt)
	require.Equal(t, pelican.Trigger, companion.Trigger)
	require.Equal(t, pelican.Model, companion.Model)
	require.Equal(t, pelican.ReasoningEffort, companion.ReasoningEffort)
	require.Equal(t, pelican.SourceType, companion.SourceType)
	require.Equal(t, pelican.SourceName, companion.SourceName)
	require.Equal(t, pelican.SourceEndpoint, companion.SourceEndpoint)
	require.Equal(t, pelican.SourceSnapshot, companion.SourceSnapshot)
	require.Equal(t, pelican.RateSnapshot, companion.RateSnapshot)
	require.Equal(t, pelican.NotesSnapshot, companion.NotesSnapshot)
	require.Equal(t, pelican.APIMode, companion.APIMode)
	require.Equal(t, pelican.TimeoutSeconds, companion.TimeoutSeconds)
	require.Equal(t, pelican.RequestKeyEncrypted, companion.RequestKeyEncrypted)
	require.Empty(t, page.Items[0].RequestKeyEncrypted)
	stored, err = repo.GetPlan(ctx, plan.ID)
	require.NoError(t, err)
	require.Nil(t, stored.NextRunAt)
	require.Nil(t, stored.CandyNextRunAt, "each active kind consumes only its own countdown")
	require.ErrorIs(t, repo.Enqueue(ctx, candy, false), service.ErrIntelligenceBusy)
	require.ErrorIs(t, repo.ArchivePlan(ctx, plan.ID), service.ErrIntelligenceBusy)
	page, err = repo.ListRuns(ctx, service.IntelligenceMonitorRunQuery{PlanID: &plan.ID, Page: 1, PageSize: 20})
	require.NoError(t, err)
	require.Equal(t, int64(1), page.Total, "default gallery excludes candy")
	require.Equal(t, pelican.ID, page.Items[0].ID)
	_, err = repo.ListRuns(ctx, service.IntelligenceMonitorRunQuery{TestKind: "unknown", Page: 1, PageSize: 20})
	require.ErrorIs(t, err, service.ErrIntelligenceInvalid)
}

func TestIntelligenceCandyPostgresIndependentEnqueueIsAtomicAndConcurrentSafe(t *testing.T) {
	db, ctx := intelligenceMonitorTestDB(t)
	repo := &intelligenceMonitorRepository{db: db}
	plan := candyRepositoryPlan(t, ctx, repo, "Atomic round", true, true)
	_, err := db.ExecContext(ctx, `CREATE FUNCTION reject_test_candy() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN IF NEW.test_kind='candy' THEN RAISE EXCEPTION 'synthetic companion failure'; END IF; RETURN NEW; END $$;
CREATE TRIGGER reject_test_candy BEFORE INSERT ON intelligence_monitor_runs FOR EACH ROW EXECUTE FUNCTION reject_test_candy()`)
	require.NoError(t, err)
	run := candyRepositoryRun(plan, "pelican")
	require.NoError(t, repo.Enqueue(ctx, run, true), "candy insertion errors cannot affect independent artwork")
	require.Error(t, repo.Enqueue(ctx, candyRepositoryRun(plan, "candy"), true))
	var count int
	require.NoError(t, db.QueryRowContext(ctx, `SELECT COUNT(*) FROM intelligence_monitor_runs`).Scan(&count))
	require.Equal(t, 1, count, "failed candy enqueue leaves the artwork task intact")
	stored, err := repo.GetPlan(ctx, plan.ID)
	require.NoError(t, err)
	require.NotNil(t, stored.CandyNextRunAt, "failed transaction does not consume the candy schedule")
	_, err = db.ExecContext(ctx, `DROP TRIGGER reject_test_candy ON intelligence_monitor_runs`)
	require.NoError(t, err)
	other := &intelligenceMonitorRepository{db: candyRepositoryConnection(t, ctx, db)}
	errorsByCaller := make(chan error, 2)
	start := make(chan struct{})
	for _, r := range []*intelligenceMonitorRepository{repo, other} {
		go func(r *intelligenceMonitorRepository) {
			<-start
			errorsByCaller <- r.Enqueue(ctx, candyRepositoryRun(plan, "candy"), true)
		}(r)
	}
	close(start)
	succeeded := 0
	for range 2 {
		err = <-errorsByCaller
		if err == nil {
			succeeded++
		} else {
			require.True(t, errors.Is(err, service.ErrIntelligenceNotFound) || errors.Is(err, service.ErrIntelligenceBusy), "%v", err)
		}
	}
	require.Equal(t, 1, succeeded)
	require.NoError(t, db.QueryRowContext(ctx, `SELECT COUNT(*) FROM intelligence_monitor_runs`).Scan(&count))
	require.Equal(t, 2, count, "racing schedulers produce exactly one candy job")
	// Even a stale external due timestamp cannot accumulate same-kind work.
	_, err = db.ExecContext(ctx, `UPDATE intelligence_monitor_plans SET next_run_at=NOW(),candy_next_run_at=NOW()`)
	require.NoError(t, err)
	due, err := repo.DuePlanIDs(ctx, 10)
	require.NoError(t, err)
	require.Empty(t, due)
	due, err = repo.DueCandyPlanIDs(ctx, 10)
	require.NoError(t, err)
	require.Empty(t, due)
	require.ErrorIs(t, repo.Enqueue(ctx, candyRepositoryRun(plan, "pelican"), true), service.ErrIntelligenceBusy)
	require.ErrorIs(t, repo.Enqueue(ctx, candyRepositoryRun(plan, "candy"), true), service.ErrIntelligenceBusy)
}

func TestIntelligenceCandyPostgresIndependentCompletionAndGlobalWorkers(t *testing.T) {
	db, ctx := intelligenceMonitorTestDB(t)
	repo := &intelligenceMonitorRepository{db: db}
	plan := candyRepositoryPlan(t, ctx, repo, "Pair lifecycle", true, true)
	second := candyRepositoryPlan(t, ctx, repo, "Second round", true, true)
	require.NoError(t, repo.Enqueue(ctx, candyRepositoryRun(plan, "pelican"), true))
	require.NoError(t, repo.Enqueue(ctx, candyRepositoryRun(plan, "candy"), true))
	require.NoError(t, repo.Enqueue(ctx, candyRepositoryRun(second, "pelican"), true))
	first, err := repo.ClaimNext(ctx, "first-worker")
	require.NoError(t, err)
	otherPlan, err := repo.ClaimNext(ctx, "second-worker")
	require.NoError(t, err)
	require.Equal(t, "pelican", first.TestKind)
	require.Equal(t, "pelican", otherPlan.TestKind)
	require.Equal(t, second.ID, otherPlan.PlanID, "another plan uses the second worker while this plan's candy waits")
	third, err := repo.ClaimNext(ctx, "third-worker")
	require.NoError(t, err)
	require.Nil(t, third, "the two-worker limit spans both test kinds")
	first.Status, first.HTML = "succeeded", "<html>artwork</html>"
	require.NoError(t, repo.CompleteRun(ctx, first))
	stored, err := repo.GetPlan(ctx, plan.ID)
	require.NoError(t, err)
	require.NotNil(t, stored.NextRunAt, "artwork starts its interval even while candy remains queued")
	require.Equal(t, 97*time.Second, stored.NextRunAt.Sub(*stored.LastRunAt))
	require.Nil(t, stored.CandyNextRunAt)
	pelicanNext := *stored.NextRunAt
	companion, err := repo.ClaimNext(ctx, "candy-worker")
	require.NoError(t, err)
	require.Equal(t, "candy", companion.TestKind)
	require.Equal(t, first.PlanID, companion.PlanID)
	third, err = repo.ClaimNext(ctx, "fourth-worker")
	require.NoError(t, err)
	require.Nil(t, third, "one pelican and one candy consume both global worker slots")
	correct := false
	companion.Status, companion.Correct, companion.Answer, companion.RawText = "succeeded", &correct, "synthetic wrong answer", "synthetic explanation"
	require.NoError(t, repo.CompleteRun(ctx, companion))
	detail, err := repo.GetRun(ctx, companion.ID)
	require.NoError(t, err)
	require.Equal(t, &correct, detail.Correct, "a wrong answer remains a completed generation")
	require.Equal(t, companion.Answer, detail.Answer)
	require.Equal(t, companion.RawText, detail.RawText)
	require.Empty(t, detail.RequestKeyEncrypted)
	require.NotNil(t, detail.DurationMs)
	require.ErrorIs(t, repo.CompleteRun(ctx, companion), service.ErrIntelligenceNotFound, "a stale worker cannot complete twice")
	stored, err = repo.GetPlan(ctx, plan.ID)
	require.NoError(t, err)
	require.NotNil(t, stored.NextRunAt)
	require.Equal(t, pelicanNext, *stored.NextRunAt, "candy completion does not change the artwork countdown")
	require.NotNil(t, stored.CandyNextRunAt)
	require.Equal(t, 180*time.Second, stored.CandyNextRunAt.Sub(*stored.CandyLastRunAt))
	require.NoError(t, repo.ArchivePlan(ctx, plan.ID))
	detail, err = repo.GetRun(ctx, companion.ID)
	require.NoError(t, err)
	require.Equal(t, companion.Answer, detail.Answer, "archive preserves candy results")
}

func TestIntelligenceCandyPostgresExpiryReschedulesOnlyItsKind(t *testing.T) {
	db, ctx := intelligenceMonitorTestDB(t)
	repo := &intelligenceMonitorRepository{db: db}
	plan := candyRepositoryPlan(t, ctx, repo, "Expiry lifecycle", true, true)
	require.NoError(t, repo.Enqueue(ctx, candyRepositoryRun(plan, "pelican"), true))
	require.NoError(t, repo.Enqueue(ctx, candyRepositoryRun(plan, "candy"), true))
	first, err := repo.ClaimNext(ctx, "expiring-worker")
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `UPDATE intelligence_monitor_runs SET lease_until=NOW()-INTERVAL '1 minute' WHERE id=$1`, first.ID)
	require.NoError(t, err)
	require.NoError(t, repo.ExpireRuns(ctx))
	stored, err := repo.GetPlan(ctx, plan.ID)
	require.NoError(t, err)
	require.NotNil(t, stored.NextRunAt, "artwork expiry begins only its own interval")
	require.Equal(t, 97*time.Second, stored.NextRunAt.Sub(*stored.LastRunAt))
	pelicanNext := *stored.NextRunAt
	require.Nil(t, stored.CandyNextRunAt)
	companion, err := repo.ClaimNext(ctx, "expiring-candy-worker")
	require.NoError(t, err)
	require.Equal(t, "candy", companion.TestKind)
	_, err = db.ExecContext(ctx, `UPDATE intelligence_monitor_runs SET lease_until=NOW()-INTERVAL '1 minute' WHERE id=$1`, companion.ID)
	require.NoError(t, err)
	require.NoError(t, repo.ExpireRuns(ctx))
	stored, err = repo.GetPlan(ctx, plan.ID)
	require.NoError(t, err)
	require.NotNil(t, stored.NextRunAt)
	require.Equal(t, pelicanNext, *stored.NextRunAt)
	require.NotNil(t, stored.CandyNextRunAt)
	require.Equal(t, 180*time.Second, stored.CandyNextRunAt.Sub(*stored.CandyLastRunAt))
	detail, err := repo.GetRun(ctx, companion.ID)
	require.NoError(t, err)
	require.Equal(t, "failed", detail.Status)
	require.Nil(t, detail.Correct, "transport interruption is not an incorrect answer")
	require.Empty(t, detail.Answer)
	require.Empty(t, detail.RequestKeyEncrypted)
}

func TestIntelligenceCandyPostgresIndependentRetentionAndLightweightLists(t *testing.T) {
	db, ctx := intelligenceMonitorTestDB(t)
	repo := &intelligenceMonitorRepository{db: db}
	_, err := db.ExecContext(ctx, `ALTER TABLE accounts ADD COLUMN name TEXT NOT NULL DEFAULT '',ADD COLUMN IF NOT EXISTS deleted_at TIMESTAMPTZ;
ALTER TABLE upstream_targets ADD COLUMN name TEXT NOT NULL DEFAULT '',ADD COLUMN deleted_at TIMESTAMPTZ;
CREATE TABLE groups(id BIGINT PRIMARY KEY,name TEXT,deleted_at TIMESTAMPTZ)`)
	require.NoError(t, err)
	plan := candyRepositoryPlan(t, ctx, repo, "Retained pairs", false, true)
	hidden := candyRepositoryPlan(t, ctx, repo, "Candy switched off", false, false)
	for _, id := range []int64{plan.ID, hidden.ID} {
		for _, seed := range []struct {
			kind  string
			count int
		}{{"pelican", 25}, {"candy", 65}} {
			_, err = db.ExecContext(ctx, `INSERT INTO intelligence_monitor_runs(plan_id,plan_name,status,trigger,model,reasoning_effort,prompt,source_type,source_name,source_endpoint,api_mode,timeout_seconds,test_kind,correct,answer,html,raw_text,request_key_encrypted,created_at)
SELECT $1,'history',CASE WHEN i%2=0 THEN 'failed' ELSE 'succeeded' END,'manual','gpt-6-astra','high','fixture','external','source','','responses',600,$2::varchar,CASE WHEN $2::varchar='candy' THEN i%2=1 ELSE NULL END,'answer','<html>large art</html>','large raw','legacy-secret',NOW()-INTERVAL '1 day' FROM generate_series(1,$3) i`, id, seed.kind, seed.count)
			require.NoError(t, err)
		}
	}
	// Old active tasks survive both retention and the list's terminal cutoff.
	for _, kind := range []string{"pelican", "candy"} {
		require.NoError(t, repo.Enqueue(ctx, candyRepositoryRun(plan, kind), false))
	}
	_, err = db.ExecContext(ctx, `UPDATE intelligence_monitor_runs SET created_at=NOW()-INTERVAL '2 days' WHERE status='pending'`)
	require.NoError(t, err)
	require.NoError(t, repo.PruneRuns(ctx))
	for _, kind := range []string{"pelican", "candy"} {
		expected := 21
		if kind == "candy" {
			expected = 61
		}
		page, err := repo.ListRuns(ctx, service.IntelligenceMonitorRunQuery{PlanID: &plan.ID, TestKind: kind, Page: 1, PageSize: 100})
		require.NoError(t, err)
		require.Equal(t, int64(expected), page.Total)
		for _, run := range page.Items {
			require.Equal(t, kind, run.TestKind)
			require.Empty(t, run.HTML)
			require.Empty(t, run.RawText)
			require.Empty(t, run.RequestKeyEncrypted)
			if kind == "candy" {
				require.Empty(t, run.Prompt)
				require.Empty(t, run.SourceSnapshot)
				require.Empty(t, run.NotesSnapshot)
			} else {
				require.NotEmpty(t, run.Prompt, "artwork summaries preserve existing fields")
			}
		}
	}
	data, err := repo.LoadPlanListData(ctx, []int64{plan.ID, hidden.ID, plan.ID, 0})
	require.NoError(t, err)
	require.Len(t, data.Runs[plan.ID], 21)
	require.Len(t, data.CandyRuns[plan.ID], 61)
	require.Equal(t, "pending", data.CandyRuns[plan.ID][0].Status)
	require.Equal(t, "candy", data.CandyRuns[plan.ID][0].TestKind)
	require.Equal(t, "fixture", data.CandyRuns[plan.ID][0].RateSnapshot.Source)
	require.Equal(t, plan.Name, data.CandyRuns[plan.ID][0].SourceName)
	require.Empty(t, data.CandyRuns[plan.ID][0].Prompt)
	require.Empty(t, data.CandyRuns[plan.ID][0].SourceSnapshot)
	require.Empty(t, data.CandyRuns[plan.ID][0].NotesSnapshot)
	require.Equal(t, "private-schema-fixture", data.Runs[plan.ID][0].SourceSnapshot["source"])
	require.Equal(t, "private group", data.Runs[plan.ID][0].NotesSnapshot["group"])
	detail, err := repo.GetRun(ctx, data.CandyRuns[plan.ID][0].ID)
	require.NoError(t, err)
	require.Equal(t, service.IntelligenceMonitorCandyPrompt, detail.Prompt, "the detail endpoint retains the full test question")
	require.Equal(t, "private-schema-fixture", detail.SourceSnapshot["source"])
	require.Equal(t, "private group", detail.NotesSnapshot["group"])
	require.Equal(t, "fixture", detail.RateSnapshot.Source)
	require.Empty(t, data.CandyRuns[hidden.ID], "disabled plans do not load candy history")
	for _, runs := range []map[int64][]*service.IntelligenceMonitorRun{data.Runs, data.CandyRuns} {
		for _, history := range runs {
			for _, run := range history {
				require.Empty(t, run.HTML)
				require.Empty(t, run.RawText)
				require.Empty(t, run.RequestKeyEncrypted)
				if run.TestKind == "candy" {
					require.Empty(t, run.Prompt)
					require.Empty(t, run.SourceSnapshot)
					require.Empty(t, run.NotesSnapshot)
				}
			}
		}
	}
	// Completion and expiration each enforce the appropriate kind's cap.
	first, err := repo.ClaimNext(ctx, "retention-pelican")
	require.NoError(t, err)
	companion, err := repo.ClaimNext(ctx, "retention-candy")
	require.NoError(t, err)
	require.Nil(t, companion, "a plan never runs both kinds concurrently")
	_, err = db.ExecContext(ctx, `UPDATE intelligence_monitor_runs SET created_at=NOW(),lease_until=NOW()-INTERVAL '1 minute' WHERE id=$1`, first.ID)
	require.NoError(t, err)
	require.NoError(t, repo.ExpireRuns(ctx))
	page, err := repo.ListRuns(ctx, service.IntelligenceMonitorRunQuery{PlanID: &plan.ID, Page: 1, PageSize: 100})
	require.NoError(t, err)
	require.Equal(t, int64(20), page.Total)
	companion, err = repo.ClaimNext(ctx, "retention-candy")
	require.NoError(t, err)
	require.NotNil(t, companion)
	require.Equal(t, "candy", companion.TestKind)
	_, err = db.ExecContext(ctx, `UPDATE intelligence_monitor_runs SET created_at=NOW() WHERE id=$1`, companion.ID)
	require.NoError(t, err)
	correct := true
	companion.Status, companion.Correct, companion.Answer = "succeeded", &correct, "synthetic correct answer"
	require.NoError(t, repo.CompleteRun(ctx, companion))
	page, err = repo.ListRuns(ctx, service.IntelligenceMonitorRunQuery{PlanID: &plan.ID, TestKind: "candy", Page: 1, PageSize: 100})
	require.NoError(t, err)
	require.Equal(t, int64(60), page.Total)
	require.Equal(t, companion.ID, page.Items[0].ID)
	require.Equal(t, &correct, page.Items[0].Correct)
	require.NoError(t, repo.ArchivePlan(ctx, hidden.ID))
	_, err = db.ExecContext(ctx, `INSERT INTO intelligence_monitor_runs(plan_id,plan_name,status,trigger,model,reasoning_effort,prompt,source_type,source_name,source_endpoint,api_mode,timeout_seconds,test_kind)
SELECT $1,'archived','failed','manual','gpt-6-astra','high','fixture','external','source','','responses',600,'candy' FROM generate_series(1,3)`, hidden.ID)
	require.NoError(t, err)
	require.NoError(t, repo.PruneRuns(ctx))
	page, err = repo.ListRuns(ctx, service.IntelligenceMonitorRunQuery{PlanID: &hidden.ID, TestKind: "candy", Page: 1, PageSize: 100})
	require.NoError(t, err)
	require.Equal(t, int64(60), page.Total, "archived plans keep the same candy retention limit")
}
