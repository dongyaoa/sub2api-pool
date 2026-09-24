package service

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"math"
	"strings"
)

// parseUpstreamUsage accepts Sub2API's raw /v1/usage response and a standard
// {data:...} envelope used by some deployments. Unrecognized/invalid values stay
// unknown; API errors and arbitrary HTTP 200 JSON are never treated as $0.
func parseUpstreamUsage(body []byte) (*UpstreamBalanceSnapshot, error) {
	var payload map[string]any
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	if err := decoder.Decode(&payload); err != nil {
		return nil, err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return nil, errors.New("multiple JSON values")
	}
	// Validate the envelope before discarding it. Some relays return HTTP 200
	// with an error plus stale data; its balance is not a successful observation.
	if upstreamUsagePayloadFailed(payload) {
		return nil, errors.New("error response")
	}
	if data, ok := payload["data"].(map[string]any); ok {
		payload = data
	}
	if upstreamUsagePayloadFailed(payload) {
		return nil, errors.New("error response")
	}
	mode, _ := payload["mode"].(string)
	snapshot := &UpstreamBalanceSnapshot{Kind: "unknown", Status: "ok"}
	quota, _ := payload["quota"].(map[string]any)
	subscription, _ := payload["subscription"].(map[string]any)
	unit, _ := payload["unit"].(string)
	if unit == "" {
		unit, _ = quota["unit"].(string)
	}
	unit = strings.ToUpper(strings.TrimSpace(unit))
	if len(unit) > 16 {
		return nil, errors.New("invalid unit")
	}
	if unit == "" {
		snapshot.Currency = "USD"
		snapshot.CurrencySource = "sub2api_default"
	} else {
		snapshot.Currency = unit
		snapshot.CurrencySource = "reported"
	}
	switch {
	case mode == "quota_limited" || quota != nil:
		snapshot.Kind = "key_quota"
		snapshot.QuotaRemaining = upstreamAmount(quota["remaining"])
		if snapshot.QuotaRemaining == nil {
			snapshot.QuotaRemaining = upstreamAmount(payload["remaining"])
		}
		// A rate-limited key without a total quota has no single remaining amount.
	case subscription != nil:
		snapshot.Kind = "subscription"
		snapshot.QuotaRemaining = upstreamAmount(payload["remaining"])
	case payload["balance"] != nil:
		snapshot.Kind = "wallet"
		snapshot.Balance = upstreamAmount(payload["balance"])
	case mode == "unrestricted":
		// remaining alone can be wallet balance OR a subscription allowance.
		// Older deployments without an explicit discriminator remain unknown.
		if name, _ := payload["planName"].(string); name == "钱包余额" || strings.EqualFold(name, "wallet balance") {
			snapshot.Kind = "wallet"
			snapshot.Balance = upstreamAmount(payload["remaining"])
		}
	}
	usage, _ := payload["usage"].(map[string]any)
	today, _ := usage["today"].(map[string]any)
	total, _ := usage["total"].(map[string]any)
	snapshot.TodayUsed = upstreamAmount(today["actual_cost"])
	snapshot.TotalUsed = upstreamAmount(total["actual_cost"])
	if snapshot.TodayUsed != nil && *snapshot.TodayUsed < 0 {
		snapshot.TodayUsed = nil
	}
	if snapshot.TotalUsed != nil && *snapshot.TotalUsed < 0 {
		snapshot.TotalUsed = nil
	}
	if snapshot.Kind == "unknown" && snapshot.TodayUsed == nil && snapshot.TotalUsed == nil {
		return nil, errors.New("unsupported usage response")
	}
	if snapshot.Kind == "wallet" && snapshot.Balance == nil {
		return nil, errors.New("invalid wallet balance")
	}
	return snapshot, nil
}

func upstreamUsagePayloadFailed(payload map[string]any) bool {
	if code, exists := payload["code"]; exists {
		number := upstreamAmount(code)
		if number == nil || (*number != 0 && *number != 200) {
			return true
		}
	}
	if value, exists := payload["error"]; exists && value != nil {
		if message, ok := value.(string); !ok || strings.TrimSpace(message) != "" {
			return true
		}
	}
	if success, ok := payload["success"].(bool); ok && !success {
		return true
	}
	if valid, ok := payload["isValid"].(bool); ok && !valid {
		return true
	}
	return false
}

func upstreamAmount(value any) *float64 {
	var amount float64
	var err error
	switch v := value.(type) {
	case json.Number:
		amount, err = v.Float64()
	case float64:
		amount = v
	case string:
		amount, err = json.Number(strings.TrimSpace(v)).Float64()
	default:
		return nil
	}
	if err != nil || math.IsNaN(amount) || math.IsInf(amount, 0) || math.Abs(amount) >= 1e14 {
		return nil
	}
	return &amount
}
