package repository

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/Wei-Shaw/sub2api/migrations"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

type intelligencePGEncryptor struct{}

type intelligencePGAccounts struct{ service.AccountRepository }

func (intelligencePGAccounts) GetByID(_ context.Context, id int64) (*service.Account, error) {
	return &service.Account{ID: id, Name: "OAuth database account", Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth, Status: service.StatusActive, Schedulable: true}, nil
}

func (intelligencePGEncryptor) Encrypt(plain string) (string, error) { return "cipher:" + plain, nil }
func (intelligencePGEncryptor) Decrypt(cipher string) (string, error) {
	return strings.TrimPrefix(cipher, "cipher:"), nil
}

// Uses a random private schema and a single connection. It never migrates or
// modifies public. Run with the same opt-in DSN as the upstream ledger test.
func intelligenceMonitorTestDB(t *testing.T, legacySchema ...bool) (*sql.DB, context.Context) {
	t.Helper()
	dsn := os.Getenv("UPSTREAM_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("UPSTREAM_TEST_DATABASE_URL is not configured")
	}
	db, err := sql.Open("postgres", dsn)
	require.NoError(t, err)
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = db.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	t.Cleanup(cancel)
	schema := "intelligence_monitor_test_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	_, err = db.ExecContext(ctx, `CREATE SCHEMA `+pqQuoteIdentifier(schema))
	require.NoError(t, err)
	t.Cleanup(func() {
		_, _ = db.ExecContext(context.Background(), `DROP SCHEMA `+pqQuoteIdentifier(schema)+` CASCADE`)
	})
	_, err = db.ExecContext(ctx, `SET search_path TO `+pqQuoteIdentifier(schema))
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `CREATE TABLE upstream_targets(id BIGINT PRIMARY KEY); CREATE TABLE accounts(id BIGINT PRIMARY KEY); CREATE TABLE api_keys(id BIGINT PRIMARY KEY,status TEXT NOT NULL,updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),deleted_at TIMESTAMPTZ)`)
	require.NoError(t, err)
	migration, err := migrations.FS.ReadFile("245_intelligence_monitor.sql")
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, string(migration))
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, string(migration))
	require.NoError(t, err, "migration must remain idempotent")
	migration, err = migrations.FS.ReadFile("246_intelligence_monitor_oauth.sql")
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, string(migration))
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, string(migration))
	require.NoError(t, err, "OAuth migration must remain idempotent")
	if len(legacySchema) == 0 || !legacySchema[0] {
		for _, name := range []string{"249_intelligence_monitor_interval_seconds.sql", "250_intelligence_monitor_generation_timeout.sql"} {
			migration, err = migrations.FS.ReadFile(name)
			require.NoError(t, err)
			_, err = db.ExecContext(ctx, string(migration))
			require.NoError(t, err)
		}
	}
	return db, ctx
}

func TestIntelligenceMonitorPostgresCRUDAndRuns(t *testing.T) {
	db, ctx := intelligenceMonitorTestDB(t)
	repo := &intelligenceMonitorRepository{db: db}
	svc := service.NewIntelligenceMonitorService(repo, intelligencePGEncryptor{}, nil, nil, nil, nil, nil)
	name, endpoint, key := "PostgreSQL CRUD", "https://8.8.8.8", "test-private-key-12345"
	plan, err := svc.SavePlan(ctx, 0, 7, service.IntelligenceMonitorInput{Name: &name, Endpoint: &endpoint, APIKey: &key})
	require.NoError(t, err)
	require.Positive(t, plan.ID)
	require.False(t, plan.Enabled)
	require.Nil(t, plan.NextRunAt)
	originalCreated := plan.CreatedAt
	blank, notes := "", "updated without replacing credential"
	plan, err = svc.SavePlan(ctx, plan.ID, 99, service.IntelligenceMonitorInput{APIKey: &blank, Notes: &notes})
	require.NoError(t, err, "real PostgreSQL rejects gaps in UPDATE placeholder numbering")
	require.Equal(t, notes, plan.Notes)
	require.Equal(t, int64(7), plan.CreatedBy)
	require.WithinDuration(t, originalCreated, plan.CreatedAt, time.Microsecond)
	stored, err := repo.GetPlan(ctx, plan.ID)
	require.NoError(t, err)
	require.Equal(t, "cipher:"+key, stored.APIKeyEncrypted)
	// Even a direct repository caller cannot rewrite created_by on update.
	stored.CreatedBy = 99
	stored.Name = "Renamed"
	require.NoError(t, repo.SavePlan(ctx, stored))
	stored, err = repo.GetPlan(ctx, stored.ID)
	require.NoError(t, err)
	require.Equal(t, int64(7), stored.CreatedBy)
	enabled := true
	plan, err = svc.SavePlan(ctx, plan.ID, 7, service.IntelligenceMonitorInput{Enabled: &enabled})
	require.NoError(t, err)
	require.NotNil(t, plan.NextRunAt)
	enabled = false
	plan, err = svc.SavePlan(ctx, plan.ID, 7, service.IntelligenceMonitorInput{Enabled: &enabled})
	require.NoError(t, err)
	require.Nil(t, plan.NextRunAt)
	run, err := svc.Enqueue(ctx, plan.ID)
	require.NoError(t, err)
	require.Equal(t, "pending", run.Status)
	_, err = svc.Enqueue(ctx, plan.ID)
	require.ErrorIs(t, err, service.ErrIntelligenceBusy)
	require.ErrorIs(t, repo.ArchivePlan(ctx, plan.ID), service.ErrIntelligenceBusy)
	claimed, err := repo.ClaimNext(ctx, "pg-worker")
	require.NoError(t, err)
	require.Equal(t, run.ID, claimed.ID)
	require.Equal(t, "running", claimed.Status)
	claimed.Status = "succeeded"
	claimed.HTML = "<!doctype html><html><svg></svg></html>"
	claimed.RawText = claimed.HTML
	require.NoError(t, repo.CompleteRun(ctx, claimed))
	detailed, err := repo.GetRun(ctx, run.ID)
	require.NoError(t, err)
	require.Equal(t, "succeeded", detailed.Status)
	require.Equal(t, claimed.HTML, detailed.HTML)
	require.Empty(t, detailed.RequestKeyEncrypted)
	require.NotNil(t, detailed.StartedAt)
	require.NotNil(t, detailed.FinishedAt)
	page, err := repo.ListRuns(ctx, service.IntelligenceMonitorRunQuery{PlanID: &plan.ID, Page: 1, PageSize: 20})
	require.NoError(t, err)
	require.Equal(t, int64(1), page.Total)
	require.Empty(t, page.Items[0].HTML)
	require.NoError(t, repo.ArchivePlan(ctx, plan.ID))
	_, err = repo.GetPlan(ctx, plan.ID)
	require.ErrorIs(t, err, service.ErrIntelligenceNotFound)
	detailed, err = repo.GetRun(ctx, run.ID)
	require.NoError(t, err)
	require.Equal(t, claimed.HTML, detailed.HTML, "archival must preserve generated history")
	// The dedicated-key revocation is in the same transaction as archival.
	_, err = db.ExecContext(ctx, `INSERT INTO api_keys(id,status)VALUES(44,'active')`)
	require.NoError(t, err)
	keyID, owner, groupID := int64(44), int64(7), int64(3)
	local := &service.IntelligenceMonitorPlan{Name: "Local", SourceType: "local_group", GroupID: &groupID, LocalAPIKeyID: &keyID, LocalKeyOwnerID: &owner, APIMode: "responses", IntervalSeconds: 3600, TimeoutSeconds: 300, CreatedBy: owner}
	require.NoError(t, repo.SavePlan(ctx, local))
	require.NoError(t, repo.ArchivePlan(ctx, local.ID))
	var status string
	require.NoError(t, db.QueryRowContext(ctx, `SELECT status FROM api_keys WHERE id=44`).Scan(&status))
	require.Equal(t, "disabled", status)
	// Exercise the new column through real INSERT, UPDATE, queued snapshots and
	// account deletion. No model request is made by creating a paused plan.
	_, err = db.ExecContext(ctx, `INSERT INTO accounts(id)VALUES(55),(56)`)
	require.NoError(t, err)
	svc.ConfigureOpenAIOAuth(intelligencePGAccounts{}, nil, nil)
	oauthSource, ignoredName := "openai_oauth", "must be ignored"
	oauth, err := svc.SavePlan(ctx, 0, owner, service.IntelligenceMonitorInput{SourceType: &oauthSource, AccountID: []byte(`55`), Name: &ignoredName})
	require.NoError(t, err)
	require.Equal(t, "OAuth database account", oauth.Name)
	require.Equal(t, int64(55), *oauth.AccountID)
	oauth, err = svc.SavePlan(ctx, oauth.ID, owner, service.IntelligenceMonitorInput{AccountID: []byte(`56`)})
	require.NoError(t, err)
	stored, err = repo.GetPlan(ctx, oauth.ID)
	require.NoError(t, err)
	require.Equal(t, int64(56), *stored.AccountID)
	oauthRun, err := svc.Enqueue(ctx, oauth.ID)
	require.NoError(t, err)
	require.Empty(t, oauthRun.RequestKeyEncrypted)
	claimed, err = repo.ClaimNext(ctx, "oauth-database-worker")
	require.NoError(t, err)
	require.Equal(t, oauthRun.ID, claimed.ID)
	require.True(t, claimed.OAuth)
	require.Equal(t, float64(56), claimed.SourceSnapshot["account_id"])
	claimed.Status = "failed"
	claimed.Error = "database fixture; no model request"
	require.NoError(t, repo.CompleteRun(ctx, claimed))
	_, err = db.ExecContext(ctx, `DELETE FROM accounts WHERE id=56`)
	require.NoError(t, err)
	stored, err = repo.GetPlan(ctx, oauth.ID)
	require.NoError(t, err)
	require.Nil(t, stored.AccountID)
	detailed, err = repo.GetRun(ctx, oauthRun.ID)
	require.NoError(t, err)
	require.Equal(t, float64(56), detailed.SourceSnapshot["account_id"])
}

func TestIntelligenceMonitorPostgresCustomSecondIntervals(t *testing.T) {
	db, ctx := intelligenceMonitorTestDB(t, true)
	repo := &intelligenceMonitorRepository{db: db}
	var existingID int64
	require.NoError(t, db.QueryRowContext(ctx, `INSERT INTO intelligence_monitor_plans(name,source_type,interval_seconds,created_by) VALUES('Existing seconds','external',3600,1) RETURNING id`).Scan(&existingID))
	migration, err := migrations.FS.ReadFile("249_intelligence_monitor_interval_seconds.sql")
	require.NoError(t, err)
	for range 2 {
		_, err = db.ExecContext(ctx, string(migration))
		require.NoError(t, err)
	}
	existing, err := repo.GetPlan(ctx, existingID)
	require.NoError(t, err)
	require.Equal(t, 3600, existing.IntervalSeconds, "migration must not rewrite existing schedules")
	require.Equal(t, 300, existing.TimeoutSeconds)
	var id int64
	var interval, timeout int
	require.NoError(t, db.QueryRowContext(ctx, `INSERT INTO intelligence_monitor_plans(name,source_type,created_by) VALUES('Default seconds','external',1) RETURNING id,interval_seconds,timeout_seconds`).Scan(&id, &interval, &timeout))
	require.Equal(t, 3600, interval)
	require.Equal(t, 300, timeout)
	for _, seconds := range []int{29, 86401} {
		_, err = db.ExecContext(ctx, `UPDATE intelligence_monitor_plans SET interval_seconds=$2 WHERE id=$1`, id, seconds)
		require.Error(t, err)
	}
	for _, seconds := range []int{30, 31, 97, 86400} {
		_, err = db.ExecContext(ctx, `UPDATE intelligence_monitor_plans SET interval_seconds=$2,enabled=TRUE,next_run_at=NOW() WHERE id=$1`, id, seconds)
		require.NoError(t, err)
		plan, err := repo.GetPlan(ctx, id)
		require.NoError(t, err)
		run := &service.IntelligenceMonitorRun{PlanID: id, PlanUpdatedAt: plan.UpdatedAt, PlanName: plan.Name, Trigger: "scheduled", Model: service.IntelligenceMonitorModel, ReasoningEffort: service.IntelligenceMonitorReasoning, Prompt: service.IntelligenceMonitorPrompt, SourceType: "external", SourceName: plan.Name, APIMode: "responses", TimeoutSeconds: 300}
		require.NoError(t, repo.Enqueue(ctx, run, true))
		duplicate := *run
		require.ErrorIs(t, repo.Enqueue(ctx, &duplicate, false), service.ErrIntelligenceBusy, "a short interval must not allow overlapping runs")
		claimed, err := repo.ClaimNext(ctx, "custom-seconds-worker")
		require.NoError(t, err)
		require.NotNil(t, claimed)
		require.Equal(t, run.ID, claimed.ID)
		claimed.Status = "failed"
		claimed.Error = "Synthetic repository regression; no generation requested"
		require.NoError(t, repo.CompleteRun(ctx, claimed))
		plan, err = repo.GetPlan(ctx, id)
		require.NoError(t, err)
		require.NotNil(t, plan.LastRunAt)
		require.NotNil(t, plan.NextRunAt)
		require.Equal(t, time.Duration(seconds)*time.Second, plan.NextRunAt.Sub(*plan.LastRunAt), "schedule uses exact seconds after completion")
	}
}

func TestIntelligenceMonitorPostgresRetainsOnlyTwentyTerminalRuns(t *testing.T) {
	db, ctx := intelligenceMonitorTestDB(t)
	repo := &intelligenceMonitorRepository{db: db}
	first := &service.IntelligenceMonitorPlan{Name: "first", SourceType: "external", APIMode: "responses", IntervalSeconds: 3600, TimeoutSeconds: 300, CreatedBy: 1}
	second := &service.IntelligenceMonitorPlan{Name: "second", SourceType: "external", APIMode: "responses", IntervalSeconds: 3600, TimeoutSeconds: 300, CreatedBy: 1}
	require.NoError(t, repo.SavePlan(ctx, first))
	require.NoError(t, repo.SavePlan(ctx, second))
	seed := func(planID int64, count int) {
		// Equal timestamps deliberately exercise the id tie-breaker. Alternating
		// success/failure proves errors consume the same bounded history budget.
		_, err := db.ExecContext(ctx, `INSERT INTO intelligence_monitor_runs(plan_id,plan_name,status,trigger,model,reasoning_effort,prompt,source_type,source_name,source_endpoint,api_mode,timeout_seconds,html,raw_text,created_at) SELECT $1,'fixture',CASE WHEN i%2=0 THEN 'failed' ELSE 'succeeded' END,'manual','gpt-6-astra','high','fixture','external','fixture','','responses',300,'<html>fixture</html>','fixture',NOW()-INTERVAL '1 day' FROM generate_series(1,$2) i`, planID, count)
		require.NoError(t, err)
	}
	seed(first.ID, 25)
	seed(second.ID, 22)
	queue := func(plan *service.IntelligenceMonitorPlan) *service.IntelligenceMonitorRun {
		run := &service.IntelligenceMonitorRun{PlanID: plan.ID, PlanUpdatedAt: plan.UpdatedAt, PlanName: plan.Name, SourceType: plan.SourceType, Trigger: "manual", Model: service.IntelligenceMonitorModel, ReasoningEffort: service.IntelligenceMonitorReasoning, Prompt: service.IntelligenceMonitorPrompt, APIMode: "responses", TimeoutSeconds: 300, SourceSnapshot: map[string]any{}, NotesSnapshot: map[string]string{}}
		require.NoError(t, repo.Enqueue(ctx, run, false))
		return run
	}
	firstActive := queue(first)
	claimed, err := repo.ClaimNext(ctx, "retention-worker")
	require.NoError(t, err)
	require.Equal(t, firstActive.ID, claimed.ID)
	secondActive := queue(second)
	// Age both active rows beyond historical works. Retention must keep them.
	_, err = db.ExecContext(ctx, `UPDATE intelligence_monitor_runs SET created_at=NOW()-INTERVAL '2 days' WHERE status IN ('pending','running')`)
	require.NoError(t, err)
	require.NoError(t, repo.PruneRuns(ctx))
	assertIDs := func(planID int64, terminalCount, totalCount int) []int64 {
		page, err := repo.ListRuns(ctx, service.IntelligenceMonitorRunQuery{PlanID: &planID, Page: 1, PageSize: 100})
		require.NoError(t, err)
		require.Equal(t, int64(totalCount), page.Total)
		ids := []int64{}
		for _, run := range page.Items {
			if run.Status == "succeeded" || run.Status == "failed" {
				ids = append(ids, run.ID)
			}
		}
		require.Len(t, ids, terminalCount)
		return ids
	}
	require.Equal(t, int64(6), assertIDs(first.ID, 20, 21)[19])
	require.Equal(t, int64(28), assertIDs(second.ID, 20, 21)[19])
	for _, id := range []int64{firstActive.ID, secondActive.ID} {
		_, err = repo.GetRun(ctx, id)
		require.NoError(t, err)
	}
	_, err = repo.GetRun(ctx, 1)
	require.ErrorIs(t, err, service.ErrIntelligenceNotFound, "old HTML and raw text are physically deleted")
	// Completing the newest run prunes once more inside the same transaction.
	_, err = db.ExecContext(ctx, `UPDATE intelligence_monitor_runs SET created_at=NOW() WHERE id=$1`, claimed.ID)
	require.NoError(t, err)
	claimed.Status, claimed.HTML = "succeeded", "<html>new</html>"
	require.NoError(t, repo.CompleteRun(ctx, claimed))
	require.Equal(t, claimed.ID, assertIDs(first.ID, 20, 20)[0])
	// Expiration also creates a terminal artifact and must apply retention.
	claimed, err = repo.ClaimNext(ctx, "expired-worker")
	require.NoError(t, err)
	require.Equal(t, secondActive.ID, claimed.ID)
	_, err = db.ExecContext(ctx, `UPDATE intelligence_monitor_runs SET created_at=NOW(),lease_until=NOW()-INTERVAL '1 minute' WHERE id=$1`, claimed.ID)
	require.NoError(t, err)
	require.NoError(t, repo.ExpireRuns(ctx))
	require.Equal(t, claimed.ID, assertIDs(second.ID, 20, 20)[0])
	require.NoError(t, repo.ArchivePlan(ctx, second.ID))
	seed(second.ID, 3)
	require.NoError(t, repo.PruneRuns(ctx))
	assertIDs(second.ID, 20, 20)
}

func pqQuoteIdentifier(value string) string { return `"` + strings.ReplaceAll(value, `"`, `""`) + `"` }
