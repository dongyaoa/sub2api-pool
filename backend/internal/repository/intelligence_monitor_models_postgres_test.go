package repository

import (
	"context"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/Wei-Shaw/sub2api/migrations"
	"github.com/stretchr/testify/require"
)

func TestIntelligenceModelsPostgresPersistenceAndMembership(t *testing.T) {
	for _, source := range []string{"upstream", "openai_oauth", "local_group"} {
		t.Run(source, func(t *testing.T) {
			db, ctx := intelligenceMonitorTestDB(t)
			repo := &intelligenceMonitorRepository{db: db}
			_, err := db.ExecContext(ctx, `INSERT INTO upstream_targets(id) VALUES(10); INSERT INTO accounts(id) VALUES(10)`)
			require.NoError(t, err)
			newPlan := func(model string) *service.IntelligenceMonitorPlan {
				id := int64(10)
				plan := &service.IntelligenceMonitorPlan{Name: source + model, SourceType: source, Model: model, APIMode: "responses", IntervalSeconds: 300, TimeoutSeconds: 600, CreatedBy: 7}
				switch source {
				case "upstream":
					plan.UpstreamTargetID = &id
				case "openai_oauth":
					plan.AccountID = &id
				case "local_group":
					plan.GroupID = &id
				}
				return plan
			}
			astra, sol := newPlan(""), newPlan(service.IntelligenceMonitorSolModel)
			require.NoError(t, repo.SavePlan(ctx, astra))
			require.Equal(t, service.IntelligenceMonitorModel, astra.Model)
			require.NoError(t, repo.SavePlan(ctx, sol), "one source may have separate Astra and Sol plans")
			assertDuplicate := func(p *service.IntelligenceMonitorPlan) {
				t.Helper()
				err := repo.SavePlan(ctx, p)
				switch source {
				case "upstream":
					require.ErrorIs(t, err, service.ErrIntelligenceUpstreamPlanExists)
				case "openai_oauth":
					require.ErrorIs(t, err, service.ErrIntelligenceOAuthPlanExists)
				case "local_group":
					require.ErrorIs(t, err, service.ErrIntelligenceLocalPlanExists)
				}
			}
			assertDuplicate(newPlan(service.IntelligenceMonitorSolModel))
			stored, err := repo.GetPlan(ctx, sol.ID)
			require.NoError(t, err)
			require.Equal(t, sol.Model, stored.Model)
			stored.Notes = "updated"
			require.NoError(t, repo.SavePlan(ctx, stored), "ordinary edits preserve model membership")
			stored.Model = service.IntelligenceMonitorModel
			assertDuplicate(stored)
			require.NoError(t, repo.ArchivePlan(ctx, astra.ID))
			require.NoError(t, repo.SavePlan(ctx, stored), "model changes may enter an unoccupied model")
			stored, err = repo.GetPlan(ctx, stored.ID)
			require.NoError(t, err)
			require.Equal(t, service.IntelligenceMonitorModel, stored.Model)
			stored.Model = "unsupported"
			require.ErrorIs(t, repo.SavePlan(ctx, stored), service.ErrIntelligenceInvalid)
		})
	}
}

func TestIntelligenceModelsMigrationPreservesLegacyPlansAndRunSnapshots(t *testing.T) {
	db, ctx := intelligenceMonitorTestDB(t)
	_, err := db.ExecContext(ctx, `ALTER TABLE intelligence_monitor_plans DROP COLUMN model CASCADE;
INSERT INTO intelligence_monitor_plans(id,name,source_type,created_by) VALUES(50,'Legacy','external',7);
INSERT INTO intelligence_monitor_runs(plan_id,plan_name,status,trigger,model,reasoning_effort,prompt,source_type,source_name,source_endpoint,api_mode,timeout_seconds)
 VALUES(50,'Legacy','succeeded','manual','gpt-6-astra','high','original','external','Legacy','','responses',600)`)
	require.NoError(t, err)
	data, err := migrations.FS.ReadFile("262_intelligence_monitor_models.sql")
	require.NoError(t, err)
	for range 2 {
		_, err = db.ExecContext(ctx, string(data))
		require.NoError(t, err, "model migration remains idempotent")
	}
	var planModel, runModel, prompt string
	require.NoError(t, db.QueryRowContext(ctx, `SELECT p.model,r.model,r.prompt FROM intelligence_monitor_plans p JOIN intelligence_monitor_runs r ON r.plan_id=p.id WHERE p.id=50`).Scan(&planModel, &runModel, &prompt))
	require.Equal(t, service.IntelligenceMonitorModel, planModel)
	require.Equal(t, service.IntelligenceMonitorModel, runModel)
	require.Equal(t, "original", prompt)
	_, err = db.ExecContext(context.Background(), `UPDATE intelligence_monitor_plans SET model='invalid' WHERE id=50`)
	require.Error(t, err, "database also enforces the supported model set")
}
