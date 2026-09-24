package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/securityaudit"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type handlerPromptEngine struct {
	mu sync.Mutex

	mode      securityaudit.Mode
	decision  *securityaudit.PromptDecision
	err       error
	evaluated int
	enqueued  int
	requests  []securityaudit.Request
}

func (e *handlerPromptEngine) EffectiveMode() securityaudit.Mode { return e.mode }
func (e *handlerPromptEngine) Enqueue(_ context.Context, req securityaudit.Request) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.enqueued++
	e.requests = append(e.requests, req.Clone())
	return e.err
}
func (e *handlerPromptEngine) Evaluate(_ context.Context, req securityaudit.Request) (*securityaudit.PromptDecision, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.evaluated++
	e.requests = append(e.requests, req.Clone())
	return e.decision, e.err
}
func TestSecurityAuditBlockingFailuresLeaveAllDownstreamCountersAtZero(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, kind := range []securityaudit.DecisionKind{securityaudit.DecisionBlock, securityaudit.DecisionUnavailable, securityaudit.DecisionInvalid} {
		t.Run(string(kind), func(t *testing.T) {
			promptDecision := promptGuardDecision(kind)
			engine := &handlerPromptEngine{mode: securityaudit.ModeBlocking, decision: &securityaudit.PromptDecision{
				Kind: kind, ErrorCode: promptDecision.ErrorCode, AllowNextStage: false,
			}}
			coordinator := securityaudit.NewCoordinator(nil, engine)
			recorder := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(recorder)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"gpt-test","messages":[{"role":"user","content":"guard me"}]}`))
			groupID := int64(3)
			apiKey := &service.APIKey{ID: 9, UserID: 7, GroupID: &groupID, Group: &service.Group{ID: groupID, Platform: service.PlatformOpenAI}}
			subject := middleware2.AuthSubject{UserID: 7, Concurrency: 2}
			decision := runSecurityAudit(c, nil, coordinator, nil, apiKey, subject, service.ContentModerationProtocolOpenAIChat, "gpt-test", []byte(`{"messages":[{"role":"user","content":"guard me"}]}`), "http")
			require.NotNil(t, decision)
			require.False(t, decision.AllowNextStage)
			require.False(t, recorder.Result().Header.Get("Content-Type") != "", "Guard evaluation itself must not start SSE/HTTP output")

			accountSelections, billingChecks, billingPreconsumes, upstreamDispatches := 0, 0, 0, 0
			if decision.AllowNextStage {
				accountSelections++
				billingChecks++
				billingPreconsumes++
				upstreamDispatches++
			}
			require.Zero(t, accountSelections)
			require.Zero(t, billingChecks)
			require.Zero(t, billingPreconsumes)
			require.Zero(t, upstreamDispatches)
			(&OpenAIGatewayHandler{}).openAISecurityAuditError(c, decision)
			require.Equal(t, promptDecision.HTTPStatus, recorder.Code)
		})
	}
}
