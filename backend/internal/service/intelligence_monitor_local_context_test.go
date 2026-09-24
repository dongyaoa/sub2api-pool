//go:build unit

package service

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func newIntelligenceLocalPermitRequest(t *testing.T, ctx context.Context, key *APIKey) *http.Request {
	t.Helper()
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://127.0.0.1:8081/v1/responses", nil)
	require.NoError(t, err)
	cleanup, err := AuthorizeIntelligenceLocalRequest(request, key.ID, key.Key)
	require.NoError(t, err)
	t.Cleanup(cleanup)
	// Model the HTTP boundary: an inbound request has a new context and only
	// the opaque header crosses from the worker into the gateway.
	request = request.Clone(context.Background())
	request.RemoteAddr = "127.0.0.1:54321"
	return request
}

func intelligencePermitExists(token string) bool {
	intelligenceLocalPermits.Lock()
	defer intelligenceLocalPermits.Unlock()
	_, exists := intelligenceLocalPermits.entries[token]
	return exists
}

func TestIntelligenceLocalRequestOneUseAndBoundedLifecycle(t *testing.T) {
	workerCtx, cancelWorker := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancelWorker)
	key := &APIKey{ID: 72, Key: "monitor-secret"}
	request := newIntelligenceLocalPermitRequest(t, workerCtx, key)
	replay := request.Clone(context.Background())
	token := request.Header.Get(intelligenceLocalRequestHeader)
	require.Len(t, token, 64)
	require.NotContains(t, token, key.Key)

	ctx, release := BindIntelligenceLocalRequest(request, key)
	defer release()
	deadline, bounded := ctx.Deadline()
	workerDeadline, _ := workerCtx.Deadline()
	require.True(t, bounded)
	require.Equal(t, workerDeadline, deadline)
	require.Equal(t, true, ctx.Value(intelligenceGenerationContextKey{}))
	require.Equal(t, 15*time.Minute, HTTPUpstreamResponseHeaderTimeoutFromContext(ctx))
	require.Empty(t, request.Header.Get(intelligenceLocalRequestHeader))
	require.False(t, intelligencePermitExists(token))

	replayedCtx, releaseReplay := BindIntelligenceLocalRequest(replay, key)
	defer releaseReplay()
	require.True(t, replay.Context() == replayedCtx)
	require.Zero(t, HTTPUpstreamResponseHeaderTimeoutFromContext(replayedCtx))

	for _, detach := range []func(context.Context) (context.Context, context.CancelFunc){
		detachUpstreamContext,
		func(value context.Context) (context.Context, context.CancelFunc) {
			return detachStreamUpstreamContext(value, true)
		},
	} {
		upstreamCtx, releaseUpstream := detach(ctx)
		defer releaseUpstream()
		require.Same(t, ctx, upstreamCtx)
	}
	cancelWorker()
	select {
	case <-ctx.Done():
		require.ErrorIs(t, ctx.Err(), context.Canceled)
	case <-time.After(time.Second):
		t.Fatal("worker cancellation did not reach the gateway")
	}
}

func TestIntelligenceLocalRequestRejectsMismatches(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*http.Request, *APIKey)
	}{
		{"key_id", func(_ *http.Request, key *APIKey) { key.ID++ }},
		{"key_secret", func(_ *http.Request, key *APIKey) { key.Key = "another-secret" }},
		{"method", func(request *http.Request, _ *APIKey) { request.Method = http.MethodGet }},
		{"path", func(request *http.Request, _ *APIKey) { request.URL.Path = "/v1/chat/completions" }},
		{"query", func(request *http.Request, _ *APIKey) { request.URL.RawQuery = "different=1" }},
		{"external_peer", func(request *http.Request, _ *APIKey) {
			request.RemoteAddr = "192.0.2.1:80"
			request.Header.Set("X-Forwarded-For", "127.0.0.1")
		}},
		{"malformed_peer", func(request *http.Request, _ *APIKey) { request.RemoteAddr = "127.0.0.1" }},
		{"forged_token", func(request *http.Request, _ *APIKey) {
			request.Header.Set(intelligenceLocalRequestHeader, strings.Repeat("a", 64))
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			workerCtx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			key := &APIKey{ID: 72, Key: "monitor-secret"}
			request := newIntelligenceLocalPermitRequest(t, workerCtx, key)
			originalToken := request.Header.Get(intelligenceLocalRequestHeader)
			tc.mutate(request, key)
			ctx, release := BindIntelligenceLocalRequest(request, key)
			defer release()
			require.True(t, request.Context() == ctx)
			require.Zero(t, HTTPUpstreamResponseHeaderTimeoutFromContext(ctx))
			require.Empty(t, request.Header.Get(intelligenceLocalRequestHeader))
			if tc.name != "forged_token" {
				require.False(t, intelligencePermitExists(originalToken), "mismatches must burn the token")
			}
		})
	}
}

func TestIntelligenceLocalRequestCleanupAndCancellation(t *testing.T) {
	for _, expired := range []bool{false, true} {
		t.Run(map[bool]string{false: "cancelled", true: "expired"}[expired], func(t *testing.T) {
			duration := time.Minute
			if expired {
				duration = 40 * time.Millisecond
			}
			workerCtx, cancel := context.WithTimeout(context.Background(), duration)
			defer cancel()
			key := &APIKey{ID: 72, Key: "monitor-secret"}
			request := newIntelligenceLocalPermitRequest(t, workerCtx, key)
			token := request.Header.Get(intelligenceLocalRequestHeader)
			if !expired {
				cancel()
			}
			<-workerCtx.Done()
			require.Eventually(t, func() bool { return !intelligencePermitExists(token) }, time.Second, time.Millisecond)
			ctx, release := BindIntelligenceLocalRequest(request, key)
			defer release()
			require.True(t, request.Context() == ctx)
		})
	}

	workerCtx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	request, err := http.NewRequestWithContext(workerCtx, http.MethodPost, "http://127.0.0.1/v1/responses", nil)
	require.NoError(t, err)
	cleanup, err := AuthorizeIntelligenceLocalRequest(request, 72, "monitor-secret")
	require.NoError(t, err)
	token := request.Header.Get(intelligenceLocalRequestHeader)
	cleanup()
	cleanup()
	require.False(t, intelligencePermitExists(token))
}

func TestIntelligenceLocalRequestInboundCancellation(t *testing.T) {
	workerCtx, cancelWorker := context.WithTimeout(context.Background(), time.Minute)
	defer cancelWorker()
	key := &APIKey{ID: 72, Key: "monitor-secret"}
	request := newIntelligenceLocalPermitRequest(t, workerCtx, key)
	inboundCtx, cancelInbound := context.WithCancel(context.Background())
	ctx, release := BindIntelligenceLocalRequest(request.WithContext(inboundCtx), key)
	defer release()
	cancelInbound()
	require.ErrorIs(t, ctx.Err(), context.Canceled)
	require.NoError(t, workerCtx.Err())
}

func TestIntelligenceLocalRequestAuthorizationRequiresShortLoopbackDeadline(t *testing.T) {
	for _, tc := range []struct {
		name, endpoint string
		timeout        time.Duration
	}{
		{"external", "http://192.0.2.1/v1/responses", time.Minute},
		{"hostname", "http://localhost/v1/responses", time.Minute},
		{"wrong_path", "http://127.0.0.1/v1/messages", time.Minute},
		{"query", "http://127.0.0.1/v1/responses?query=1", time.Minute},
		{"unbounded", "http://127.0.0.1/v1/responses", 0},
		{"expired", "http://127.0.0.1/v1/responses", -time.Second},
		{"too_long", "http://127.0.0.1/v1/responses", 16 * time.Minute},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			if tc.timeout != 0 {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, tc.timeout)
				defer cancel()
			}
			request, err := http.NewRequestWithContext(ctx, http.MethodPost, tc.endpoint, nil)
			require.NoError(t, err)
			cleanup, err := AuthorizeIntelligenceLocalRequest(request, 72, "monitor-secret")
			require.Error(t, err)
			require.Nil(t, cleanup)
			require.Empty(t, request.Header.Get(intelligenceLocalRequestHeader))
		})
	}
}

func TestIntelligenceLocalGeneratePermitScopedAndCleaned(t *testing.T) {
	for _, source := range []string{"local_group", "external", "upstream"} {
		t.Run(source, func(t *testing.T) {
			var token string
			client := &http.Client{Transport: upstreamModelsTransport(func(request *http.Request) (*http.Response, error) {
				token = request.Header.Get(intelligenceLocalRequestHeader)
				if source == "local_group" {
					require.True(t, intelligencePermitExists(token))
				} else {
					require.Empty(t, token)
				}
				return nil, errors.New("simulated dispatch failure")
			})}
			svc := &IntelligenceMonitorService{localClient: client, externalClient: client, localEndpoint: "http://127.0.0.1:8081"}
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			_, _, message := svc.generate(ctx, &IntelligenceMonitorRun{SourceType: source, SourceEndpoint: "https://8.8.8.8", SourceSnapshot: map[string]any{"local_api_key_id": int64(72)}}, "monitor-secret")
			require.Contains(t, message, "failed to connect")
			if source == "local_group" {
				require.Len(t, token, 64)
				require.False(t, intelligencePermitExists(token))
			}
		})
	}
}

func TestIntelligenceLocalStreamIntervalsPreserveOrdinaryTraffic(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	ordinaryCtx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil).WithContext(ordinaryCtx)
	c.Request.Header.Set("User-Agent", "Sub2API-IntelligenceMonitor/1")
	c.Request.Header.Set(intelligenceLocalRequestHeader, strings.Repeat("a", 64))
	require.Equal(t, 180*time.Second, intelligenceMonitorStreamInterval(c, 180*time.Second))
	detached, release := detachStreamUpstreamContext(ordinaryCtx, true)
	defer release()
	_, bounded := detached.Deadline()
	require.False(t, bounded)
	cancel()
	require.NoError(t, detached.Err())
}

func TestIntelligenceLocalBufferedReadersWaitBeyondOrdinaryIdle(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, reader := range []string{"responses_sse", "native_cc", "native_responses"} {
		for _, trusted := range []bool{false, true} {
			name := reader + map[bool]string{false: "_ordinary", true: "_iq"}[trusted]
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				svc := newNativeAnthropicHangTestService(1)
				c, _ := gin.CreateTestContext(httptest.NewRecorder())
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
				if trusted {
					workerCtx, cancelWorker := context.WithTimeout(context.Background(), 5*time.Second)
					defer cancelWorker()
					key := &APIKey{ID: 72, Key: "monitor-secret"}
					request := newIntelligenceLocalPermitRequest(t, workerCtx, key)
					ctx, release := BindIntelligenceLocalRequest(request, key)
					defer release()
					c.Request = request.WithContext(ctx)
				}
				resp, pr, pw := newHangingUpstreamResponse()
				defer pr.Close()
				defer pw.Close()
				payload := miniAnthropicSSEStream()
				if reader == "responses_sse" {
					payload = "data: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_iq\",\"status\":\"completed\",\"output\":[]}}\n\n"
				}
				go func() { time.Sleep(1250 * time.Millisecond); _, _ = io.WriteString(pw, payload); _ = pw.Close() }()
				var err error
				switch reader {
				case "responses_sse":
					var response *apicompat.ResponsesResponse
					response, _, _, err = svc.readOpenAICompatBufferedTerminal(resp, c, "iq-test", "iq-test")
					if trusted {
						require.NotNil(t, response)
					}
				case "native_cc":
					_, err = svc.handleCCBufferedFromNativeAnthropic(resp, c, "glm-4.7", "glm-4.7", "glm-4.7", nil, time.Now())
				case "native_responses":
					_, err = svc.handleResponsesBufferedFromNativeAnthropic(resp, c, "glm-4.7", "glm-4.7", "glm-4.7", nil, time.Now(), apicompat.ResponsesClientToolMapping{})
				}
				if trusted {
					require.NoError(t, err)
				} else {
					require.ErrorContains(t, err, "stream data interval timeout")
				}
			})
		}
	}
}
