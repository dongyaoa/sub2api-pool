package service

import (
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/Wei-Shaw/sub2api/internal/util/logredact"
	"github.com/tidwall/gjson"
)

const (
	intelligenceDiagnosticMaxJSONBytes = 64 * 1024
	intelligenceDiagnosticMaxBytes     = 1024
	intelligenceDiagnosticMessageBytes = 512
)

var (
	intelligenceDiagnosticURLPattern       = regexp.MustCompile(`(?i)\b(?:https?|wss?)://[^\s<>"']+`)
	intelligenceDiagnosticAuthPattern      = regexp.MustCompile(`(?i)\b(?:proxy-)?authorization["']?\s*[:=]\s*(?:"[^"]*"|'[^']*'|[^\r\n,;]+)`)
	intelligenceDiagnosticBearerPattern    = regexp.MustCompile(`(?i)\b(bearer|basic)\s+[^\s,;"'<>]+`)
	intelligenceDiagnosticSecretPattern    = regexp.MustCompile(`(?i)\b(?:api[_-]?key|x-api-key|access[_-]?token|refresh[_-]?token|id[_-]?token|token|client[_-]?secret|secret|password|cookie|set-cookie)["']?\s*[:=]\s*(?:"[^"]*"|'[^']*'|[^\s,;&]+)`)
	intelligenceDiagnosticQueryPattern     = regexp.MustCompile(`([?&])[a-zA-Z0-9_.%~-]+=[^&\s"'<>]+`)
	intelligenceDiagnosticEmailPattern     = regexp.MustCompile(`[^\s"'<>@,;:]+@[^\s"'<>@,;:]+`)
	intelligenceDiagnosticKeyPattern       = regexp.MustCompile(`(?i)\b(?:sk-|xai-)[a-zA-Z0-9_-]{4,}`)
	intelligenceDiagnosticJWTPattern       = regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{4,}\.[A-Za-z0-9_-]{4,}\.[A-Za-z0-9_-]{4,}`)
	intelligenceDiagnosticHTMLPattern      = regexp.MustCompile(`(?i)<(?:!doctype|/?(?:html|head|body|script|style|title|h[1-6]|div|p|pre|span|a|br))(?:\s|/?>)`)
	intelligenceDiagnosticRequestIDPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._:-]{0,127}$`)
)

// Persist only selected scalar diagnostics. Error pages, arbitrary response
// objects and request credentials must never enter a monitoring record.
func intelligenceGenerationHTTPError(status int, headers http.Header, raw []byte, key string) string {
	base := fmt.Sprintf("generation endpoint returned HTTP %d", status)
	details := intelligenceGenerationErrorDetails(raw, key)
	for _, header := range []string{"X-Request-Id", "Request-Id", "X-Amzn-Requestid", "X-Amz-Request-Id", "Cf-Ray"} {
		value := strings.TrimSpace(headers.Get(header))
		if value == "" || !intelligenceDiagnosticRequestIDPattern.MatchString(value) ||
			intelligenceSanitizeDiagnostic(value, key) != value {
			continue
		}
		details = append(details, strings.ToLower(header)+"="+value)
	}
	return intelligenceJoinDiagnostic(base, details)
}

func intelligenceGenerationEventError(base string, raw []byte, key string) string {
	return intelligenceJoinDiagnostic(base, intelligenceGenerationErrorDetails(raw, key))
}

func intelligenceGenerationErrorDetails(raw []byte, key string) []string {
	if len(raw) == 0 || len(raw) > intelligenceDiagnosticMaxJSONBytes || !gjson.ValidBytes(raw) {
		return nil
	}
	root := gjson.ParseBytes(raw)
	if !root.IsObject() {
		return nil
	}
	firstString := func(paths ...string) string {
		for _, path := range paths {
			value := root.Get(path)
			if value.Type == gjson.String && strings.TrimSpace(value.String()) != "" {
				return value.String()
			}
		}
		return ""
	}
	message := firstString("response.error.message", "error.message", "message", "reason", "response.incomplete_details.reason")
	// Some gateways put serialized JSON in error.message. Extract its selected
	// message only; never persist that embedded response wholesale.
	for depth := 0; depth < 2; depth++ {
		trimmed := strings.TrimSpace(message)
		if !strings.HasPrefix(trimmed, "{") && !strings.HasPrefix(trimmed, "[") {
			break
		}
		message = ""
		if !gjson.Valid(trimmed) {
			break
		}
		for _, path := range []string{"error.message", "message", "reason"} {
			value := gjson.Get(trimmed, path)
			if value.Type == gjson.String && strings.TrimSpace(value.String()) != "" {
				message = value.String()
				break
			}
		}
	}
	var details []string
	if message = intelligenceSanitizeDiagnostic(message, key); message != "" {
		details = append(details, intelligenceTruncateDiagnostic(message, intelligenceDiagnosticMessageBytes))
	}
	typeName := firstString("response.error.type", "error.type", "type")
	if typeName != "error" && !strings.HasPrefix(typeName, "response.") {
		if typeName = intelligenceSanitizeDiagnostic(typeName, key); typeName != "" {
			details = append(details, "type="+intelligenceTruncateDiagnostic(typeName, 96))
		}
	}
	for _, path := range []string{"response.error.code", "error.code", "code"} {
		value := root.Get(path)
		if value.Type != gjson.String && value.Type != gjson.Number {
			continue
		}
		if code := intelligenceSanitizeDiagnostic(value.String(), key); code != "" {
			details = append(details, "code="+intelligenceTruncateDiagnostic(code, 96))
			break
		}
	}
	return details
}

func intelligenceSanitizeDiagnostic(value, key string) string {
	value = strings.ToValidUTF8(value, "")
	value = strings.TrimSpace(value)
	lower := strings.ToLower(value)
	if value == "" || strings.HasPrefix(value, "{") || strings.HasPrefix(value, "[") ||
		strings.Contains(lower, "<!doctype") || intelligenceDiagnosticHTMLPattern.MatchString(value) {
		return ""
	}
	if key != "" {
		for _, secret := range []string{key, url.QueryEscape(key), url.PathEscape(key)} {
			value = strings.ReplaceAll(value, secret, "[REDACTED]")
		}
	}
	value = intelligenceDiagnosticURLPattern.ReplaceAllString(value, "[URL REDACTED]")
	value = intelligenceDiagnosticAuthPattern.ReplaceAllString(value, "authorization=[REDACTED]")
	value = intelligenceDiagnosticBearerPattern.ReplaceAllString(value, "$1 [REDACTED]")
	value = intelligenceDiagnosticSecretPattern.ReplaceAllString(value, "credential=[REDACTED]")
	value = intelligenceDiagnosticQueryPattern.ReplaceAllString(value, "$1[REDACTED]")
	value = intelligenceDiagnosticEmailPattern.ReplaceAllString(value, "[EMAIL REDACTED]")
	value = intelligenceDiagnosticKeyPattern.ReplaceAllString(value, "[REDACTED]")
	value = intelligenceDiagnosticJWTPattern.ReplaceAllString(value, "[REDACTED]")
	value = logredact.RedactText(sanitizeErrorMessage(value), "authorization", "api_key", "api-key", "token", "cookie", "secret")
	value = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
			return ' '
		}
		return r
	}, value)
	return strings.Join(strings.Fields(value), " ")
}

func intelligenceJoinDiagnostic(base string, details []string) string {
	if len(details) == 0 {
		return base
	}
	return intelligenceTruncateDiagnostic(base+": "+strings.Join(details, "; "), intelligenceDiagnosticMaxBytes)
}

func intelligenceTruncateDiagnostic(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	const suffix = "..."
	end := limit - len(suffix)
	for end > 0 && !utf8.RuneStart(value[end]) {
		end--
	}
	return value[:end] + suffix
}
