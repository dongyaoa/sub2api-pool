package repository

import (
	"context"
	"encoding/json"
	"math"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func intelligenceRepositoryFingerprint() *service.IntelligenceFingerprintResult {
	distance, probability, passed, status := .1, .9, true, 200
	return &service.IntelligenceFingerprintResult{
		Method: "choice4-v1", Mode: "quick", Status: "passed", Passed: &passed,
		Model: service.IntelligenceMonitorModel, ReasoningEffort: "high", Total: 8, Done: 8, Valid: 8, DurationMS: 1250,
		Samples: []service.IntelligenceFingerprintSample{{Probe: "probe-1", Answer: "A", Category: "valid", HTTPStatus: &status}},
		Attribution: &service.IntelligenceFingerprintAttribution{
			Status: "consistent", Message: "matches declared model", Nearest: service.IntelligenceMonitorModel, Alpha: .01,
			Warnings: []string{"retained in detail only"},
			Comparisons: []service.IntelligenceFingerprintComparison{
				{Model: service.IntelligenceMonitorModel, MeanJSD: &distance, PValue: &probability, Verdict: "near", Cells: []service.IntelligenceFingerprintCellComparison{{Cell: "probe-1", JSD: distance, ValidA: 8, ValidB: 8}}},
				{Model: "other-model", MeanJSD: &distance, PValue: &probability, Verdict: "near", Cells: []service.IntelligenceFingerprintCellComparison{{Cell: "probe-1", JSD: distance, ValidA: 8, ValidB: 8}}},
			},
		},
	}
}

func TestIntelligenceFingerprintRepositoryJSONSeparatesSummaryAndEvidence(t *testing.T) {
	full := intelligenceRepositoryFingerprint()
	summaryJSON, detailJSON, err := intelligenceFingerprintJSON(full)
	require.NoError(t, err)
	var summary, detail service.IntelligenceFingerprintResult
	require.NoError(t, json.Unmarshal(summaryJSON, &summary))
	require.NoError(t, json.Unmarshal(detailJSON, &detail))
	require.Equal(t, full, &detail)
	require.Equal(t, full.Passed, summary.Passed)
	require.Empty(t, summary.Samples)
	require.Empty(t, summary.Attribution.Warnings)
	require.Len(t, summary.Attribution.Comparisons, 1)
	require.Empty(t, summary.Attribution.Comparisons[0].Cells)
	require.Len(t, full.Attribution.Comparisons, 2, "summary serialization must not mutate full evidence")
	require.Len(t, full.Attribution.Comparisons[0].Cells, 1)
	require.Less(t, len(summaryJSON), len(detailJSON))
	summaryJSON, detailJSON, err = intelligenceFingerprintJSON(nil)
	require.NoError(t, err)
	require.JSONEq(t, `{}`, string(summaryJSON))
	require.JSONEq(t, `{}`, string(detailJSON))
}

func TestIntelligenceFingerprintRepositoryRejectsInvalidProgressAndJSON(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	repo := &intelligenceMonitorRepository{db: db}
	for _, run := range []*service.IntelligenceMonitorRun{
		{ID: 1, TestKind: "pelican", LeaseToken: "lease"},
		{ID: 1, TestKind: "candy"},
	} {
		require.ErrorIs(t, repo.SaveCandyProgress(context.Background(), run), service.ErrIntelligenceInvalid)
	}
	nonfinite := math.NaN()
	result := intelligenceRepositoryFingerprint()
	result.Attribution.Comparisons[0].MeanJSD = &nonfinite
	run := &service.IntelligenceMonitorRun{ID: 1, TestKind: "candy", LeaseToken: "lease", Fingerprint: result}
	require.Error(t, repo.SaveCandyProgress(context.Background(), run))
	require.Error(t, repo.CompleteRun(context.Background(), run))
	require.NoError(t, mock.ExpectationsWereMet(), "invalid progress must not open a database transaction")
}
