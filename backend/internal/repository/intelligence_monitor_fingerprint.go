package repository

import (
	"context"
	"encoding/json"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

// Expiry cannot collect more evidence. Preserve partial samples and separately
// graded candy answers, but never leave a terminal run looking active in polls.
// Empty historical fingerprints remain empty rather than becoming a new test.
const intelligenceFingerprintExpiredAssignmentsSQL = `fingerprint=CASE WHEN test_kind='candy' AND jsonb_typeof(fingerprint)='object' AND fingerprint<>'{}'::jsonb THEN fingerprint || '{"status":"timeout","passed":false,"error":"Fingerprint collection interrupted or monitoring lease expired"}'::jsonb ELSE fingerprint END,
fingerprint_detail=CASE WHEN test_kind='candy' AND jsonb_typeof(fingerprint_detail)='object' AND fingerprint_detail<>'{}'::jsonb THEN fingerprint_detail || '{"status":"timeout","passed":false,"error":"Fingerprint collection interrupted or monitoring lease expired"}'::jsonb ELSE fingerprint_detail END`

func intelligenceFingerprintForCompletion(run *service.IntelligenceMonitorRun) *service.IntelligenceFingerprintResult {
	result := run.Fingerprint
	if result == nil {
		return nil
	}
	switch result.Status {
	case "pending", "running", "collecting", "comparing":
		// CompleteRun only receives terminal work. A cancellation or unexpected
		// interruption may bypass the collector's normal final status update.
		terminal := *result
		passed := false
		terminal.Status, terminal.Passed = "failed", &passed
		terminal.Error = "Fingerprint collection interrupted before comparison completed"
		return &terminal
	default:
		return result
	}
}

func intelligenceFingerprintJSON(result *service.IntelligenceFingerprintResult) ([]byte, []byte, error) {
	if result == nil {
		return []byte(`{}`), []byte(`{}`), nil
	}
	summary, err := json.Marshal(result.Summary())
	if err != nil {
		return nil, nil, err
	}
	detail, err := json.Marshal(result)
	return summary, detail, err
}

func (r *intelligenceMonitorRepository) SaveCandyProgress(ctx context.Context, run *service.IntelligenceMonitorRun) error {
	if run.TestKind != service.IntelligenceMonitorTestCandy || run.LeaseToken == "" {
		return service.ErrIntelligenceInvalid
	}
	summary, detail, err := intelligenceFingerprintJSON(run.Fingerprint)
	if err != nil {
		return err
	}
	result, err := r.db.ExecContext(ctx, `UPDATE intelligence_monitor_runs SET correct=$3,answer=$4,fingerprint=$5::jsonb,fingerprint_detail=$6::jsonb WHERE id=$1 AND lease_token=$2 AND status='running' AND test_kind='candy'`, run.ID, run.LeaseToken, run.Correct, run.Answer, string(summary), string(detail))
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count != 1 {
		return service.ErrIntelligenceNotFound
	}
	return nil
}
