package repository

import (
	"encoding/json"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestIntelligenceCandyPostgresIgnoresLegacyComparisonEvidence(t *testing.T) {
	db, ctx := intelligenceMonitorTestDB(t)
	repo := &intelligenceMonitorRepository{db: db}
	for _, tc := range []struct {
		name, summary, detail string
	}{
		{"old mismatch", `{"status":"completed","passed":false}`, `{"status":"completed","passed":false,"samples":[{"answer":"B"}]}`},
		{"unexpected legacy shape", `"legacy summary"`, `["legacy detail"]`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			plan := candyRepositoryPlan(t, ctx, repo, tc.name, false, true)
			queued := candyRepositoryRun(plan, "candy")
			require.NoError(t, repo.Enqueue(ctx, queued, false))
			_, err := db.ExecContext(ctx, `UPDATE intelligence_monitor_runs SET fingerprint=$2::jsonb,fingerprint_detail=$3::jsonb WHERE id=$1`, queued.ID, tc.summary, tc.detail)
			require.NoError(t, err)
			run, err := repo.ClaimNextForKind(ctx, "candy-only-worker", "candy", 4)
			require.NoError(t, err, "claiming must not parse retired comparison fields")
			require.NotNil(t, run)
			correct, status := true, 200
			run.Status, run.Correct, run.Answer, run.RawText, run.HTTPStatus = "succeeded", &correct, "21", `\boxed{21}`, &status
			require.NoError(t, repo.CompleteRun(ctx, run))
			detail, err := repo.GetRun(ctx, run.ID)
			require.NoError(t, err, "old comparison data must not prevent reading candy replies")
			require.Equal(t, "succeeded", detail.Status)
			require.NotNil(t, detail.Correct)
			require.True(t, *detail.Correct, "an old comparison mismatch must not override the candy result")
			require.Equal(t, "21", detail.Answer)
			require.Equal(t, run.RawText, detail.RawText)
			page, err := repo.ListRuns(ctx, service.IntelligenceMonitorRunQuery{PlanID: &plan.ID, TestKind: "candy", Page: 1, PageSize: 60})
			require.NoError(t, err)
			require.Len(t, page.Items, 1)
			require.Equal(t, detail.Correct, page.Items[0].Correct)
			batched := map[int64][]*service.IntelligenceMonitorRun{}
			require.NoError(t, repo.loadPlanKindRuns(ctx, []int64{plan.ID}, "candy", 60, batched))
			require.Len(t, batched[plan.ID], 1)
			require.Equal(t, detail.Correct, batched[plan.ID][0].Correct)
			for _, item := range []*service.IntelligenceMonitorRun{detail, page.Items[0], batched[plan.ID][0]} {
				encoded, err := json.Marshal(item)
				require.NoError(t, err)
				var payload map[string]any
				require.NoError(t, json.Unmarshal(encoded, &payload))
				require.NotContains(t, payload, "fingerprint")
				require.NotContains(t, payload, "fingerprint_detail")
			}
			var storedSummary, storedDetail string
			var gradeVersion int
			require.NoError(t, db.QueryRowContext(ctx, `SELECT fingerprint::text,fingerprint_detail::text,candy_grade_version FROM intelligence_monitor_runs WHERE id=$1`, run.ID).Scan(&storedSummary, &storedDetail, &gradeVersion))
			require.JSONEq(t, tc.summary, storedSummary, "retired data is preserved without being read or updated by monitoring")
			require.JSONEq(t, tc.detail, storedDetail)
			require.Equal(t, service.IntelligenceMonitorCandyGradeVersion, gradeVersion)
		})
	}
}

func TestIntelligenceCandyPostgresExpiryPreservesPreviouslyStoredAnswer(t *testing.T) {
	db, ctx := intelligenceMonitorTestDB(t)
	repo := &intelligenceMonitorRepository{db: db}
	plan := candyRepositoryPlan(t, ctx, repo, "Existing graded request", false, true)
	require.NoError(t, repo.Enqueue(ctx, candyRepositoryRun(plan, "candy"), false))
	run, err := repo.ClaimNextForKind(ctx, "old-candy-worker", "candy", 4)
	require.NoError(t, err)
	require.NotNil(t, run)
	const legacySummary = `{"status":"collecting","passed":null}`
	const legacyDetail = `{"status":"collecting","samples":[{"answer":"A"}]}`
	_, err = db.ExecContext(ctx, `UPDATE intelligence_monitor_runs SET correct=TRUE,answer='21',fingerprint=$2::jsonb,fingerprint_detail=$3::jsonb,lease_until=NOW()-INTERVAL '1 minute' WHERE id=$1`, run.ID, legacySummary, legacyDetail)
	require.NoError(t, err)
	require.NoError(t, repo.ExpireRuns(ctx))
	detail, err := repo.GetRun(ctx, run.ID)
	require.NoError(t, err)
	require.Equal(t, "failed", detail.Status)
	require.NotNil(t, detail.Correct)
	require.True(t, *detail.Correct, "lease expiry must not erase a previously graded answer")
	require.Equal(t, "21", detail.Answer)
	require.Empty(t, detail.RequestKeyEncrypted)
	require.Empty(t, detail.LeaseToken)
	var storedSummary, storedDetail string
	require.NoError(t, db.QueryRowContext(ctx, `SELECT fingerprint::text,fingerprint_detail::text FROM intelligence_monitor_runs WHERE id=$1`, run.ID).Scan(&storedSummary, &storedDetail))
	require.JSONEq(t, legacySummary, storedSummary)
	require.JSONEq(t, legacyDetail, storedDetail)
}
