//go:build unit

package service

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestIntelligenceModelsSaveDefaultsValidationAndSnapshot(t *testing.T) {
	for _, model := range []string{"", IntelligenceMonitorModel, IntelligenceMonitorSolModel, "arbitrary-model"} {
		t.Run(model, func(t *testing.T) {
			repo := &intelligenceTestRepository{}
			svc := NewIntelligenceMonitorService(repo, upstreamTestEncryptor{}, nil, nil, nil, nil, nil)
			name, endpoint, key := "Comparison", "https://8.8.8.8", "private-test-key"
			in := IntelligenceMonitorInput{Name: &name, Endpoint: &endpoint, APIKey: &key}
			if model != "" {
				in.Model = &model
			}
			plan, err := svc.SavePlan(context.Background(), 0, 1, in)
			if model == "arbitrary-model" {
				require.ErrorIs(t, err, ErrIntelligenceInvalid)
				require.Nil(t, repo.saved)
				return
			}
			require.NoError(t, err)
			require.Equal(t, intelligenceMonitorModel(model), plan.Model)
			require.Equal(t, plan.Model, repo.saved.Model)
			plan.ID, plan.CandyEnabled = 3, true
			repo.plan = plan
			for _, kind := range []string{IntelligenceMonitorTestPelican, IntelligenceMonitorTestCandy} {
				run, err := svc.enqueueTest(context.Background(), plan.ID, false, kind)
				require.NoError(t, err)
				require.Equal(t, plan.Model, run.Model)
				require.Equal(t, "high", run.ReasoningEffort)
			}
			paused := false
			_, err = svc.SavePlan(context.Background(), plan.ID, 1, IntelligenceMonitorInput{Enabled: &paused})
			require.NoError(t, err)
			require.Equal(t, plan.Model, repo.saved.Model, "schedule toggles preserve the selected model")
		})
	}
}

func TestIntelligenceModelsHTTPUsesSnapshotForPelicanCandyAndLocal(t *testing.T) {
	for _, source := range []string{"external", "local_group"} {
		for _, mode := range []string{MonitorAPIModeResponses, MonitorAPIModeChatCompletions} {
			for _, kind := range []string{IntelligenceMonitorTestPelican, IntelligenceMonitorTestCandy} {
				t.Run(source+"/"+mode+"/"+kind, func(t *testing.T) {
					svc := NewIntelligenceMonitorService(nil, nil, nil, nil, nil, nil, nil)
					called := false
					client := &http.Client{Transport: upstreamModelsTransport(func(req *http.Request) (*http.Response, error) {
						called = true
						var payload map[string]any
						require.NoError(t, json.NewDecoder(req.Body).Decode(&payload))
						require.Equal(t, IntelligenceMonitorSolModel, payload["model"])
						if mode == MonitorAPIModeResponses {
							require.Equal(t, "high", payload["reasoning"].(map[string]any)["effort"])
						} else {
							require.Equal(t, "high", payload["reasoning_effort"])
						}
						body := `{"output_text":"21","status":"completed"}`
						if mode == MonitorAPIModeChatCompletions {
							body = `{"choices":[{"message":{"content":"21"},"finish_reason":"stop"}]}`
						}
						return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
					})}
					svc.externalClient, svc.localClient = client, client
					ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
					defer cancel()
					run := &IntelligenceMonitorRun{Model: IntelligenceMonitorSolModel, SourceType: source, SourceEndpoint: "https://8.8.8.8", APIMode: mode, TestKind: kind, SourceSnapshot: map[string]any{"local_api_key_id": int64(8)}}
					status, _, message := svc.generate(ctx, run, "private-test-key")
					require.True(t, called)
					require.Equal(t, 200, *status)
					require.Empty(t, message)
				})
			}
		}
	}
}

func TestIntelligenceModelsOAuthChecksSelectedModelAndForwardsIt(t *testing.T) {
	for _, kind := range []string{IntelligenceMonitorTestPelican, IntelligenceMonitorTestCandy} {
		t.Run(kind, func(t *testing.T) {
			svc, accounts, _ := intelligenceOAuthFixture()
			accounts.account.Credentials["model_mapping"] = map[string]any{IntelligenceMonitorSolModel: IntelligenceMonitorSolModel}
			source, model := "openai_oauth", IntelligenceMonitorSolModel
			svc.repo = &intelligenceTestRepository{}
			plan, err := svc.SavePlan(context.Background(), 0, 1, IntelligenceMonitorInput{SourceType: &source, Model: &model, AccountID: json.RawMessage(`55`)})
			require.NoError(t, err, "Sol-only accounts must not be checked against Astra")
			require.Equal(t, model, plan.Model)
			svc.oauthForward = intelligenceOAuthForwardFunc(func(_ context.Context, c *gin.Context, _ *Account, body []byte) (*OpenAIForwardResult, error) {
				var payload map[string]any
				require.NoError(t, json.Unmarshal(body, &payload))
				require.Equal(t, model, payload["model"])
				require.Equal(t, "high", payload["reasoning"].(map[string]any)["effort"])
				c.Writer.Header().Set("Content-Type", "application/json")
				_, _ = c.Writer.Write([]byte(`{"status":"completed","output_text":"21"}`))
				return &OpenAIForwardResult{UpstreamModel: model}, nil
			})
			run := intelligenceOAuthRun()
			run.Model, run.TestKind = model, kind
			_, _, message := svc.generateOpenAIOAuth(context.Background(), run)
			require.Empty(t, message)
			require.Equal(t, model, run.SourceSnapshot["actual_model"])
			accounts.account.Credentials["model_mapping"] = map[string]any{model: IntelligenceMonitorModel}
			_, err = svc.SavePlan(context.Background(), 0, 1, IntelligenceMonitorInput{SourceType: &source, Model: &model, AccountID: json.RawMessage(`55`)})
			require.ErrorIs(t, err, ErrIntelligenceInvalid)
		})
	}
}

func TestIntelligenceModelsLocalGroupChecksSelectedAllowlist(t *testing.T) {
	svc, repo, keys := newLocalIntelligenceKeyTestService()
	groupRepo := svc.groups.(intelligenceLocalGroupRepo)
	groupRepo.group.ModelAllowlist = GroupModelAllowlist{Enabled: true, Models: []string{IntelligenceMonitorSolModel}}
	in := localIntelligenceInput()
	_, err := svc.SavePlan(context.Background(), 0, 7, in)
	require.ErrorIs(t, err, ErrIntelligenceInvalid)
	require.Nil(t, repo.saved)
	require.Empty(t, keys.created)
	model := IntelligenceMonitorSolModel
	in.Model = &model
	plan, err := svc.SavePlan(context.Background(), 0, 7, in)
	require.NoError(t, err)
	require.Equal(t, model, plan.Model)
}
