package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/tidwall/gjson"
)

const intelligenceResponseMaxBytes = 4 * 1024 * 1024

func (s *IntelligenceMonitorService) clientAndEndpoint(run *IntelligenceMonitorRun) (*http.Client, string) {
	if run.SourceType == "local_group" {
		return s.localClient, s.localEndpoint
	}
	return s.externalClient, run.SourceEndpoint
}
func (s *IntelligenceMonitorService) generate(ctx context.Context, run *IntelligenceMonitorRun, key string) (*int, string, string) {
	client, endpoint := s.clientAndEndpoint(run)
	if run.SourceType != "local_group" {
		if err := validateEndpoint(endpoint); err != nil {
			return nil, "", "upstream endpoint is unavailable or rejected by the public HTTPS policy"
		}
	}
	path := "/v1/responses"
	// Remote proxies can time out while a long non-streaming generation stays
	// silent. Receive remote generations incrementally, then store one artifact.
	// Loopback requests retain their trusted buffered path and its IQ deadline;
	// switching that path to streaming would re-enable ordinary gateway guards.
	stream := run.SourceType != "local_group"
	payload := map[string]any{"model": IntelligenceMonitorModel, "input": IntelligenceMonitorPrompt, "reasoning": map[string]string{"effort": IntelligenceMonitorReasoning}, "max_output_tokens": 24000, "stream": stream}
	if run.APIMode == MonitorAPIModeChatCompletions {
		path = "/v1/chat/completions"
		payload = map[string]any{"model": IntelligenceMonitorModel, "messages": []map[string]string{{"role": "user", "content": IntelligenceMonitorPrompt}}, "reasoning_effort": IntelligenceMonitorReasoning, "max_completion_tokens": 24000, "stream": stream}
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, "", "failed to build the fixed generation request"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, joinURL(endpoint, path), bytes.NewReader(body))
	if err != nil {
		return nil, "", "invalid generation endpoint"
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if stream {
		req.Header.Set("Accept", "text/event-stream, application/json")
	}
	req.Header.Set("User-Agent", "Sub2API-IntelligenceMonitor/1")
	if run.SourceType == "local_group" {
		cleanup, permitErr := AuthorizeIntelligenceLocalRequest(req, intelligenceSnapshotID(run.SourceSnapshot["local_api_key_id"]), key)
		if permitErr != nil {
			return nil, "", "local generation context is unavailable"
		}
		defer cleanup()
	}
	response, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return nil, "", "generation cancelled or exceeded its configured time limit"
		}
		return nil, "", "generation request failed to connect or receive a response"
	}
	defer func() { _ = response.Body.Close() }()
	status := response.StatusCode
	if status < 200 || status >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(response.Body, 16*1024))
		return &status, "", intelligenceGenerationHTTPError(status, response.Header, raw, key)
	}
	text, message := readIntelligenceGenerationBody(response.Body, run.APIMode, response.Header.Get("Content-Type"), key)
	if message != "" && ctx.Err() != nil {
		message = "generation cancelled or exceeded its configured time limit"
	}
	// Generated text is retained for comparison, but echoed credentials never are.
	text = strings.ReplaceAll(text, key, "[REDACTED]")
	return &status, text, message
}

func extractIntelligenceModelText(raw []byte, mode string, stream bool) (string, string) {
	return extractIntelligenceModelTextWithKey(raw, mode, stream, "")
}

func extractIntelligenceModelTextWithKey(raw []byte, mode string, stream bool, key string) (string, string) {
	if len(raw) > intelligenceResponseMaxBytes {
		return "", "generation response exceeded the 4 MiB limit"
	}
	// A gateway may assemble OAuth SSE into JSON while preserving the upstream
	// content type. Valid complete JSON takes precedence over that stale header.
	if gjson.ValidBytes(raw) {
		stream = false
	}
	if stream || intelligenceLooksLikeSSE(raw) {
		return readIntelligenceEventStream(bytes.NewReader(raw), mode, key)
	}
	if !gjson.ValidBytes(raw) {
		return "", "generation endpoint did not return valid JSON"
	}
	if value := gjson.GetBytes(raw, "error"); value.Exists() && value.Type != gjson.Null {
		return "", intelligenceGenerationEventError("model generation returned an error", raw, key)
	}
	if mode == MonitorAPIModeChatCompletions {
		text := gjson.GetBytes(raw, "choices.0.message.content").String()
		finish := gjson.GetBytes(raw, "choices.0.finish_reason").String()
		if text == "" {
			return "", "model returned no text"
		}
		if finish != "" && finish != "stop" {
			return text, "model output was truncated or filtered"
		}
		return text, ""
	}
	text := extractOpenAIResponsesText(raw)
	status := gjson.GetBytes(raw, "status").String()
	if status == "incomplete" || status == "failed" || status == "cancelled" {
		return text, intelligenceGenerationEventError("model generation failed or returned an incomplete result", raw, key)
	}
	if text == "" {
		return "", "model returned no text"
	}
	return text, ""
}

func extractIntelligenceHTML(raw string) string {
	text := strings.TrimSpace(raw)
	lower := strings.ToLower(text)
	if start := strings.Index(lower, "```html"); start >= 0 {
		rest := text[start+7:]
		if end := strings.Index(rest, "```"); end >= 0 {
			text = strings.TrimSpace(rest[:end])
			lower = strings.ToLower(text)
		}
	}
	start := strings.Index(lower, "<!doctype html")
	if start < 0 {
		start = strings.Index(lower, "<html")
	}
	if start >= 0 {
		if end := strings.LastIndex(lower, "</html>"); end >= start {
			return strings.TrimSpace(text[start : end+7])
		}
		return ""
	}
	// Models sometimes return a complete SVG document in place of its HTML
	// wrapper. Preserve the SVG and provide only the minimal missing wrapper.
	if start = strings.Index(lower, "<svg"); start >= 0 {
		if end := strings.LastIndex(lower, "</svg>"); end >= start {
			return "<!doctype html><html><head><meta charset=\"utf-8\"></head><body>" + text[start:end+6] + "</body></html>"
		}
	}
	return ""
}

func (s *IntelligenceMonitorService) fetchLocalBilling(ctx context.Context, run *IntelligenceMonitorRun, key string) *UpstreamRemoteBillingSnapshot {
	now := time.Now()
	groupID := intelligenceSnapshotID(run.SourceSnapshot["group_id"])
	name := run.SourceName
	snapshot := &UpstreamRemoteBillingSnapshot{GroupID: &groupID, GroupName: &name, Source: "local_billing", Status: "error", Stale: true, LastAttemptAt: &now, Error: "local billing information unavailable"}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.localEndpoint+"/v1/sub2api/billing", nil)
	if err != nil {
		return snapshot
	}
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Accept", "application/json")
	response, err := s.localClient.Do(req)
	if err != nil {
		return snapshot
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		if response.StatusCode == http.StatusNotFound {
			snapshot.Status = "unsupported"
		}
		snapshot.Error = fmt.Sprintf("local billing endpoint returned HTTP %d", response.StatusCode)
		return snapshot
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, 64*1024))
	if err != nil || !gjson.ValidBytes(raw) {
		return snapshot
	}
	parsed, err := parseUpstreamRemoteBilling(raw, "local_billing")
	if err != nil {
		return snapshot
	}
	parsed.GroupID = &groupID
	parsed.GroupName = &name
	parsed.SyncedAt = &now
	parsed.LastAttemptAt = &now
	return parsed
}
