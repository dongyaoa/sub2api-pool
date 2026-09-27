package repository

import (
	"context"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

func (r *intelligenceMonitorRepository) RegradeCandyRuns(ctx context.Context, limit, version int, grade func(string) (string, bool)) (int, error) {
	if limit < 1 || limit > 64 || version < 1 || grade == nil {
		return 0, service.ErrIntelligenceInvalid
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()
	// Responses over the parser's input bound remain unidentified. Do not
	// materialize a batch of potentially multi-megabyte raw outputs in memory.
	rows, err := tx.QueryContext(ctx, `SELECT id,CASE WHEN octet_length(raw_text)<=$3 THEN raw_text ELSE ''::text END FROM intelligence_monitor_runs WHERE test_kind='candy' AND status='succeeded' AND error='' AND candy_grade_version<$1 ORDER BY candy_grade_version,id FOR UPDATE SKIP LOCKED LIMIT $2`, version, limit, service.IntelligenceMonitorCandyMaxGradeBytes)
	if err != nil {
		return 0, err
	}
	type response struct {
		id   int64
		text string
	}
	responses := make([]response, 0, limit)
	for rows.Next() {
		var item response
		if err = rows.Scan(&item.id, &item.text); err != nil {
			_ = rows.Close()
			return 0, err
		}
		responses = append(responses, item)
	}
	err = rows.Err()
	_ = rows.Close()
	if err != nil {
		return 0, err
	}
	for _, item := range responses {
		answer, correct := grade(item.text)
		if _, err = tx.ExecContext(ctx, `UPDATE intelligence_monitor_runs SET answer=$2,correct=$3,candy_grade_version=$4 WHERE id=$1`, item.id, answer, correct, version); err != nil {
			return 0, err
		}
	}
	if err = tx.Commit(); err != nil {
		return 0, err
	}
	return len(responses), nil
}
