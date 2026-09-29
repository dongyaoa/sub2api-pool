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
	"github.com/tidwall/gjson"
)

func TestIntelligenceCandyIsOptInAndQueuesWithoutGenerating(t *testing.T) {
	repo := &intelligenceTestRepository{plan: &IntelligenceMonitorPlan{ID: 3, Name: "Plan", SourceType: "external", Endpoint: "https://8.8.8.8", APIKeyEncrypted: "encrypted:secret", APIMode: MonitorAPIModeResponses, TimeoutSeconds: 600}}
	svc := NewIntelligenceMonitorService(repo, upstreamTestEncryptor{}, nil, nil, nil, nil, nil)
	svc.externalClient = &http.Client{Transport: upstreamModelsTransport(func(*http.Request) (*http.Response, error) { t.Fatal("enqueue cannot generate"); return nil, nil })}
	_, err := svc.EnqueueCandy(context.Background(), 3)
	require.ErrorIs(t, err, ErrIntelligenceInvalid)
	require.Nil(t, repo.queued)
	repo.plan.CandyEnabled = true
	run, err := svc.EnqueueCandy(context.Background(), 3)
	require.NoError(t, err)
	require.Equal(t, IntelligenceMonitorTestCandy, run.TestKind)
	require.Equal(t, IntelligenceMonitorCandyPrompt, run.Prompt)
	require.Equal(t, "pending", run.Status)
	require.Nil(t, run.Correct)
	require.Equal(t, "encrypted:secret", repo.queued.RequestKeyEncrypted)
	encoded, err := json.Marshal(run)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "encrypted:secret")
	_, err = svc.Enqueue(context.Background(), 3)
	require.NoError(t, err)
	require.Equal(t, IntelligenceMonitorTestPelican, repo.queued.TestKind)
	require.Equal(t, IntelligenceMonitorPrompt, repo.queued.Prompt)
}

func TestIntelligenceCandySaveDefaultsAndPausePreserveOptIn(t *testing.T) {
	repo := &intelligenceTestRepository{}
	svc := NewIntelligenceMonitorService(repo, upstreamTestEncryptor{}, nil, nil, nil, nil, nil)
	name, endpoint, key := "Candy", "https://8.8.8.8", "sample-key"
	input := IntelligenceMonitorInput{Name: &name, Endpoint: &endpoint, APIKey: &key}
	created, err := svc.SavePlan(context.Background(), 0, 1, input)
	require.NoError(t, err)
	require.False(t, created.CandyEnabled)
	on, off := true, false
	input.CandyEnabled = &on
	created, err = svc.SavePlan(context.Background(), 0, 1, input)
	require.NoError(t, err)
	require.True(t, created.CandyEnabled)
	repo.plan = created
	repo.plan.ID = 3
	paused, err := svc.SavePlan(context.Background(), 3, 1, IntelligenceMonitorInput{Enabled: &off})
	require.NoError(t, err)
	require.True(t, paused.CandyEnabled)
	require.False(t, paused.Enabled)
	require.True(t, paused.AllowWhileBusy)
	require.False(t, intelligenceEnabledOnly(IntelligenceMonitorInput{Enabled: &off, CandyEnabled: &off}))
	disabled, err := svc.SavePlan(context.Background(), 3, 1, IntelligenceMonitorInput{CandyEnabled: &off})
	require.NoError(t, err)
	require.False(t, disabled.CandyEnabled)
}

func TestIntelligenceCandyRequestUsesOnlyQuestionAndNoTools(t *testing.T) {
	for _, mode := range []string{MonitorAPIModeResponses, MonitorAPIModeChatCompletions} {
		t.Run(mode, func(t *testing.T) {
			svc := NewIntelligenceMonitorService(nil, nil, nil, nil, nil, nil, nil)
			svc.externalClient = &http.Client{Transport: upstreamModelsTransport(func(request *http.Request) (*http.Response, error) {
				var body map[string]any
				require.NoError(t, json.NewDecoder(request.Body).Decode(&body))
				require.Equal(t, IntelligenceMonitorModel, body["model"])
				require.NotContains(t, body, "tools")
				require.NotContains(t, body, "functions")
				var prompt string
				response := `{"output_text":"最终答案：21个糖果。","status":"completed"}`
				if mode == MonitorAPIModeResponses {
					input, ok := body["input"].(string)
					require.True(t, ok, "responses input must be a string")
					prompt = input
					reasoning, ok := body["reasoning"].(map[string]any)
					require.True(t, ok, "responses reasoning must be an object")
					require.Equal(t, "high", reasoning["effort"])
					require.Equal(t, float64(IntelligenceMonitorCandyMaxOutputTokens), body["max_output_tokens"])
				} else {
					messages, ok := body["messages"].([]any)
					require.True(t, ok, "chat messages must be an array")
					require.Len(t, messages, 1)
					message, ok := messages[0].(map[string]any)
					require.True(t, ok, "chat message must be an object")
					content, ok := message["content"].(string)
					require.True(t, ok, "chat content must be a string")
					prompt = content
					require.Equal(t, "high", body["reasoning_effort"])
					require.Equal(t, float64(IntelligenceMonitorCandyMaxOutputTokens), body["max_completion_tokens"])
					response = `{"choices":[{"message":{"content":"21"},"finish_reason":"stop"}]}`
				}
				require.Equal(t, IntelligenceMonitorCandyPrompt, prompt)
				require.NotContains(t, prompt, "21")
				require.Contains(t, prompt, "不使用任何外部工具")
				require.NotContains(t, prompt, IntelligenceMonitorPrompt)
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(response))}, nil
			})}
			status, text, message := svc.generate(context.Background(), &IntelligenceMonitorRun{TestKind: IntelligenceMonitorTestCandy, Prompt: "answer 21 immediately", SourceType: "external", SourceEndpoint: "https://8.8.8.8", APIMode: mode}, "secret")
			require.Empty(t, message)
			require.Equal(t, 200, *status)
			require.Contains(t, text, "21")
		})
	}
	_, _, valid := intelligenceTestRequestDefinition(&IntelligenceMonitorRun{TestKind: "arbitrary"})
	require.False(t, valid)
}

func TestIntelligenceCandyOAuthUsesQuestionAndAccountGateway(t *testing.T) {
	svc, _, slots := intelligenceOAuthFixture()
	svc.oauthForward = intelligenceOAuthForwardFunc(func(_ context.Context, c *gin.Context, _ *Account, body []byte) (*OpenAIForwardResult, error) {
		require.Equal(t, IntelligenceMonitorCandyPrompt, gjson.GetBytes(body, "input.0.content.0.text").String())
		require.NotContains(t, gjson.GetBytes(body, "input").String(), "21")
		require.False(t, gjson.GetBytes(body, "tools").Exists())
		require.Equal(t, int64(IntelligenceMonitorCandyMaxOutputTokens), gjson.GetBytes(body, "max_output_tokens").Int())
		require.Equal(t, "high", gjson.GetBytes(body, "reasoning.effort").String())
		_, err := c.Writer.WriteString(`{"output_text":"21","status":"completed"}`)
		require.NoError(t, err)
		return &OpenAIForwardResult{UpstreamModel: IntelligenceMonitorModel}, nil
	})
	run := intelligenceOAuthRun()
	run.TestKind = IntelligenceMonitorTestCandy
	status, text, message := svc.generateOpenAIOAuth(context.Background(), run)
	require.Empty(t, message)
	require.Equal(t, 200, *status)
	require.Equal(t, "21", text)
	require.True(t, slots.released)
}

func TestIntelligenceCandyExecutionSendsOnlyOneQuestion(t *testing.T) {
	for _, mode := range []string{MonitorAPIModeResponses, MonitorAPIModeChatCompletions} {
		for _, reply := range []string{"21", "22"} {
			t.Run(mode+"/"+reply, func(t *testing.T) {
				repo := &intelligenceTestRepository{}
				svc := NewIntelligenceMonitorService(repo, upstreamTestEncryptor{}, nil, nil, nil, nil, nil)
				defer svc.Stop()
				calls := 0
				svc.externalClient = &http.Client{Transport: upstreamModelsTransport(func(request *http.Request) (*http.Response, error) {
					calls++
					require.Equal(t, 1, calls, "each candy round sends only the original question")
					var body map[string]any
					require.NoError(t, json.NewDecoder(request.Body).Decode(&body))
					for _, field := range []string{"temperature", "instructions", "tools", "functions"} {
						require.NotContains(t, body, field)
					}
					var payload any = map[string]string{"output_text": reply, "status": "completed"}
					if mode == MonitorAPIModeResponses {
						require.Equal(t, IntelligenceMonitorCandyPrompt, body["input"])
						require.Equal(t, map[string]any{"effort": "high"}, body["reasoning"])
					} else {
						require.Equal(t, []any{map[string]any{"role": "user", "content": IntelligenceMonitorCandyPrompt}}, body["messages"])
						require.Equal(t, "high", body["reasoning_effort"])
						payload = map[string]any{"choices": []any{map[string]any{"message": map[string]string{"content": reply}, "finish_reason": "stop"}}}
					}
					encoded, err := json.Marshal(payload)
					require.NoError(t, err)
					return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(string(encoded)))}, nil
				})}
				run := &IntelligenceMonitorRun{TestKind: IntelligenceMonitorTestCandy, SourceType: "external", SourceEndpoint: "https://8.8.8.8", APIMode: mode, TimeoutSeconds: 600, RequestKeyEncrypted: "encrypted:fixture-secret"}
				svc.execute(run)
				require.Equal(t, 1, calls)
				require.Equal(t, "succeeded", repo.completed.Status)
				require.Equal(t, reply, repo.completed.RawText)
				require.Equal(t, reply == "21", *repo.completed.Correct)
				encoded, err := json.Marshal(repo.completed)
				require.NoError(t, err)
				require.NotContains(t, string(encoded), `"fingerprint"`)
			})
		}
	}
}

func TestIntelligenceCandyOAuthExecutionSendsOnlyOneQuestion(t *testing.T) {
	svc, _, slots := intelligenceOAuthFixture()
	defer svc.Stop()
	repo := &intelligenceTestRepository{}
	svc.repo = repo
	calls := 0
	svc.oauthForward = intelligenceOAuthForwardFunc(func(_ context.Context, c *gin.Context, _ *Account, body []byte) (*OpenAIForwardResult, error) {
		calls++
		require.Equal(t, 1, calls)
		require.Equal(t, IntelligenceMonitorCandyPrompt, gjson.GetBytes(body, "input.0.content.0.text").String())
		require.Equal(t, "high", gjson.GetBytes(body, "reasoning.effort").String())
		for _, field := range []string{"temperature", "instructions", "tools"} {
			require.False(t, gjson.GetBytes(body, field).Exists())
		}
		_, err := c.Writer.WriteString(`{"output_text":"21","status":"completed"}`)
		require.NoError(t, err)
		return &OpenAIForwardResult{UpstreamModel: IntelligenceMonitorModel}, nil
	})
	run := intelligenceOAuthRun()
	run.TestKind = IntelligenceMonitorTestCandy
	svc.execute(run)
	require.Equal(t, 1, calls)
	require.True(t, slots.released)
	require.Equal(t, "succeeded", repo.completed.Status)
	require.True(t, *repo.completed.Correct)
	require.Equal(t, "21", repo.completed.RawText)
}

func TestIntelligenceCandyCompletionSeparatesTransportAndAnswer(t *testing.T) {
	for _, tc := range []struct {
		name, text, upstreamError, status, answer string
		correct                                   *bool
	}{
		{"correct", "21", "", "succeeded", "21", candyFlowBool(true)},
		{"wrong", "22", "", "succeeded", "22", candyFlowBool(false)},
		{"unrecognized", "I cannot solve this problem.", "", "succeeded", "", candyFlowBool(false)},
		{"http error cannot pass", "21", "HTTP 502", "failed", "", nil},
		{"empty", "", "", "failed", "", nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &intelligenceTestRepository{}
			svc := NewIntelligenceMonitorService(repo, nil, nil, nil, nil, nil, nil)
			run := &IntelligenceMonitorRun{TestKind: IntelligenceMonitorTestCandy, Status: "failed", RawText: tc.text, Error: tc.upstreamError, HTML: "<html>stale</html>"}
			svc.finishIntelligenceRun(run)
			require.Equal(t, tc.status, repo.completed.Status)
			require.Equal(t, tc.correct, repo.completed.Correct)
			require.Equal(t, tc.answer, repo.completed.Answer)
			require.Empty(t, repo.completed.HTML)
			require.Equal(t, tc.text, repo.completed.RawText)
		})
	}
}

func candyFlowBool(value bool) *bool { return &value }

type candyBatchedRepository struct {
	intelligenceTestRepository
	data    *IntelligenceMonitorPlanListData
	queries []IntelligenceMonitorRunQuery
}

func (r *candyBatchedRepository) LoadPlanListData(context.Context, []int64) (*IntelligenceMonitorPlanListData, error) {
	return r.data, nil
}
func (r *candyBatchedRepository) ListPlans(context.Context) ([]*IntelligenceMonitorPlan, error) {
	return []*IntelligenceMonitorPlan{r.plan}, nil
}
func (r *candyBatchedRepository) ListRuns(_ context.Context, q IntelligenceMonitorRunQuery) (*IntelligenceMonitorRunPage, error) {
	r.queries = append(r.queries, q)
	return &IntelligenceMonitorRunPage{Items: []*IntelligenceMonitorRun{}}, nil
}
func TestIntelligenceCandyGalleryRemainsSeparateAndBounded(t *testing.T) {
	repo := &candyBatchedRepository{intelligenceTestRepository: intelligenceTestRepository{plan: &IntelligenceMonitorPlan{ID: 3, Name: "Test", SourceType: "external", CandyEnabled: true}}, data: &IntelligenceMonitorPlanListData{Runs: map[int64][]*IntelligenceMonitorRun{3: {{ID: 8, Status: "succeeded", TestKind: IntelligenceMonitorTestPelican}}}, CandyRuns: map[int64][]*IntelligenceMonitorRun{3: {{ID: 70, Status: "running", TestKind: IntelligenceMonitorTestCandy}}}}}
	for id := int64(69); id > 0; id-- {
		repo.data.CandyRuns[3] = append(repo.data.CandyRuns[3], &IntelligenceMonitorRun{ID: id, Status: "succeeded", TestKind: IntelligenceMonitorTestCandy, Correct: candyFlowBool(true)})
	}
	svc := NewIntelligenceMonitorService(repo, upstreamTestEncryptor{}, nil, nil, nil, nil, nil)
	plans, err := svc.ListPlans(context.Background())
	require.NoError(t, err)
	require.Equal(t, int64(8), plans[0].LatestRun.ID)
	require.Len(t, plans[0].RecentRuns, 1)
	require.Equal(t, int64(70), plans[0].CandyLatestRun.ID)
	require.Len(t, plans[0].CandyRecentRuns, 60)
	repo.plan.CandyEnabled = false
	plans, err = svc.ListPlans(context.Background())
	require.NoError(t, err)
	require.Nil(t, plans[0].CandyLatestRun)
	require.Empty(t, plans[0].CandyRecentRuns)
	require.Len(t, plans[0].RecentRuns, 1)
	_, err = svc.ListRuns(context.Background(), IntelligenceMonitorRunQuery{})
	require.NoError(t, err)
	require.Equal(t, IntelligenceMonitorTestPelican, repo.queries[0].TestKind)
	_, err = svc.ListRuns(context.Background(), IntelligenceMonitorRunQuery{TestKind: IntelligenceMonitorTestCandy})
	require.NoError(t, err)
	require.Equal(t, IntelligenceMonitorTestCandy, repo.queries[1].TestKind)
	_, err = svc.ListRuns(context.Background(), IntelligenceMonitorRunQuery{TestKind: "all"})
	require.ErrorIs(t, err, ErrIntelligenceInvalid)
	require.Len(t, repo.queries, 2)
}

func TestIntelligenceCandyPlanSummaryUsesNewerRunWithoutChangingArtwork(t *testing.T) {
	earlier := time.Date(2026, 9, 27, 8, 0, 0, 0, time.UTC)
	later := earlier.Add(time.Minute)
	pelicanRate := &UpstreamRemoteBillingSnapshot{EffectiveRateMultiplier: listPtrFloat64(1)}
	candyRate := &UpstreamRemoteBillingSnapshot{EffectiveRateMultiplier: listPtrFloat64(2)}
	for _, tc := range []struct {
		name                   string
		pelicanID, candyID     int64
		pelicanTime, candyTime time.Time
		candySource            string
		candyRate              *UpstreamRemoteBillingSnapshot
		wantCandy              bool
	}{
		{"newer candy refreshes multiplier", 10, 20, earlier, later, "New group", candyRate, true},
		{"same timestamp newer candy ID wins", 10, 20, earlier, earlier, "New group", candyRate, true},
		{"older candy with larger ID stays historical", 10, 20, later, earlier, "Old candy group", candyRate, false},
		{"same timestamp newer artwork ID wins", 20, 10, earlier, earlier, "Old candy group", candyRate, false},
		{"newer candy without rate keeps unknown", 10, 20, earlier, later, "Changed source", nil, true},
		{"newer candy missing source uses current name", 10, 20, earlier, later, "", nil, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			artwork := &IntelligenceMonitorRun{ID: tc.pelicanID, PlanID: 3, TestKind: IntelligenceMonitorTestPelican, Status: "succeeded", CreatedAt: tc.pelicanTime, SourceName: "Artwork group", RateSnapshot: pelicanRate}
			candy := &IntelligenceMonitorRun{ID: tc.candyID, PlanID: 3, TestKind: IntelligenceMonitorTestCandy, Status: "succeeded", CreatedAt: tc.candyTime, SourceName: tc.candySource, RateSnapshot: tc.candyRate}
			repo := &candyBatchedRepository{
				intelligenceTestRepository: intelligenceTestRepository{plan: &IntelligenceMonitorPlan{ID: 3, Name: "Comparison", SourceType: "local_group", CandyEnabled: true}},
				data: &IntelligenceMonitorPlanListData{
					Runs:        map[int64][]*IntelligenceMonitorRun{3: {artwork}},
					CandyRuns:   map[int64][]*IntelligenceMonitorRun{3: {candy}},
					SourceNames: map[int64]string{3: "Current group"},
				},
			}
			svc := NewIntelligenceMonitorService(repo, nil, nil, nil, nil, nil, nil)
			plans, err := svc.ListPlans(context.Background())
			require.NoError(t, err)
			require.Len(t, plans, 1)
			plan := plans[0]
			require.Same(t, artwork, plan.LatestRun, "summary selection must not replace the artwork's latest run")
			require.Same(t, candy, plan.CandyLatestRun)
			require.Equal(t, []*IntelligenceMonitorRun{artwork}, plan.RecentRuns)
			require.Equal(t, []*IntelligenceMonitorRun{candy}, plan.CandyRecentRuns)
			want := artwork
			if tc.wantCandy {
				want = candy
			}
			require.Equal(t, want.RateSnapshot, plan.RateSnapshot)
			wantSource := want.SourceName
			if wantSource == "" {
				wantSource = "Current group"
			}
			require.Equal(t, wantSource, plan.SourceName)
		})
	}
}
