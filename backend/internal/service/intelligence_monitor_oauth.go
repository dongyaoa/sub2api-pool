package service

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/util/logredact"
	"github.com/gin-gonic/gin"
)

type intelligenceOAuthForwarder interface {
	Forward(context.Context, *gin.Context, *Account, []byte) (*OpenAIForwardResult, error)
}
type intelligenceOAuthSlots interface {
	AcquireAccountSlotForAccount(context.Context, *Account) (*AcquireResult, error)
}

// ConfigureOpenAIOAuth is called during construction, before Start. Account
// credentials remain owned/refreshed by the established OpenAI gateway.
func (s *IntelligenceMonitorService) ConfigureOpenAIOAuth(accounts AccountRepository, gateway *OpenAIGatewayService, concurrency *ConcurrencyService) {
	s.accounts = accounts
	if gateway != nil {
		s.oauthForward = gateway
	}
	if concurrency != nil {
		s.oauthSlots = concurrency
	}
}

func (s *IntelligenceMonitorService) intelligenceOAuthAccount(ctx context.Context, id *int64) (*Account, error) {
	invalid := func(message string) (*Account, error) {
		return nil, ErrIntelligenceInvalid.WithMetadata(map[string]string{"field": "account_id", "detail": message})
	}
	if id == nil || *id <= 0 || s.accounts == nil {
		return invalid("choose an existing OpenAI OAuth account")
	}
	account, err := s.accounts.GetByID(ctx, *id)
	if err != nil || account == nil || !account.IsOpenAIOAuth() || account.IsShadow() || account.IsSyntheticUITest() {
		return invalid("choose an existing OpenAI OAuth account; shadow and synthetic accounts are not supported")
	}
	if !account.IsModelSupported(IntelligenceMonitorModel) || account.GetMappedModel(IntelligenceMonitorModel) != IntelligenceMonitorModel {
		return invalid("the selected account must support the fixed gpt-6-astra model without remapping")
	}
	// The picker and save endpoint must not admit an account that execution
	// would immediately reject. Recheck again at execution because status and
	// transient limits can change while a generation waits in the queue.
	if !account.IsSchedulable() {
		return invalid("the selected OAuth account is disabled, paused, expired, rate limited or cooling down")
	}
	return account, nil
}

func (s *IntelligenceMonitorService) generateOpenAIOAuth(ctx context.Context, run *IntelligenceMonitorRun) (*int, string, string) {
	if msg, _ := run.SourceSnapshot["resolution_error"].(string); msg != "" {
		return nil, "", msg
	}
	if s.oauthForward == nil || s.oauthSlots == nil {
		return nil, "", "OpenAI OAuth monitoring is unavailable"
	}
	id := intelligenceSnapshotID(run.SourceSnapshot["account_id"])
	account, err := s.intelligenceOAuthAccount(ctx, &id)
	if err != nil {
		return nil, "", "selected OAuth account is unavailable, not schedulable, or does not support the fixed model"
	}
	// Proxy selection mutates only this request's account snapshot. Remove
	// unavailable pool entries before concurrency can try an alternate proxy.
	selected := *account
	selected.ProxyPool = make([]AccountProxyPoolEntry, 0, len(account.ProxyPool))
	for _, entry := range account.ProxyPool {
		if entry.Concurrency > 0 && entry.Proxy != nil && (entry.Proxy.Status == "" || entry.Proxy.IsActive()) && !entry.Proxy.IsExpired(time.Now()) {
			selected.ProxyPool = append(selected.ProxyPool, entry)
		}
	}
	if len(account.ProxyPool) > 0 && len(selected.ProxyPool) == 0 {
		return nil, "", "selected OAuth account has no usable configured proxy"
	}
	if err = selectAccountTestProxy(&selected, nil); err != nil {
		return nil, "", "selected OAuth account has no usable configured proxy"
	}
	slot, err := s.oauthSlots.AcquireAccountSlotForAccount(ctx, &selected)
	if err != nil || slot == nil || !slot.Acquired {
		return nil, "", "selected OAuth account or proxy has no available concurrency slot"
	}
	defer slot.ReleaseFunc()
	if run.SourceSnapshot == nil {
		run.SourceSnapshot = map[string]any{}
	}
	run.SourceSnapshot["oauth"] = true
	run.SourceSnapshot["auth_type"] = "oauth"
	run.SourceSnapshot["execution_account_id"] = selected.ID
	run.SourceSnapshot["execution_account_name"] = selected.Name
	if selected.ProxyID != nil {
		run.SourceSnapshot["proxy_id"] = *selected.ProxyID
	}
	// Unlike an interactive gateway stream, a background generation must stop
	// on timeout/shutdown and use its generation budget rather than a shorter
	// interactive response-header setting. All overrides remain process-local.
	ctx = context.WithValue(ctx, boundUpstreamLifecycleContextKey{}, true)
	ctx = context.WithValue(ctx, upstreamResponseReadLimitContextKey{}, int64(intelligenceResponseMaxBytes))
	ctx = WithHTTPUpstreamResponseHeaderTimeout(ctx, time.Duration(IntelligenceMonitorMaxTimeoutSeconds)*time.Second)
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	payload, _ := json.Marshal(map[string]any{
		"model":             IntelligenceMonitorModel,
		"input":             []map[string]any{{"role": "user", "content": []map[string]string{{"type": "input_text", "text": IntelligenceMonitorPrompt}}}},
		"reasoning":         map[string]string{"effort": IntelligenceMonitorReasoning},
		"max_output_tokens": 24000, "stream": false, "store": false,
	})
	request, _ := http.NewRequestWithContext(ctx, http.MethodPost, "http://127.0.0.1/v1/responses", bytes.NewReader(payload))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json")
	request.Header.Set("User-Agent", "Sub2API-IntelligenceMonitor/1")
	request.RemoteAddr = "127.0.0.1:0"
	writer := &intelligenceResponseWriter{header: make(http.Header), status: http.StatusOK, limit: intelligenceResponseMaxBytes, cancel: cancel, closed: make(chan bool)}
	stopNotify := context.AfterFunc(ctx, func() { close(writer.closed) })
	defer stopNotify()
	ginCtx, _ := gin.CreateTestContext(writer)
	ginCtx.Request = request
	SetOpenAIClientTransport(ginCtx, OpenAIClientTransportHTTP)
	result, forwardErr := s.oauthForward.Forward(ctx, ginCtx, &selected, payload)
	status := writer.status
	if writer.overflow || errors.Is(forwardErr, ErrUpstreamResponseBodyTooLarge) {
		return &status, "", "generation response exceeded the 4 MiB limit"
	}
	if ctx.Err() != nil {
		return &status, "", "generation cancelled or exceeded its configured time limit"
	}
	if forwardErr != nil || result == nil {
		// Gateway errors may include provider diagnostics. Persist only a safe
		// status; do not copy arbitrary error strings or provider response bodies.
		var failover *UpstreamFailoverError
		if errors.As(forwardErr, &failover) && failover.StatusCode > 0 {
			status = failover.StatusCode
		}
		if status >= 400 {
			return &status, "", fmt.Sprintf("OAuth generation returned HTTP %d", status)
		}
		return nil, "", "OAuth generation failed; the same request was not scheduled again"
	}
	if status < 200 || status >= 300 {
		return &status, "", fmt.Sprintf("OAuth generation returned HTTP %d", status)
	}
	run.SourceSnapshot["actual_model"] = result.UpstreamModel
	run.SourceSnapshot["response_model"] = result.UpstreamResponseModel
	if result.ReasoningEffort != nil {
		run.SourceSnapshot["actual_reasoning_effort"] = *result.ReasoningEffort
	}
	text, message := extractIntelligenceModelText(writer.body.Bytes(), MonitorAPIModeResponses, strings.Contains(writer.header.Get("Content-Type"), "text/event-stream"))
	text = logredact.RedactText(text)
	if result.UpstreamModel != "" && result.UpstreamModel != IntelligenceMonitorModel {
		message = "OAuth gateway used a different model than the fixed comparison model"
	}
	if result.ReasoningEffort != nil && *result.ReasoningEffort != IntelligenceMonitorReasoning {
		message = "OAuth gateway used a different reasoning effort than the fixed comparison setting"
	}
	return &status, text, message
}

// Implements the optional HTTP interfaces used by Gin as well as bounded body
// capture. CloseNotify follows the task context; no client connection exists.
type intelligenceResponseWriter struct {
	header   http.Header
	status   int
	written  bool
	body     bytes.Buffer
	limit    int
	overflow bool
	cancel   context.CancelFunc
	closed   chan bool
}

func (w *intelligenceResponseWriter) Header() http.Header { return w.header }
func (w *intelligenceResponseWriter) WriteHeader(status int) {
	if !w.written {
		w.status, w.written = status, true
	}
}
func (w *intelligenceResponseWriter) Write(p []byte) (int, error) {
	w.WriteHeader(http.StatusOK)
	if len(p) > w.limit-w.body.Len() {
		w.overflow = true
		w.cancel()
		return 0, ErrUpstreamResponseBodyTooLarge
	}
	return w.body.Write(p)
}
func (w *intelligenceResponseWriter) WriteString(value string) (int, error) {
	return w.Write([]byte(value))
}
func (w *intelligenceResponseWriter) Flush()                   { w.WriteHeader(http.StatusOK) }
func (w *intelligenceResponseWriter) CloseNotify() <-chan bool { return w.closed }
func (w *intelligenceResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	return nil, nil, errors.New("background intelligence generations do not support connection hijacking")
}
