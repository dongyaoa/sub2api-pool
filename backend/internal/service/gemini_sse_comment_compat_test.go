package service

import (
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestGeminiClientRejectsSSEComments(t *testing.T) {
	cases := []struct {
		name string
		hint string
		want bool
	}{
		{"go-genai (Antigravity CLI)", "google-genai-sdk/1.71.0 gl-go/go1.28-20260721-RC03 cl/951519500 +3ebc191975 X:fieldtrack,boringcrypto", true},
		{"python-genai", "google-genai-sdk/1.20.0 gl-python/3.12.4", true},
		{"js-genai tolerates comments", "google-genai-sdk/1.9.0 gl-node/22.3.0", false},
		{"gemini-cli", "GeminiCLI/0.60.0 (darwin; arm64)", false},
		{"curl", "curl/8.7.1", false},
		{"empty", "", false},
		{"case-insensitive", "Google-GenAI-SDK/1.0.0 GL-Go/go1.27", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, geminiClientRejectsSSEComments(tc.hint))
		})
	}
}

func TestDownstreamRejectsSSECommentsReadsBothHeaders(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := newAntigravityCompatContext(http.MethodPost, "/v1beta/models/gemini-3.8-flash:streamGenerateContent", nil)
	require.False(t, downstreamRejectsSSEComments(c))

	c.Request.Header.Set("X-Goog-Api-Client", "google-genai-sdk/1.71.0 gl-go/go1.28")
	require.True(t, downstreamRejectsSSEComments(c))

	c.Request.Header.Del("X-Goog-Api-Client")
	c.Request.Header.Set("User-Agent", "google-genai-sdk/1.71.0 gl-go/go1.28")
	require.True(t, downstreamRejectsSSEComments(c))

	require.False(t, downstreamRejectsSSEComments(nil))
}

type antigravityKeepaliveObserver struct {
	gin.ResponseWriter
	data      chan struct{}
	keepalive chan struct{}
}

func (w *antigravityKeepaliveObserver) Write(p []byte) (int, error) {
	n, err := w.ResponseWriter.Write(p)
	if err == nil {
		if strings.HasPrefix(string(p[:n]), "data:") {
			select {
			case w.data <- struct{}{}:
			default:
			}
		}
		if strings.Contains(string(p[:n]), ":\n\n") {
			select {
			case w.keepalive <- struct{}{}:
			default:
			}
		}
	}
	return n, err
}

// runAntigravityGeminiStreamWithIdle 先发一个 data 事件，再保持上游空闲。
// 普通客户端等待真实心跳；不兼容客户端覆盖两个完整的 keepalive 周期。
func runAntigravityGeminiStreamWithIdle(t *testing.T, userAgent string, wantKeepalive bool) string {
	t.Helper()
	gin.SetMode(gin.TestMode)
	svc := newAntigravityCompatService(
		config.GatewayConfig{MaxLineSize: defaultMaxLineSize, StreamKeepaliveInterval: 1},
		nil,
	)
	c, recorder := newAntigravityCompatContext(http.MethodPost, "/v1beta/models/gemini-3.8-flash:streamGenerateContent", nil)
	if userAgent != "" {
		c.Request.Header.Set("User-Agent", userAgent)
	}
	observer := &antigravityKeepaliveObserver{
		ResponseWriter: c.Writer,
		data:           make(chan struct{}, 1),
		keepalive:      make(chan struct{}, 1),
	}
	c.Writer = observer
	reader, writer := io.Pipe()
	t.Cleanup(func() {
		_ = writer.Close()
		_ = reader.Close()
	})
	resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: reader}
	done := make(chan error, 1)
	go func() {
		_, err := svc.handleGeminiStreamingResponse(c, resp, time.Now())
		done <- err
	}()
	_, err := io.WriteString(
		writer,
		`data: {"response":{"responseId":"resp_1","candidates":[{"content":{"parts":[{"text":"partial"}]}}],"usageMetadata":{"promptTokenCount":8,"candidatesTokenCount":1}}}`+"\n\n",
	)
	require.NoError(t, err)
	select {
	case <-observer.data:
	case err := <-done:
		t.Fatalf("stream ended before the data event: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for the data event")
	}
	if wantKeepalive {
		// The first tick can occur less than one interval after the data event
		// and be skipped. Synchronize on the actual write instead of sleeping.
		select {
		case <-observer.keepalive:
		case err := <-done:
			t.Fatalf("stream ended before the idle keepalive: %v", err)
		case <-time.After(5 * time.Second):
			t.Fatal("timed out waiting for the idle keepalive")
		}
	} else {
		select {
		case <-observer.keepalive:
			t.Fatal("incompatible client received an idle keepalive")
		case err := <-done:
			t.Fatalf("stream ended before the idle observation window: %v", err)
		case <-time.After(2500 * time.Millisecond):
		}
	}
	require.NoError(t, writer.Close())
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("stream did not finish after the upstream closed")
	}
	require.NoError(t, reader.Close())
	return recorder.Body.String()
}

func TestAntigravityGeminiStreamKeepsCommentKeepaliveForOrdinaryClients(t *testing.T) {
	out := runAntigravityGeminiStreamWithIdle(t, "curl/8.7.1", true)
	require.Contains(t, out, ":\n\n", "ordinary clients should still get the idle keepalive")
	require.Contains(t, out, `"text":"partial"`)
}

func TestAntigravityGeminiStreamSkipsCommentKeepaliveForGoGenai(t *testing.T) {
	out := runAntigravityGeminiStreamWithIdle(t, "google-genai-sdk/1.71.0 gl-go/go1.28-20260721-RC03", false)
	require.Contains(t, out, `"text":"partial"`)
	for _, event := range strings.Split(out, "\n\n") {
		require.False(t, strings.HasPrefix(event, ":"), "go-genai must never receive an SSE comment event, got %q", event)
	}
}
