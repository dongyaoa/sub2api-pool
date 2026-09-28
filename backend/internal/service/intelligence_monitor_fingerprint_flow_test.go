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

	"github.com/stretchr/testify/require"
)

type intelligenceFingerprintProgressFixture struct {
	intelligenceTestRepository
	progress []*IntelligenceMonitorRun
	saveErr  error
}

func (r *intelligenceFingerprintProgressFixture) SaveCandyProgress(_ context.Context, run *IntelligenceMonitorRun) error {
	data, _ := json.Marshal(run)
	var copy IntelligenceMonitorRun
	_ = json.Unmarshal(data, &copy)
	r.progress = append(r.progress, &copy)
	return r.saveErr
}

func intelligenceFingerprintHTTPReply(text string) *http.Response {
	data, _ := json.Marshal(map[string]string{"output_text": text, "status": "completed"})
	return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(string(data)))}
}

func TestIntelligenceFingerprintCandyRoundCollectsIndependentSamplesAndPublishesAnswer(t *testing.T) {
	for _, candyAnswer := range []string{"所以最少取出 **21** 个。", "22"} {
		t.Run(candyAnswer, func(t *testing.T) {
			repo := &intelligenceFingerprintProgressFixture{}
			svc := NewIntelligenceMonitorService(repo, upstreamTestEncryptor{}, nil, nil, nil, nil, nil)
			defer svc.Stop()
			var reference intelligenceFingerprintBaseline
			for _, baseline := range intelligenceFingerprintBaselines {
				if baseline.Model == IntelligenceMonitorModel {
					reference = baseline
				}
			}
			counts, calls := map[string]int{}, 0
			svc.externalClient = &http.Client{Transport: upstreamModelsTransport(func(request *http.Request) (*http.Response, error) {
				calls++
				var body map[string]any
				require.NoError(t, json.NewDecoder(request.Body).Decode(&body))
				require.Equal(t, "Bearer fixture-secret", request.Header.Get("Authorization"))
				if body["input"] == IntelligenceMonitorCandyPrompt {
					require.Equal(t, 1, calls)
					return intelligenceFingerprintHTTPReply(candyAnswer), nil
				}
				require.NotEmpty(t, repo.progress, "answer is published before the first probe")
				require.Equal(t, "collecting", repo.progress[0].Fingerprint.Status)
				require.NotNil(t, repo.progress[0].Correct)
				require.Equal(t, candyAnswer != "22", *repo.progress[0].Correct)
				for _, probe := range intelligenceFingerprintQuickProbes() {
					for _, prompt := range probe.Prompts {
						if body["input"] == prompt {
							answer := reference.Cells[probe.ID][counts[probe.ID]]
							counts[probe.ID]++
							return intelligenceFingerprintHTTPReply(answer), nil
						}
					}
				}
				t.Fatal("unexpected request outside fixed probes")
				return nil, nil
			})}
			run := &IntelligenceMonitorRun{ID: 4, TestKind: IntelligenceMonitorTestCandy, Model: IntelligenceMonitorModel, ReasoningEffort: "high", SourceType: "external", SourceEndpoint: "https://8.8.8.8", APIMode: MonitorAPIModeResponses, RequestKeyEncrypted: "encrypted:fixture-secret", TimeoutSeconds: 600, SourceSnapshot: map[string]any{"execution_account_id": int64(42)}}
			svc.execute(run)
			require.Equal(t, 61, calls)
			for _, probe := range intelligenceFingerprintQuickProbes() {
				require.Equal(t, 15, counts[probe.ID])
			}
			require.Equal(t, "succeeded", repo.completed.Status)
			require.Equal(t, candyAnswer, repo.completed.RawText)
			require.Equal(t, candyAnswer != "22", *repo.completed.Correct)
			require.Equal(t, int64(42), repo.completed.SourceSnapshot["execution_account_id"])
			fingerprint := repo.completed.Fingerprint
			require.Equal(t, "completed", fingerprint.Status)
			require.Equal(t, 60, fingerprint.Done)
			require.Equal(t, 60, fingerprint.Valid)
			require.Zero(t, fingerprint.Errors)
			require.Len(t, fingerprint.Samples, 60)
			require.Equal(t, "consistent", fingerprint.Attribution.Status)
			require.True(t, *fingerprint.Passed)
			require.Equal(t, "comparing", repo.progress[len(repo.progress)-1].Fingerprint.Status)
			// Fast upstreams do not cause 60 progress writes for one short round.
			require.LessOrEqual(t, len(repo.progress), 3)
			encoded, err := json.Marshal(repo.completed)
			require.NoError(t, err)
			require.NotContains(t, string(encoded), "fixture-secret")
		})
	}
}

func TestIntelligenceFingerprintFailedOrInvalidSamplesNeverPass(t *testing.T) {
	svc := NewIntelligenceMonitorService(nil, nil, nil, nil, nil, nil, nil)
	defer svc.Stop()
	calls := 0
	svc.externalClient = &http.Client{Transport: upstreamModelsTransport(func(*http.Request) (*http.Response, error) {
		calls++
		if calls%2 == 0 {
			return &http.Response{StatusCode: 503, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"error":{"message":"fixture-secret"}}`))}, nil
		}
		return intelligenceFingerprintHTTPReply(strings.Repeat("x", 4097)), nil
	})}
	run := &IntelligenceMonitorRun{TestKind: IntelligenceMonitorTestCandy, RawText: "21", SourceType: "external", SourceEndpoint: "https://8.8.8.8", APIMode: MonitorAPIModeResponses}
	svc.collectIntelligenceFingerprint(context.Background(), run, "fixture-secret")
	require.Equal(t, 60, calls)
	require.Equal(t, 60, run.Fingerprint.Errors)
	require.Zero(t, run.Fingerprint.Valid)
	require.False(t, *run.Fingerprint.Passed)
	require.Equal(t, "insufficient", run.Fingerprint.Attribution.Status)
	require.True(t, *run.Correct, "failed probes must not replace the valid candy answer")
	encoded, err := json.Marshal(run.Fingerprint)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "fixture-secret")
}

func TestIntelligenceFingerprintCancellationBoundsRoundAndPreservesCandy(t *testing.T) {
	svc := NewIntelligenceMonitorService(nil, nil, nil, nil, nil, nil, nil)
	defer svc.Stop()
	calls := 0
	svc.externalClient = &http.Client{Transport: upstreamModelsTransport(func(request *http.Request) (*http.Response, error) {
		calls++
		<-request.Context().Done()
		return nil, request.Context().Err()
	})}
	run := &IntelligenceMonitorRun{TestKind: IntelligenceMonitorTestCandy, RawText: "21", SourceType: "external", SourceEndpoint: "https://8.8.8.8", APIMode: MonitorAPIModeResponses}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	svc.collectIntelligenceFingerprint(ctx, run, "fixture-secret")
	require.Equal(t, 1, calls)
	require.Equal(t, "timeout", run.Fingerprint.Status)
	require.False(t, *run.Fingerprint.Passed)
	require.True(t, *run.Correct)
	require.Empty(t, run.Error)
	require.Equal(t, "21", run.RawText)
}

func TestIntelligenceFingerprintStopsAfterEightConsecutiveRequestErrors(t *testing.T) {
	svc := NewIntelligenceMonitorService(nil, nil, nil, nil, nil, nil, nil)
	defer svc.Stop()
	calls := 0
	svc.externalClient = &http.Client{Transport: upstreamModelsTransport(func(*http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 400, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(`{"error":{"message":"unsupported parameter"}}`))}, nil
	})}
	run := &IntelligenceMonitorRun{TestKind: IntelligenceMonitorTestCandy, RawText: "21", SourceType: "external", SourceEndpoint: "https://8.8.8.8", APIMode: MonitorAPIModeResponses}
	svc.collectIntelligenceFingerprint(context.Background(), run, "fixture-secret")
	require.Equal(t, 8, calls)
	require.Equal(t, 8, run.Fingerprint.Done)
	require.Equal(t, "failed", run.Fingerprint.Status)
	require.False(t, *run.Fingerprint.Passed)
	require.Nil(t, run.Fingerprint.Attribution)
	require.True(t, *run.Correct)
}

func TestIntelligenceFingerprintDoesNotCallAfterFailedCandyOrLostLease(t *testing.T) {
	for _, tc := range []string{"failed candy", "lost lease", "pelican"} {
		t.Run(tc, func(t *testing.T) {
			repo := &intelligenceFingerprintProgressFixture{}
			svc := NewIntelligenceMonitorService(repo, nil, nil, nil, nil, nil, nil)
			defer svc.Stop()
			svc.externalClient = &http.Client{Transport: upstreamModelsTransport(func(*http.Request) (*http.Response, error) { t.Fatal("must not send probes"); return nil, nil })}
			run := &IntelligenceMonitorRun{TestKind: IntelligenceMonitorTestCandy, RawText: "21"}
			switch tc {
			case "failed candy":
				run.Error = "HTTP 503"
			case "lost lease":
				repo.saveErr = ErrIntelligenceNotFound
			case "pelican":
				run.TestKind = IntelligenceMonitorTestPelican
			}
			svc.collectIntelligenceFingerprint(context.Background(), run, "fixture-secret")
			if tc == "pelican" {
				require.Nil(t, run.Fingerprint)
			} else {
				require.Equal(t, "failed", run.Fingerprint.Status)
				require.False(t, *run.Fingerprint.Passed)
			}
		})
	}
}
