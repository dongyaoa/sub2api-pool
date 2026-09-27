//go:build unit

package service

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestIntelligenceExecutionTraceRequiresBoundOneUsePermit(t *testing.T) {
	worker, cancel := context.WithTimeout(context.Background(), time.Minute)
	t.Cleanup(cancel)
	worker, trace := newIntelligenceExecutionTrace(worker)
	account := &Account{ID: 14, Name: "Actual route", Type: AccountTypeAPIKey, Platform: PlatformOpenAI, Credentials: map[string]any{"base_url": "https://name:secret@relay.example:8443/private-key/v1?key=secret#token", "api_key": "must-not-copy"}}
	RecordIntelligenceExecutionAccount(context.Background(), account)
	RecordIntelligenceExecutionAccount(worker, account)
	require.Nil(t, trace.sourceSnapshot(nil, true), "the worker context alone is not a validated inbound request")
	key := &APIKey{ID: 72, Key: "monitor-secret"}
	request := newIntelligenceLocalPermitRequest(t, worker, key)
	replay := request.Clone(context.Background())
	bound, release := BindIntelligenceLocalRequest(request, key)
	defer release()
	RecordIntelligenceExecutionAccount(bound, nil)
	RecordIntelligenceExecutionAccount(bound, &Account{})
	RecordIntelligenceExecutionAccount(bound, account)
	replayed, releaseReplay := BindIntelligenceLocalRequest(replay, key)
	defer releaseReplay()
	RecordIntelligenceExecutionAccount(replayed, &Account{ID: 99, Name: "Forged replacement"})
	snapshot := trace.sourceSnapshot(map[string]any{"group_id": int64(8)}, true)
	require.Equal(t, int64(8), snapshot["group_id"])
	require.Equal(t, int64(14), snapshot["execution_account_id"])
	require.Equal(t, "Actual route", snapshot["execution_account_name"])
	require.Equal(t, AccountTypeAPIKey, snapshot["execution_account_type"])
	require.Equal(t, PlatformOpenAI, snapshot["execution_account_platform"])
	require.Equal(t, "https://relay.example:8443", snapshot["execution_account_base_origin"])
	started, err := time.Parse(time.RFC3339Nano, snapshot["execution_started_at"].(string))
	require.NoError(t, err)
	require.WithinDuration(t, time.Now(), started, time.Second)
	require.Equal(t, "completed", snapshot["execution_source_status"])
	require.Equal(t, 1, snapshot["execution_attempt_count"])
	encoded, err := json.Marshal(snapshot)
	require.NoError(t, err)
	for _, secret := range []string{"private-key", "secret", "must-not-copy", "Forged replacement", "token"} {
		require.NotContains(t, string(encoded), secret)
	}
	account.Name = "Renamed after request"
	require.Equal(t, "Actual route", trace.sourceSnapshot(nil, true)["execution_account_name"], "execution metadata is captured, not read later")
}

func TestIntelligenceExecutionTraceIsolatedRequestsAndForgedHeaders(t *testing.T) {
	worker, cancel := context.WithTimeout(context.Background(), time.Minute)
	t.Cleanup(cancel)
	firstCtx, firstTrace := newIntelligenceExecutionTrace(worker)
	secondCtx, secondTrace := newIntelligenceExecutionTrace(worker)
	key := &APIKey{ID: 72, Key: "monitor-secret"}
	first := newIntelligenceLocalPermitRequest(t, firstCtx, key)
	second := newIntelligenceLocalPermitRequest(t, secondCtx, key)
	firstBound, releaseFirst := BindIntelligenceLocalRequest(first, key)
	defer releaseFirst()
	second.Header.Set(intelligenceLocalRequestHeader, strings.Repeat("a", 64))
	second.Header.Set("X-Sub2api-Execution-Account-ID", "999")
	secondBound, releaseSecond := BindIntelligenceLocalRequest(second, key)
	defer releaseSecond()
	RecordIntelligenceExecutionAccount(firstBound, &Account{ID: 11})
	RecordIntelligenceExecutionAccount(secondBound, &Account{ID: 22})
	require.Equal(t, int64(11), firstTrace.sourceSnapshot(nil, true)["execution_account_id"])
	require.Nil(t, secondTrace.sourceSnapshot(nil, true))
	require.Empty(t, second.Header.Get(intelligenceLocalRequestHeader))
}

func TestIntelligenceExecutionTraceSnapshotsAreRaceSafe(t *testing.T) {
	worker, cancel := context.WithTimeout(context.Background(), time.Minute)
	t.Cleanup(cancel)
	worker, trace := newIntelligenceExecutionTrace(worker)
	request := newIntelligenceLocalPermitRequest(t, worker, &APIKey{ID: 72, Key: "monitor-secret"})
	bound, release := BindIntelligenceLocalRequest(request, &APIKey{ID: 72, Key: "monitor-secret"})
	defer release()
	var wait sync.WaitGroup
	for range 8 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			for range 100 {
				RecordIntelligenceExecutionAccount(bound, &Account{ID: 14, Name: "Concurrent"})
				_ = trace.sourceSnapshot(nil, false)
			}
		}()
	}
	wait.Wait()
	snapshot := trace.sourceSnapshot(nil, false)
	require.Equal(t, 800, snapshot["execution_attempt_count"])
	require.Equal(t, "attempted", snapshot["execution_source_status"])
}

func TestIntelligenceExecutionBaseOriginDoesNotExposeURLSecrets(t *testing.T) {
	for _, test := range []struct{ raw, want string }{
		{"https://relay.example/v1?api_key=secret#token", "https://relay.example"},
		{"http://user:password@127.0.0.1:8081/private/path", "http://127.0.0.1:8081"},
		{"https://[::1]:443/v1", "https://[::1]:443"},
		{" https://relay.example/v1 ", "https://relay.example"},
		{"", ""}, {"relay.example/path", ""}, {"//relay.example/path", ""},
		{"javascript:alert(1)", ""}, {"file:///private/secret", ""},
		{"https:///private/secret", ""}, {"https://relay.example/%zz", ""},
		{"https://" + strings.Repeat("a", 8192), ""},
	} {
		require.Equal(t, test.want, intelligenceExecutionBaseOrigin(test.raw), test.raw)
	}
}

func TestIntelligenceExecutionTraceFollowsLastForwardAttemptForBothTestsAndAPIs(t *testing.T) {
	for _, kind := range []string{IntelligenceMonitorTestPelican, IntelligenceMonitorTestCandy} {
		for _, mode := range []string{MonitorAPIModeResponses, MonitorAPIModeChatCompletions} {
			for _, outcome := range []string{"completed", "model_error", "http_error", "transport_error", "before_forward"} {
				t.Run(kind+"/"+mode+"/"+outcome, func(t *testing.T) {
					var retainedCtx context.Context
					var finalAttemptStartedAt string
					key := &APIKey{ID: 72, Key: "monitor-secret"}
					client := &http.Client{Transport: upstreamModelsTransport(func(request *http.Request) (*http.Response, error) {
						inbound := request.Clone(context.Background())
						inbound.RemoteAddr = "127.0.0.1:54321"
						bound, release := BindIntelligenceLocalRequest(inbound, key)
						defer release()
						retainedCtx = bound
						if outcome != "before_forward" {
							RecordIntelligenceExecutionAccount(bound, &Account{ID: 10, Name: "Failed first route", Type: AccountTypeAPIKey, Platform: PlatformOpenAI})
							RecordIntelligenceExecutionAccount(bound, &Account{ID: 20, Name: "Final route", Type: AccountTypeOAuth, Platform: PlatformOpenAI})
							trace := bound.Value(intelligenceExecutionTraceContextKey{}).(*intelligenceExecutionTrace)
							finalAttemptStartedAt = trace.sourceSnapshot(nil, false)["execution_started_at"].(string)
						}
						if outcome == "transport_error" {
							return nil, errors.New("simulated transport failure")
						}
						status, body := 200, `{"status":"completed","output_text":"21"}`
						if mode == MonitorAPIModeChatCompletions {
							body = `{"choices":[{"message":{"content":"21"},"finish_reason":"stop"}]}`
						}
						if outcome == "model_error" {
							body = `{"error":{"message":"model generation failed"}}`
						}
						if outcome == "http_error" || outcome == "before_forward" {
							status, body = 503, `{"error":{"message":"no available account"}}`
						}
						return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
					})}
					svc := &IntelligenceMonitorService{localClient: client, localEndpoint: "http://127.0.0.1:8081"}
					ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
					defer cancel()
					run := &IntelligenceMonitorRun{TestKind: kind, APIMode: mode, SourceType: "local_group", SourceSnapshot: map[string]any{"local_api_key_id": int64(72), "group_id": int64(3)}}
					_, _, message := svc.generate(ctx, run, key.Key)
					require.Equal(t, int64(3), run.SourceSnapshot["group_id"])
					if outcome == "before_forward" {
						require.NotContains(t, run.SourceSnapshot, "execution_account_id", "do not infer an account from group membership")
						return
					}
					require.Equal(t, int64(20), run.SourceSnapshot["execution_account_id"])
					require.Equal(t, "Final route", run.SourceSnapshot["execution_account_name"])
					require.Equal(t, 2, run.SourceSnapshot["execution_attempt_count"])
					require.Equal(t, finalAttemptStartedAt, run.SourceSnapshot["execution_started_at"], "binding resolution uses the final forwarding attempt's captured time")
					if outcome == "completed" {
						require.Empty(t, message)
						require.Equal(t, "completed", run.SourceSnapshot["execution_source_status"])
					} else {
						require.NotEmpty(t, message)
						require.Equal(t, "attempted", run.SourceSnapshot["execution_source_status"])
					}
					RecordIntelligenceExecutionAccount(retainedCtx, &Account{ID: 30, Name: "Late update"})
					require.Equal(t, int64(20), run.SourceSnapshot["execution_account_id"], "gateway must not mutate the saved run after generate returns")
				})
			}
		}
	}
}

func TestIntelligenceExecutionTraceDoesNotInventSourcesForExternalGenerations(t *testing.T) {
	for _, source := range []string{"external", "upstream"} {
		t.Run(source, func(t *testing.T) {
			client := &http.Client{Transport: upstreamModelsTransport(func(request *http.Request) (*http.Response, error) {
				require.Empty(t, request.Header.Get(intelligenceLocalRequestHeader))
				require.Nil(t, request.Context().Value(intelligenceExecutionTraceContextKey{}))
				RecordIntelligenceExecutionAccount(request.Context(), &Account{ID: 999})
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"status":"completed","output_text":"21"}`))}, nil
			})}
			svc := &IntelligenceMonitorService{externalClient: client}
			run := &IntelligenceMonitorRun{SourceType: source, SourceEndpoint: "https://8.8.8.8", SourceSnapshot: map[string]any{"supplier_id": int64(4)}}
			_, _, message := svc.generate(context.Background(), run, "sample-key")
			require.Empty(t, message)
			require.Equal(t, map[string]any{"supplier_id": int64(4)}, run.SourceSnapshot)
		})
	}
	_, _, message := (&IntelligenceMonitorService{}).generate(context.Background(), nil, "sample-key")
	require.Equal(t, "unsupported intelligence test", message)
}
