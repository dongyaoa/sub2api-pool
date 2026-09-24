package service

import (
	"bufio"
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
	payload := map[string]any{"model": IntelligenceMonitorModel, "input": IntelligenceMonitorPrompt, "reasoning": map[string]string{"effort": IntelligenceMonitorReasoning}, "max_output_tokens": 24000, "stream": false}
	if run.APIMode == MonitorAPIModeChatCompletions {
		path = "/v1/chat/completions"
		payload = map[string]any{"model": IntelligenceMonitorModel, "messages": []map[string]string{{"role": "user", "content": IntelligenceMonitorPrompt}}, "reasoning_effort": IntelligenceMonitorReasoning, "max_completion_tokens": 24000, "stream": false}
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
	req.Header.Set("User-Agent", "Sub2API-IntelligenceMonitor/1")
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
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 8192))
		return &status, "", fmt.Sprintf("generation endpoint returned HTTP %d", status)
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, intelligenceResponseMaxBytes+1))
	if err != nil {
		return &status, "", "generation response was interrupted"
	}
	if len(raw) > intelligenceResponseMaxBytes {
		return &status, "", "generation response exceeded the 4 MiB limit"
	}
	text, message := extractIntelligenceModelText(raw, run.APIMode, strings.Contains(response.Header.Get("Content-Type"), "text/event-stream"))
	// Generated text is retained for comparison, but echoed credentials never are.
	text = strings.ReplaceAll(text, key, "[REDACTED]")
	return &status, text, message
}

func extractIntelligenceModelText(raw []byte, mode string, stream bool) (string, string) {
	// A gateway may assemble OAuth SSE into JSON while preserving the upstream
	// content type. Valid complete JSON takes precedence over that stale header.
	if gjson.ValidBytes(raw) {
		stream = false
	}
	if stream || bytes.HasPrefix(bytes.TrimSpace(raw), []byte("data:")) || bytes.HasPrefix(bytes.TrimSpace(raw), []byte("event:")) {
		var deltas strings.Builder
		final := ""
		message := ""
		completed := false
		scanner := bufio.NewScanner(bytes.NewReader(raw))
		scanner.Buffer(make([]byte, 4096), intelligenceResponseMaxBytes)
		for scanner.Scan() {
			line := strings.TrimSpace(scanner.Text())
			if !strings.HasPrefix(line, "data:") {
				continue
			}
			data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if data == "[DONE]" {
				completed = true
				continue
			}
			if !gjson.Valid(data) {
				continue
			}
			event := gjson.Get(data, "type").String()
			switch event {
			case "response.output_text.delta":
				_, _ = deltas.WriteString(gjson.Get(data, "delta").String())
			case "response.completed", "response.done":
				completed = true
				final = extractOpenAIResponsesText([]byte(gjson.Get(data, "response").Raw))
			case "response.failed", "response.incomplete", "error":
				message = "model generation failed or returned an incomplete result"
			default:
				if mode == MonitorAPIModeChatCompletions {
					_, _ = deltas.WriteString(gjson.Get(data, "choices.0.delta.content").String())
					finish := gjson.Get(data, "choices.0.finish_reason").String()
					if finish == "stop" {
						completed = true
					} else if finish != "" {
						message = "model output was truncated or filtered"
					}
				}
			}
		}
		if scanner.Err() != nil {
			message = "generation event stream could not be read"
		}
		if !completed && message == "" {
			message = "generation stream ended before completion"
		}
		if final != "" {
			return final, message
		}
		if deltas.Len() == 0 && message == "" {
			message = "model returned no text"
		}
		return deltas.String(), message
	}
	if !gjson.ValidBytes(raw) {
		return "", "generation endpoint did not return valid JSON"
	}
	if gjson.GetBytes(raw, "error").Exists() {
		return "", "model generation returned an error"
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
		return text, "model generation failed or returned an incomplete result"
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
