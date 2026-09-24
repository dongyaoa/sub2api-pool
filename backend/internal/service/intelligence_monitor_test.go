//go:build unit

package service

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type intelligenceTestRepository struct {
	IntelligenceMonitorRepository
	plan      *IntelligenceMonitorPlan
	saved     *IntelligenceMonitorPlan
	queued    *IntelligenceMonitorRun
	completed *IntelligenceMonitorRun
	archived  bool
}

func (r *intelligenceTestRepository) ArchivePlan(context.Context, int64) error {
	r.archived = true
	return nil
}

func (r *intelligenceTestRepository) GetPlan(context.Context, int64) (*IntelligenceMonitorPlan, error) {
	copy := *r.plan
	return &copy, nil
}
func (r *intelligenceTestRepository) SavePlan(_ context.Context, p *IntelligenceMonitorPlan) error {
	copy := *p
	r.saved = &copy
	return nil
}
func (r *intelligenceTestRepository) Enqueue(_ context.Context, run *IntelligenceMonitorRun, _ bool) error {
	run.ID = 11
	run.Status = "pending"
	copy := *run
	r.queued = &copy
	return nil
}
func (r *intelligenceTestRepository) CompleteRun(_ context.Context, run *IntelligenceMonitorRun) error {
	copy := *run
	r.completed = &copy
	return nil
}

type intelligenceGalleryRepository struct {
	intelligenceTestRepository
	runs []*IntelligenceMonitorRun
}

func (r *intelligenceGalleryRepository) ListPlans(context.Context) ([]*IntelligenceMonitorPlan, error) {
	return []*IntelligenceMonitorPlan{r.plan}, nil
}
func (r *intelligenceGalleryRepository) ListRuns(_ context.Context, q IntelligenceMonitorRunQuery) (*IntelligenceMonitorRunPage, error) {
	runs := r.runs
	if len(runs) > q.PageSize {
		runs = runs[:q.PageSize]
	}
	return &IntelligenceMonitorRunPage{Items: runs}, nil
}
func TestIntelligenceGalleryKeepsTwentyTerminalRunsAlongsideLiveStatus(t *testing.T) {
	repo := &intelligenceGalleryRepository{intelligenceTestRepository: intelligenceTestRepository{plan: &IntelligenceMonitorPlan{ID: 3, Name: "gallery", SourceType: "external"}}}
	repo.runs = append(repo.runs, &IntelligenceMonitorRun{ID: 22, Status: "running"})
	for id := int64(21); id >= 1; id-- {
		status := "succeeded"
		if id%2 == 0 {
			status = "failed"
		}
		repo.runs = append(repo.runs, &IntelligenceMonitorRun{ID: id, Status: status})
	}
	svc := NewIntelligenceMonitorService(repo, upstreamTestEncryptor{}, nil, nil, nil, nil, nil)
	plans, err := svc.ListPlans(context.Background())
	require.NoError(t, err)
	require.Equal(t, "running", plans[0].LatestRun.Status)
	require.Len(t, plans[0].RecentRuns, 20)
	require.Equal(t, int64(21), plans[0].RecentRuns[0].ID)
	require.Equal(t, int64(2), plans[0].RecentRuns[19].ID)
	repo.runs = nil
	plans, err = svc.ListPlans(context.Background())
	require.NoError(t, err)
	require.NotNil(t, plans[0].RecentRuns)
	require.Empty(t, plans[0].RecentRuns)
}

type intelligenceStartupRepository struct {
	IntelligenceMonitorRepository
	pruned atomic.Bool
	ready  chan bool
}

func (r *intelligenceStartupRepository) PruneRuns(context.Context) error {
	r.pruned.Store(true)
	return nil
}
func (r *intelligenceStartupRepository) ExpireRuns(context.Context) error { return nil }
func (r *intelligenceStartupRepository) DuePlanIDs(context.Context, int) ([]int64, error) {
	r.ready <- r.pruned.Load()
	return nil, nil
}
func (r *intelligenceStartupRepository) ClaimNext(context.Context, string) (*IntelligenceMonitorRun, error) {
	return nil, nil
}
func TestIntelligenceStartupPrunesBeforeDispatch(t *testing.T) {
	repo := &intelligenceStartupRepository{ready: make(chan bool, 1)}
	svc := NewIntelligenceMonitorService(repo, nil, nil, nil, nil, nil, nil)
	svc.Start()
	defer svc.Stop()
	select {
	case pruned := <-repo.ready:
		require.True(t, pruned)
	case <-time.After(5 * time.Second):
		t.Fatal("scheduler did not start")
	}
}

func TestIntelligenceFixedPromptAndReasoningRequest(t *testing.T) {
	for _, mode := range []string{MonitorAPIModeResponses, MonitorAPIModeChatCompletions} {
		t.Run(mode, func(t *testing.T) {
			svc := NewIntelligenceMonitorService(nil, nil, nil, nil, nil, nil, nil)
			svc.externalClient = &http.Client{Transport: upstreamModelsTransport(func(request *http.Request) (*http.Response, error) {
				require.Equal(t, "Bearer test-secret", request.Header.Get("Authorization"))
				var body map[string]any
				require.NoError(t, json.NewDecoder(request.Body).Decode(&body))
				require.Equal(t, "gpt-6-astra", body["model"])
				require.Equal(t, false, body["stream"])
				expected := "创建一个 HTML，内容是用 SVG 绘制一个鹈鹕骑自行车的 2D 动画。你不需要任何测试。"
				response := `{"output_text":"<!doctype html><html><body>test-secret<svg></svg></body></html>","status":"completed"}`
				if mode == MonitorAPIModeResponses {
					require.Equal(t, "/v1/responses", request.URL.Path)
					require.Equal(t, expected, body["input"])
					require.Equal(t, "high", body["reasoning"].(map[string]any)["effort"])
				} else {
					require.Equal(t, "/v1/chat/completions", request.URL.Path)
					require.Equal(t, "high", body["reasoning_effort"])
					require.Equal(t, expected, body["messages"].([]any)[0].(map[string]any)["content"])
					response = `{"choices":[{"message":{"content":"<html><svg></svg></html>"},"finish_reason":"stop"}]}`
				}
				return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(response)), Header: make(http.Header)}, nil
			})}
			status, text, message := svc.generate(context.Background(), &IntelligenceMonitorRun{SourceType: "external", SourceEndpoint: "https://8.8.8.8", APIMode: mode}, "test-secret")
			require.Equal(t, 200, *status)
			require.Empty(t, message)
			require.NotContains(t, text, "test-secret")
			require.NotEmpty(t, extractIntelligenceHTML(text))
		})
	}
}

func TestIntelligenceQueueIsDurableBeforeGeneration(t *testing.T) {
	repo := &intelligenceTestRepository{plan: &IntelligenceMonitorPlan{ID: 3, Name: "External", SourceType: "external", Endpoint: "https://8.8.8.8", APIKeyEncrypted: "encrypted:secret", APIMode: "responses", TimeoutSeconds: 300, CreatedBy: 1}}
	svc := NewIntelligenceMonitorService(repo, upstreamTestEncryptor{}, nil, nil, nil, nil, nil)
	svc.externalClient = &http.Client{Transport: upstreamModelsTransport(func(*http.Request) (*http.Response, error) { t.Fatal("enqueue must not call a model"); return nil, nil })}
	run, err := svc.Enqueue(context.Background(), 3)
	require.NoError(t, err)
	require.Equal(t, "pending", run.Status)
	require.Equal(t, "encrypted:secret", repo.queued.RequestKeyEncrypted)
	encoded, err := json.Marshal(run)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "encrypted:secret")
	require.NotContains(t, string(encoded), "request_key_encrypted")
}

func TestIntelligencePauseDoesNotNeedNetworkOrRegenerateKey(t *testing.T) {
	repo := &intelligenceTestRepository{plan: &IntelligenceMonitorPlan{ID: 3, Name: "Offline", SourceType: "external", Endpoint: "https://offline.invalid", APIKeyEncrypted: "encrypted:secret", Enabled: true}}
	svc := NewIntelligenceMonitorService(repo, upstreamTestEncryptor{}, nil, nil, nil, nil, nil)
	enabled := false
	_, err := svc.SavePlan(context.Background(), 3, 1, IntelligenceMonitorInput{Enabled: &enabled})
	require.NoError(t, err)
	require.False(t, repo.saved.Enabled)
	require.True(t, repo.saved.AllowWhileBusy)
	require.Equal(t, "encrypted:secret", repo.saved.APIKeyEncrypted)
}

func TestIntelligenceExternalEndpointAndSecretValidation(t *testing.T) {
	svc := NewIntelligenceMonitorService(nil, upstreamTestEncryptor{}, nil, nil, nil, nil, nil)
	name, endpoint, key := "private", "https://127.0.0.1", "test-key"
	_, err := svc.SavePlan(context.Background(), 0, 1, IntelligenceMonitorInput{Name: &name, Endpoint: &endpoint, APIKey: &key})
	require.ErrorIs(t, err, ErrChannelMonitorEndpointPrivate)
	endpoint = "https://username:password@8.8.8.8"
	_, err = svc.SavePlan(context.Background(), 0, 1, IntelligenceMonitorInput{Name: &name, Endpoint: &endpoint, APIKey: &key})
	require.ErrorIs(t, err, ErrChannelMonitorInvalidEndpoint)
}

func TestIntelligenceLocalRouteCannotUseSourceEndpointAsOverride(t *testing.T) {
	cfg := &config.Config{Server: config.ServerConfig{Port: 9876}}
	svc := NewIntelligenceMonitorService(nil, nil, nil, nil, nil, nil, cfg)
	client, endpoint := svc.clientAndEndpoint(&IntelligenceMonitorRun{SourceType: "local_group", SourceEndpoint: "https://attacker.example"})
	require.Same(t, svc.localClient, client)
	require.Equal(t, "http://127.0.0.1:9876", endpoint)
	require.Nil(t, client.Transport.(*http.Transport).Proxy)
	require.ErrorIs(t, client.CheckRedirect(nil, nil), http.ErrUseLastResponse)
	require.Equal(t, 300*time.Second, client.Timeout)
}

func TestIntelligenceExtractsArtifactsAndRetainsIncompleteText(t *testing.T) {
	require.Equal(t, "<!doctype html><html><svg></svg></html>", extractIntelligenceHTML("Here it is:\n```html\n<!doctype html><html><svg></svg></html>\n```"))
	require.Empty(t, extractIntelligenceHTML("<html><svg>unfinished"))
	require.Contains(t, extractIntelligenceHTML("<svg><circle/></svg>"), "<body><svg><circle/></svg></body>")
	raw := []byte(`{"status":"incomplete","output_text":"<html>partial"}`)
	text, message := extractIntelligenceModelText(raw, "responses", false)
	require.Equal(t, "<html>partial", text)
	require.NotEmpty(t, message)
	sse := []byte("event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"delta\":\"<html><svg></svg></html>\"}\n\nevent: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"output_text\":\"<html><svg></svg></html>\"}}\n\n")
	text, message = extractIntelligenceModelText(sse, "responses", true)
	require.Empty(t, message)
	require.Equal(t, "<html><svg></svg></html>", text)
}

func TestIntelligenceLocalBillingIncludesEffectiveZeroAndUnknown(t *testing.T) {
	svc := NewIntelligenceMonitorService(nil, nil, nil, nil, nil, nil, nil)
	svc.localClient = &http.Client{Transport: upstreamModelsTransport(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"object":"sub2api.key_billing","schema_version":1,"peak_rate_enabled":false,"group_rate_multiplier":1,"user_rate_multiplier":0,"resolved_rate_multiplier":0,"effective_rate_multiplier":0,"billing_scope":"token","observed_at":"2026-09-24T00:00:00Z"}`))}, nil
	})}
	run := &IntelligenceMonitorRun{SourceName: "Local", SourceSnapshot: map[string]any{"group_id": int64(9)}}
	snapshot := svc.fetchLocalBilling(context.Background(), run, "key")
	require.Equal(t, "ok", snapshot.Status)
	require.False(t, snapshot.Stale)
	require.Equal(t, 0.0, *snapshot.EffectiveRateMultiplier)
	require.Equal(t, int64(9), *snapshot.GroupID)
	svc.localClient = &http.Client{Transport: upstreamModelsTransport(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"object":"sub2api.key_billing","schema_version":1,"peak_rate_enabled":false,"group_rate_multiplier":1,"user_rate_multiplier":null,"resolved_rate_multiplier":1,"effective_rate_multiplier":1,"billing_scope":"token","observed_at":"2026-09-24T00:00:00Z"}`))}, nil
	})}
	snapshot = svc.fetchLocalBilling(context.Background(), run, "key")
	require.Equal(t, "ok", snapshot.Status)
	require.Nil(t, snapshot.UserRateMultiplier)
	require.Equal(t, 1.0, *snapshot.EffectiveRateMultiplier)
	svc.localClient = &http.Client{Transport: upstreamModelsTransport(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 404, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("unsupported"))}, nil
	})}
	snapshot = svc.fetchLocalBilling(context.Background(), run, "key")
	require.Equal(t, "unsupported", snapshot.Status)
	require.Nil(t, snapshot.EffectiveRateMultiplier)
}

func TestIntelligenceModelErrorBodyIsNeverStored(t *testing.T) {
	svc := NewIntelligenceMonitorService(nil, nil, nil, nil, nil, nil, nil)
	svc.externalClient = &http.Client{Transport: upstreamModelsTransport(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 401, Header: make(http.Header), Body: io.NopCloser(strings.NewReader("echo private-test-credential"))}, nil
	})}
	status, text, message := svc.generate(context.Background(), &IntelligenceMonitorRun{SourceType: "external", SourceEndpoint: "https://8.8.8.8"}, "private-test-credential")
	require.Equal(t, 401, *status)
	require.Empty(t, text)
	require.Equal(t, "generation endpoint returned HTTP 401", message)
}

type intelligenceKeyRepoStub struct {
	APIKeyRepository
	key     *APIKey
	deleted []int64
}

func (r *intelligenceKeyRepoStub) GetByID(context.Context, int64) (*APIKey, error) {
	copy := *r.key
	return &copy, nil
}
func (r *intelligenceKeyRepoStub) GetKeyAndOwnerID(context.Context, int64) (string, int64, error) {
	return r.key.Key, r.key.UserID, nil
}
func (r *intelligenceKeyRepoStub) DeleteWithAudit(_ context.Context, id int64) error {
	r.deleted = append(r.deleted, id)
	return nil
}

func TestIntelligenceRevokesDedicatedKeyOnArchiveAndSourceChange(t *testing.T) {
	for _, action := range []string{"archive", "source_change"} {
		t.Run(action, func(t *testing.T) {
			keyID, owner, groupID := int64(22), int64(1), int64(9)
			keyRepo := &intelligenceKeyRepoStub{key: &APIKey{ID: keyID, UserID: owner, GroupID: &groupID, Key: "dedicated-key", Status: StatusActive}}
			keys := &APIKeyService{apiKeyRepo: keyRepo}
			repo := &intelligenceTestRepository{plan: &IntelligenceMonitorPlan{ID: 3, Name: "Local", SourceType: "local_group", LocalAPIKeyID: &keyID, LocalKeyOwnerID: &owner, GroupID: &groupID, IntervalSeconds: 3600, TimeoutSeconds: 300, APIMode: "responses"}}
			svc := NewIntelligenceMonitorService(repo, upstreamTestEncryptor{}, nil, nil, keys, nil, nil)
			if action == "archive" {
				require.NoError(t, svc.DeletePlan(context.Background(), 3))
				require.True(t, repo.archived)
			} else {
				source, endpoint, key := "external", "https://8.8.8.8", "external-key"
				_, err := svc.SavePlan(context.Background(), 3, owner, IntelligenceMonitorInput{SourceType: &source, Endpoint: &endpoint, APIKey: &key})
				require.NoError(t, err)
				require.Nil(t, repo.saved.LocalAPIKeyID)
				require.Nil(t, repo.saved.LocalKeyOwnerID)
			}
			require.Equal(t, []int64{keyID}, keyRepo.deleted)
		})
	}
}

func TestIntelligenceRejectsReassignedDedicatedKeyBeforeGatewayRequest(t *testing.T) {
	groupID, otherGroupID, keyID := int64(9), int64(10), int64(22)
	keyRepo := &intelligenceKeyRepoStub{key: &APIKey{ID: keyID, UserID: 1, GroupID: &otherGroupID, Key: "dedicated-key", Status: StatusActive}}
	svc := NewIntelligenceMonitorService(nil, upstreamTestEncryptor{}, nil, nil, &APIKeyService{apiKeyRepo: keyRepo}, nil, nil)
	run := &IntelligenceMonitorRun{SourceType: "local_group", SourceSnapshot: map[string]any{"group_id": groupID, "local_api_key_id": keyID}}
	require.NotEmpty(t, svc.validateRunSource(context.Background(), run, "dedicated-key"))
	keyRepo.key.GroupID = &groupID
	require.Empty(t, svc.validateRunSource(context.Background(), run, "dedicated-key"))
	keyRepo.key.Status = StatusAPIKeyDisabled
	require.NotEmpty(t, svc.validateRunSource(context.Background(), run, "dedicated-key"))
}

func TestIntelligenceStopWaitsForCancelledRunPersistence(t *testing.T) {
	repo := &intelligenceTestRepository{}
	svc := NewIntelligenceMonitorService(repo, upstreamTestEncryptor{}, nil, nil, nil, nil, nil)
	entered := make(chan struct{})
	svc.externalClient = &http.Client{Transport: upstreamModelsTransport(func(request *http.Request) (*http.Response, error) {
		close(entered)
		<-request.Context().Done()
		return nil, request.Context().Err()
	})}
	run := &IntelligenceMonitorRun{ID: 11, PlanID: 3, SourceType: "external", SourceEndpoint: "https://8.8.8.8", RequestKeyEncrypted: "encrypted:test-key", TimeoutSeconds: 180, APIMode: "responses", SourceSnapshot: map[string]any{}}
	svc.wg.Add(1)
	go func() { defer svc.wg.Done(); svc.execute(run) }()
	<-entered
	svc.Stop()
	require.NotNil(t, repo.completed)
	require.Equal(t, "failed", repo.completed.Status)
	require.Contains(t, repo.completed.Error, "cancelled")
}
