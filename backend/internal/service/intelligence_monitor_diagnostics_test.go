//go:build unit

package service

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/stretchr/testify/require"
)

func TestIntelligenceGenerationHTTPErrorKeepsSelectedDiagnostics(t *testing.T) {
	message := intelligenceGenerationHTTPError(502, http.Header{
		"X-Request-Id": {"req_123-a"}, "Cf-Ray": {"a89f-EWR"}, "Authorization": {"Bearer hidden"},
	}, []byte(`{"error":{"message":"upstream closed the connection","type":"upstream_error","code":"connection_closed","request":{"api_key":"do-not-persist"}},"payload":"do-not-persist"}`), "")
	require.Equal(t, "generation endpoint returned HTTP 502: upstream closed the connection; type=upstream_error; code=connection_closed; x-request-id=req_123-a; cf-ray=a89f-EWR", message)
	require.NotContains(t, message, "do-not-persist")
	require.NotContains(t, message, "hidden")
}

func TestIntelligenceGenerationHTTPErrorDiscardsNonJSONAndUnselectedBodies(t *testing.T) {
	for _, raw := range []string{
		"unauthorized key=actual-secret",
		"<html><body>502 actual-secret</body></html>",
		`"actual-secret"`, `null`, `["actual-secret"]`,
		`{"data":{"message":"actual-secret"},"error":{"message":{"key":"actual-secret"}}}`,
		`{"error":{"message":"<html><body>error</body></html>"}}`,
		`{"error":{"message":"<h1>Bad Gateway</h1><p>private diagnostics</p>"}}`,
		`{"message":"{"}`,
		`{"message":"` + strings.Repeat("x", intelligenceDiagnosticMaxJSONBytes) + `"}`,
	} {
		t.Run(raw[:min(len(raw), 50)], func(t *testing.T) {
			require.Equal(t, "generation endpoint returned HTTP 401", intelligenceGenerationHTTPError(401, nil, []byte(raw), "actual-secret"))
		})
	}
}

func TestIntelligenceGenerationDiagnosticsRedactCredentialsBeforeTruncation(t *testing.T) {
	secret := "actual-private-key"
	detail := "request " + secret + "; Bearer bearer-secret; Basic basic-secret; authorization: Token custom-secret; api_key='quoted secret'; refresh_token=refresh-secret; access_token=access-secret; cookie=private-cookie; " +
		"https://user:password@example.com/v1/responses?token=query-secret&user=name#fragment; ?custom=unknown-secret&another=other-secret; alice@example.com; " +
		"sk-proj-abcdefghijklmnopqrstuvwxyz; xai-abcdefghijklmno; eyJabcdefghijk.abcdefghijk.abcdefghijk; final detail"
	raw, err := json.Marshal(map[string]any{"error": map[string]any{"message": detail, "code": secret, "type": "sk-abcdefghijklmnopqrst"}})
	require.NoError(t, err)
	message := intelligenceGenerationHTTPError(502, nil, raw, secret)
	for _, sensitive := range []string{secret, "bearer-secret", "basic-secret", "custom-secret", "quoted secret", "refresh-secret", "access-secret", "private-cookie", "password", "query-secret", "unknown-secret", "other-secret", "alice@example.com", "abcdefghijklmnopqrstuvwxyz", "abcdefghijklmno", "eyJabcdefghijk.abcdefghijk.abcdefghijk"} {
		require.NotContains(t, message, sensitive)
	}
	require.Contains(t, message, "final detail")
	require.Contains(t, message, "REDACTED")
	require.LessOrEqual(t, len(message), intelligenceDiagnosticMaxBytes)
}

func TestIntelligenceGenerationRequestIDsRejectUnsafeValues(t *testing.T) {
	for _, id := range []string{"actual-key", "sk-abcdefghijklmnopqrst", "eyJabcdefghijk.abcdefghijk.abcdefghijk", "alice@example.com", "Bearer hidden", "req\r\nAuthorization: hidden", strings.Repeat("a", 129), "req/key", "req?key=hidden"} {
		t.Run(id, func(t *testing.T) {
			require.Equal(t, "generation endpoint returned HTTP 502", intelligenceGenerationHTTPError(502, http.Header{"X-Request-Id": {id}}, nil, "actual-key"))
		})
	}
	require.Equal(t, "generation endpoint returned HTTP 502: x-request-id=550e8400-e29b-41d4-a716-446655440000", intelligenceGenerationHTTPError(502, http.Header{"X-Request-Id": {"550e8400-e29b-41d4-a716-446655440000"}}, []byte("<html>error</html>"), ""))
}

func TestIntelligenceGenerationEventErrorExtractsFailedAndIncompleteResponses(t *testing.T) {
	for _, tc := range []struct{ name, raw, want string }{
		{"failed", `{"type":"response.failed","response":{"error":{"message":"upstream reset","type":"server_error","code":"connection_reset"},"output":[{"content":"ignored"}]}}`, "generation failed: upstream reset; type=server_error; code=connection_reset"},
		{"error", `{"type":"error","message":"rate limited","code":429}`, "generation failed: rate limited; code=429"},
		{"reason", `{"reason":"no healthy model route"}`, "generation failed: no healthy model route"},
		{"incomplete", `{"type":"response.incomplete","response":{"incomplete_details":{"reason":"max_output_tokens"}}}`, "generation failed: max_output_tokens"},
		{"nested", `{"error":{"message":"{\"error\":{\"message\":\"nested failure\"},\"api_key\":\"ignored-secret\"}"}}`, "generation failed: nested failure"},
		{"empty", `{"type":"response.failed","response":{"output":[{"content":"ignored"}]}}`, "generation failed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, intelligenceGenerationEventError("generation failed", []byte(tc.raw), ""))
		})
	}
}

func TestIntelligenceGenerationDiagnosticsRemainBoundedUTF8AndSingleLine(t *testing.T) {
	raw, err := json.Marshal(map[string]any{"message": strings.Repeat("请求失败", 1000) + "\r\n\t\u202e", "type": strings.Repeat("上游类型", 200), "code": strings.Repeat("错误编码", 200)})
	require.NoError(t, err)
	message := intelligenceGenerationHTTPError(502, http.Header{"X-Request-Id": {strings.Repeat("a", 128)}, "Request-Id": {strings.Repeat("b", 128)}, "Cf-Ray": {strings.Repeat("c", 128)}}, raw, "")
	require.True(t, utf8.ValidString(message))
	require.LessOrEqual(t, len(message), intelligenceDiagnosticMaxBytes)
	require.NotContains(t, message, "\n")
	require.NotContains(t, message, "\r")
	require.NotContains(t, message, "\u202e")
	require.Contains(t, message, "...")
	message = intelligenceGenerationEventError("generation failed", []byte(`{"message":"first\nsecond\u202ethird"}`), "")
	require.Equal(t, "generation failed: first second third", message)
	message = intelligenceGenerationEventError("generation failed", append(append([]byte(`{"message":"bad `), byte(0xff)), []byte(` text"}`)...), "")
	require.True(t, utf8.ValidString(message))
}
