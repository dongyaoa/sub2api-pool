package repository

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIntelligenceMonitorLoadPlanListDataPostgresKeepsLiveRunAndBoundsGallery(t *testing.T) {
	db, ctx := intelligenceMonitorTestDB(t)
	// The shared CRUD fixture intentionally keeps these source tables minimal;
	// add only the columns needed by the batched display-name query.
	_, err := db.ExecContext(ctx, `ALTER TABLE accounts ADD COLUMN name VARCHAR(100) NOT NULL DEFAULT '', ADD COLUMN deleted_at TIMESTAMPTZ;
ALTER TABLE upstream_targets ADD COLUMN name VARCHAR(100) NOT NULL DEFAULT '', ADD COLUMN deleted_at TIMESTAMPTZ;
CREATE TABLE groups(id BIGINT PRIMARY KEY,name VARCHAR(100) NOT NULL,deleted_at TIMESTAMPTZ)`)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `INSERT INTO accounts(id,name) VALUES(1,'OAuth original');
UPDATE accounts SET name='OAuth renamed' WHERE id=1;
INSERT INTO intelligence_monitor_plans(id,name,source_type,account_id,created_by) VALUES
(501,'OAuth stored name','openai_oauth',1,1),(502,'External gallery','external',NULL,1);
INSERT INTO intelligence_monitor_runs(plan_id,plan_name,status,trigger,model,reasoning_effort,prompt,source_type,source_name,source_endpoint,api_mode,timeout_seconds,source_snapshot,notes_snapshot)
VALUES (501,'OAuth stored name','running','manual','gpt-6-astra','high','draw','openai_oauth','OAuth stored name','','responses',600,'{}','{}');
INSERT INTO intelligence_monitor_runs(plan_id,plan_name,status,trigger,model,reasoning_effort,prompt,source_type,source_name,source_endpoint,api_mode,timeout_seconds,source_snapshot,notes_snapshot)
SELECT 502,'External gallery','succeeded','manual','gpt-6-astra','high','draw','external','External gallery','https://example.test','responses',600,'{}','{}' FROM generate_series(1,25);
INSERT INTO intelligence_monitor_runs(plan_id,plan_name,status,trigger,model,reasoning_effort,prompt,source_type,source_name,source_endpoint,api_mode,timeout_seconds,source_snapshot,notes_snapshot)
VALUES (502,'External gallery','running','scheduled','gpt-6-astra','high','draw','external','External gallery','https://example.test','responses',600,'{}','{}')`)
	require.NoError(t, err)
	repo := &intelligenceMonitorRepository{db: db}
	data, err := repo.LoadPlanListData(ctx, []int64{501, 502})
	require.NoError(t, err)
	require.Equal(t, "OAuth renamed", data.SourceNames[501], "OAuth cards use current account name")
	require.Len(t, data.Runs[501], 1)
	require.Equal(t, "running", data.Runs[501][0].Status)
	require.Len(t, data.Runs[502], 21, "one live run plus the twenty newest terminal works")
	require.Equal(t, "running", data.Runs[502][0].Status)
	for _, run := range data.Runs[502][1:] {
		require.Equal(t, "succeeded", run.Status)
	}
}
