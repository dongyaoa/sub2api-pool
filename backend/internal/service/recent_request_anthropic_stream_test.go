package service

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"testing/iotest"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestRecentRequestAnthropicAdaptersObserveStreamErrors(t *testing.T) {
	handlers := []struct {
		name string
		call func(*GatewayService, *http.Response, *gin.Context) (*ForwardResult, error)
	}{
		{"responses buffered", func(s *GatewayService, r *http.Response, c *gin.Context) (*ForwardResult, error) {
			return s.handleResponsesBufferedStreamingResponse(r, c, "model", "model", nil, time.Now(), apicompat.ResponsesClientToolMapping{})
		}},
		{"responses streaming", func(s *GatewayService, r *http.Response, c *gin.Context) (*ForwardResult, error) {
			return s.handleResponsesStreamingResponse(r, c, "model", "model", nil, time.Now(), apicompat.ResponsesClientToolMapping{})
		}},
		{"chat buffered", func(s *GatewayService, r *http.Response, c *gin.Context) (*ForwardResult, error) {
			return s.handleCCBufferedFromAnthropic(r, c, "model", "model", nil, time.Now())
		}},
		{"chat streaming", func(s *GatewayService, r *http.Response, c *gin.Context) (*ForwardResult, error) {
			return s.handleCCStreamingFromAnthropic(r, c, "model", "model", nil, time.Now())
		}},
	}
	const started = "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"msg_test\",\"type\":\"message\",\"role\":\"assistant\",\"model\":\"model\",\"content\":[],\"usage\":{\"input_tokens\":1}}}\n\n"
	for _, handler := range handlers {
		for _, scenario := range []string{"error event", "read error", "complete", "missing terminal"} {
			t.Run(handler.name+"/"+scenario, func(t *testing.T) {
				c, store, account := newRecentObserverTest(t)
				finish := beginAccountRecentRequest(c.Request.Context(), c, store, account, "model")
				var reader io.Reader
				var message string
				switch scenario {
				case "error event":
					message = "upstream overloaded"
					reader = strings.NewReader(started + "event: error\ndata: {\"type\":\"error\",\"error\":{\"type\":\"overloaded_error\",\"message\":\"upstream overloaded\"}}\n\n")
				case "read error":
					message = "connection reset after first event"
					reader = io.MultiReader(strings.NewReader(started), iotest.ErrReader(errors.New(message)))
				case "complete":
					reader = strings.NewReader(started + "event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n")
				case "missing terminal":
					message = "Upstream stream ended before message_stop"
					reader = strings.NewReader(started)
				}
				resp := &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(reader)}
				result, err := handler.call(&GatewayService{}, resp, c)
				require.NoError(t, err)
				require.NotNil(t, result)
				finish(result != nil, err)
				record := takeRecentObserverRecord(t, store)
				require.Equal(t, scenario == "complete", record.Success)
				require.Equal(t, http.StatusOK, record.StatusCode)
				require.Equal(t, message, record.ErrorMessage)
				require.Empty(t, store.records)
			})
		}
	}
}
