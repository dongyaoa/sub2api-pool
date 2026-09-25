//go:build unit

package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type intelligenceOAuthAccountRepo struct {
	AccountRepository
	account *Account
}

func (r *intelligenceOAuthAccountRepo) GetByID(_ context.Context, id int64) (*Account, error) {
	if r.account == nil || r.account.ID != id {
		return nil, errors.New("account unavailable")
	}
	copy := *r.account
	return &copy, nil
}

type intelligenceOAuthForwardFunc func(context.Context, *gin.Context, *Account, []byte) (*OpenAIForwardResult, error)

func (f intelligenceOAuthForwardFunc) Forward(ctx context.Context, c *gin.Context, a *Account, b []byte) (*OpenAIForwardResult, error) {
	return f(ctx, c, a, b)
}

type intelligenceOAuthSlotStub struct {
	acquired, released bool
	accountID          int64
}

func (s *intelligenceOAuthSlotStub) AcquireAccountSlotForAccount(_ context.Context, account *Account) (*AcquireResult, error) {
	s.accountID = account.ID
	return &AcquireResult{Acquired: s.acquired, ReleaseFunc: func() { s.released = true }}, nil
}
func intelligenceOAuthFixture() (*IntelligenceMonitorService, *intelligenceOAuthAccountRepo, *intelligenceOAuthSlotStub) {
	repo := &intelligenceOAuthAccountRepo{account: &Account{ID: 55, Name: "OAuth account", Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusActive, Schedulable: true, Concurrency: 1, Credentials: map[string]any{"access_token": "private-oauth-token"}}}
	svc := NewIntelligenceMonitorService(nil, upstreamTestEncryptor{}, nil, nil, nil, nil, nil)
	slots := &intelligenceOAuthSlotStub{acquired: true}
	svc.accounts, svc.oauthSlots = repo, slots
	return svc, repo, slots
}
func intelligenceOAuthRun() *IntelligenceMonitorRun {
	return &IntelligenceMonitorRun{SourceType: "openai_oauth", SourceSnapshot: map[string]any{"account_id": int64(55)}, APIMode: "responses", TimeoutSeconds: 300}
}

func TestIntelligenceOAuthUsesAccountNameAndNeverCopiesCredentials(t *testing.T) {
	svc, accounts, _ := intelligenceOAuthFixture()
	repo := &intelligenceTestRepository{}
	svc.repo = repo
	source, name, key, endpoint := "openai_oauth", "arbitrary supplied name", "do-not-save-this", "https://attacker.invalid"
	plan, err := svc.SavePlan(context.Background(), 0, 1, IntelligenceMonitorInput{SourceType: &source, AccountID: json.RawMessage(`55`), Name: &name, APIKey: &key, Endpoint: &endpoint})
	require.NoError(t, err)
	require.Equal(t, accounts.account.Name, plan.Name)
	require.Equal(t, 600, plan.TimeoutSeconds)
	require.Equal(t, 300, plan.IntervalSeconds)
	require.True(t, plan.OAuth)
	require.Equal(t, "responses", plan.APIMode)
	require.Empty(t, plan.APIKeyEncrypted)
	require.Empty(t, plan.Endpoint)
	require.Nil(t, plan.LocalAPIKeyID)
	plan.ID = 3
	repo.plan = plan
	run, err := svc.Enqueue(context.Background(), 3)
	require.NoError(t, err)
	require.Equal(t, accounts.account.Name, run.PlanName)
	require.Equal(t, 600, run.TimeoutSeconds)
	require.True(t, run.OAuth)
	require.Empty(t, run.RequestKeyEncrypted)
	encoded, err := json.Marshal(run)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "private-oauth-token")
	require.Contains(t, string(encoded), `"account_id":55`)
	gallery := &intelligenceGalleryRepository{intelligenceTestRepository: *repo}
	svc.repo = gallery
	accounts.account.Name = "renamed account"
	plans, err := svc.ListPlans(context.Background())
	require.NoError(t, err)
	require.Equal(t, "renamed account", plans[0].Name)
	require.Equal(t, "renamed account", plans[0].SourceName)
}

func TestIntelligenceOAuthRejectsOtherCredentialsAndChangedRouting(t *testing.T) {
	for _, kind := range []string{"apikey", "shadow", "synthetic", "mapped", "unsupported", "paused"} {
		t.Run(kind, func(t *testing.T) {
			svc, repo, _ := intelligenceOAuthFixture()
			called := false
			svc.oauthForward = intelligenceOAuthForwardFunc(func(context.Context, *gin.Context, *Account, []byte) (*OpenAIForwardResult, error) {
				called = true
				return nil, nil
			})
			switch kind {
			case "apikey":
				repo.account.Type = AccountTypeAPIKey
			case "shadow":
				parent := int64(7)
				repo.account.ParentAccountID = &parent
			case "synthetic":
				repo.account.Extra = map[string]any{"synthetic_ui_test": true}
			case "mapped":
				repo.account.Credentials["model_mapping"] = map[string]any{IntelligenceMonitorModel: "gpt-other"}
			case "unsupported":
				repo.account.Credentials["model_mapping"] = map[string]any{"gpt-other": "gpt-other"}
			case "paused":
				repo.account.Schedulable = false
			}
			_, _, message := svc.generateOpenAIOAuth(context.Background(), intelligenceOAuthRun())
			require.NotEmpty(t, message)
			require.False(t, called)
		})
	}
}

func TestIntelligenceOAuthSaveEnableAndExecutionRequireUsableAccount(t *testing.T) {
	future, past := time.Now().Add(time.Hour), time.Now().Add(-time.Hour)
	for _, test := range []struct {
		name   string
		change func(*Account)
	}{
		{"inactive", func(a *Account) { a.Status = "inactive" }},
		{"error", func(a *Account) { a.Status = StatusError }},
		{"paused", func(a *Account) { a.Schedulable = false }},
		{"expired", func(a *Account) { a.AutoPauseOnExpired = true; a.ExpiresAt = &past }},
		{"overloaded", func(a *Account) { a.OverloadUntil = &future }},
		{"rate limited", func(a *Account) { a.RateLimitResetAt = &future }},
		{"cooling down", func(a *Account) { a.TempUnschedulableUntil = &future }},
		{"shadow", func(a *Account) { parent := int64(3); a.ParentAccountID = &parent }},
		{"synthetic", func(a *Account) { a.Extra = map[string]any{"synthetic_ui_test": true} }},
		{"different type", func(a *Account) { a.Type = AccountTypeAPIKey }},
		{"remapped model", func(a *Account) {
			a.Credentials["model_mapping"] = map[string]any{IntelligenceMonitorModel: "another-model"}
		}},
		{"unsupported model", func(a *Account) { a.Credentials["model_mapping"] = map[string]any{"another-model": "another-model"} }},
	} {
		t.Run(test.name, func(t *testing.T) {
			svc, accounts, slots := intelligenceOAuthFixture()
			repo := &intelligenceTestRepository{}
			svc.repo = repo
			test.change(accounts.account)
			source := "openai_oauth"
			_, err := svc.SavePlan(context.Background(), 0, 1, IntelligenceMonitorInput{SourceType: &source, AccountID: json.RawMessage(`55`)})
			require.ErrorIs(t, err, ErrIntelligenceInvalid)
			require.Nil(t, repo.saved)
			repo.plan = &IntelligenceMonitorPlan{ID: 3, Name: "Existing", SourceType: source, AccountID: &accounts.account.ID, APIMode: MonitorAPIModeResponses, IntervalSeconds: 3600, TimeoutSeconds: 300}
			notes := "Update existing plan"
			_, err = svc.SavePlan(context.Background(), 3, 1, IntelligenceMonitorInput{Notes: &notes})
			require.ErrorIs(t, err, ErrIntelligenceInvalid)
			enabled := true
			_, err = svc.SavePlan(context.Background(), 3, 1, IntelligenceMonitorInput{Enabled: &enabled})
			require.ErrorIs(t, err, ErrIntelligenceInvalid, "the enabled-only fast path must revalidate the account")
			require.Nil(t, repo.saved)
			called := false
			svc.oauthForward = intelligenceOAuthForwardFunc(func(context.Context, *gin.Context, *Account, []byte) (*OpenAIForwardResult, error) {
				called = true
				return nil, nil
			})
			_, _, message := svc.generateOpenAIOAuth(context.Background(), intelligenceOAuthRun())
			require.NotEmpty(t, message)
			require.False(t, called)
			require.Zero(t, slots.accountID, "unavailable accounts must fail before concurrency acquisition")
			enabled = false
			_, err = svc.SavePlan(context.Background(), 3, 1, IntelligenceMonitorInput{Enabled: &enabled})
			require.NoError(t, err, "pausing remains possible when the source becomes unusable")
			require.False(t, repo.saved.Enabled)
		})
	}
}

func TestIntelligenceOAuthEnableAcceptsRecoveredAccountAndPauseMissingAccount(t *testing.T) {
	svc, accounts, _ := intelligenceOAuthFixture()
	past, future := time.Now().Add(-time.Minute), time.Now().Add(time.Hour)
	accounts.account.OverloadUntil = &past
	accounts.account.RateLimitResetAt = &past
	accounts.account.TempUnschedulableUntil = &past
	accounts.account.AutoPauseOnExpired = true
	accounts.account.ExpiresAt = &future
	id := accounts.account.ID
	repo := &intelligenceTestRepository{plan: &IntelligenceMonitorPlan{ID: 3, Name: "Existing", SourceType: "openai_oauth", AccountID: &id}}
	svc.repo = repo
	enabled := true
	plan, err := svc.SavePlan(context.Background(), 3, 1, IntelligenceMonitorInput{Enabled: &enabled})
	require.NoError(t, err)
	require.True(t, plan.Enabled)
	require.Equal(t, accounts.account.Name, plan.Name)
	accounts.account = nil
	_, err = svc.SavePlan(context.Background(), 3, 1, IntelligenceMonitorInput{Enabled: &enabled})
	require.ErrorIs(t, err, ErrIntelligenceInvalid)
	enabled = false
	plan, err = svc.SavePlan(context.Background(), 3, 1, IntelligenceMonitorInput{Enabled: &enabled})
	require.NoError(t, err)
	require.False(t, plan.Enabled)
}

func TestIntelligenceOAuthForwardsExactAccountPromptAndBoundedContext(t *testing.T) {
	svc, accounts, slots := intelligenceOAuthFixture()
	run := intelligenceOAuthRun()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	svc.oauthForward = intelligenceOAuthForwardFunc(func(forwardCtx context.Context, c *gin.Context, account *Account, body []byte) (*OpenAIForwardResult, error) {
		require.Equal(t, int64(55), account.ID)
		require.Equal(t, "/v1/responses", c.Request.URL.Path)
		require.Equal(t, OpenAIClientTransportHTTP, GetOpenAIClientTransport(c))
		require.Empty(t, c.GetHeader("Authorization"))
		require.Equal(t, IntelligenceMonitorModel, gjson.GetBytes(body, "model").String())
		require.Equal(t, IntelligenceMonitorReasoning, gjson.GetBytes(body, "reasoning.effort").String())
		require.Equal(t, IntelligenceMonitorPrompt, gjson.GetBytes(body, "input.0.content.0.text").String())
		bound, release := detachUpstreamContext(forwardCtx)
		defer release()
		_, hasDeadline := bound.Deadline()
		require.True(t, hasDeadline)
		select {
		case <-c.Writer.CloseNotify():
			t.Fatal("background request is prematurely closed")
		default:
		}
		require.Equal(t, "127.0.0.1", c.ClientIP())
		c.Data(200, "application/json", []byte(`{"status":"completed","output_text":"<html><svg>pelican</svg></html>"}`))
		effort := "high"
		return &OpenAIForwardResult{UpstreamModel: IntelligenceMonitorModel, ReasoningEffort: &effort}, nil
	})
	status, text, message := svc.generateOpenAIOAuth(ctx, run)
	require.Equal(t, 200, *status)
	require.Empty(t, message)
	require.Contains(t, text, "pelican")
	require.True(t, slots.released)
	require.Equal(t, int64(55), slots.accountID)
	require.Equal(t, "private-oauth-token", accounts.account.Credentials["access_token"])
	require.Equal(t, int64(55), run.SourceSnapshot["execution_account_id"])
}

func TestIntelligenceOAuthFailureIsNotRetriedAndBodyIsNotPersisted(t *testing.T) {
	svc, _, slots := intelligenceOAuthFixture()
	calls := 0
	svc.oauthForward = intelligenceOAuthForwardFunc(func(_ context.Context, c *gin.Context, _ *Account, _ []byte) (*OpenAIForwardResult, error) {
		calls++
		c.Data(401, "application/json", []byte(`{"error":"private-provider-credential"}`))
		return nil, errors.New("private-provider-credential")
	})
	status, raw, message := svc.generateOpenAIOAuth(context.Background(), intelligenceOAuthRun())
	require.Equal(t, 1, calls)
	require.Equal(t, 401, *status)
	require.Empty(t, raw)
	require.Equal(t, "OAuth generation returned HTTP 401", message)
	require.True(t, slots.released)
}

func TestIntelligenceOAuthResponseCapAndRequestReadLimit(t *testing.T) {
	svc, _, slots := intelligenceOAuthFixture()
	svc.oauthForward = intelligenceOAuthForwardFunc(func(ctx context.Context, c *gin.Context, _ *Account, _ []byte) (*OpenAIForwardResult, error) {
		_, err := ReadUpstreamResponseBody(strings.NewReader(strings.Repeat("x", intelligenceResponseMaxBytes+1)), nil, c, nil)
		require.ErrorIs(t, err, ErrUpstreamResponseBodyTooLarge)
		_, err = c.Writer.Write(bytes.Repeat([]byte("x"), intelligenceResponseMaxBytes+1))
		require.ErrorIs(t, err, ErrUpstreamResponseBodyTooLarge)
		require.ErrorIs(t, ctx.Err(), context.Canceled)
		return nil, err
	})
	_, raw, message := svc.generateOpenAIOAuth(context.Background(), intelligenceOAuthRun())
	require.Empty(t, raw)
	require.Contains(t, message, "4 MiB")
	require.True(t, slots.released)
}

func TestIntelligenceOAuthRealGatewayUsesTokenProviderAndAccountProxy(t *testing.T) {
	svc, accounts, slots := intelligenceOAuthFixture()
	proxyID := int64(9)
	accounts.account.ProxyID = &proxyID
	accounts.account.Proxy = &Proxy{ID: 9, Protocol: "http", Host: "proxy.example", Port: 3128, Status: StatusActive}
	upstream := &httpUpstreamRecorder{resp: &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader("data: {\"type\":\"response.output_text.delta\",\"delta\":\"<html><svg>pelican</svg></html>\"}\n\ndata: {\"type\":\"response.completed\",\"response\":{\"status\":\"completed\",\"output\":[],\"usage\":{\"input_tokens\":3,\"output_tokens\":9}}}\n\n"))}}
	gateway := newOpenAIImageGenerationControlTestService(upstream)
	cache := newOpenAITokenCacheStub()
	cache.tokens[OpenAITokenCacheKey(accounts.account)] = "refreshed-cache-token"
	gateway.openAITokenProvider = NewOpenAITokenProvider(accounts, cache, nil)
	gateway.accountRepo = accounts
	svc.oauthForward = gateway
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	deadline, _ := ctx.Deadline()
	status, raw, message := svc.generateOpenAIOAuth(ctx, intelligenceOAuthRun())
	require.Empty(t, message)
	require.Equal(t, 200, *status)
	require.Contains(t, raw, "<svg>pelican</svg>")
	require.Equal(t, "Bearer refreshed-cache-token", upstream.lastReq.Header.Get("Authorization"))
	require.Equal(t, "http://proxy.example:3128", upstream.lastProxyURL)
	require.Equal(t, "high", gjson.GetBytes(upstream.lastBody, "reasoning.effort").String())
	require.Equal(t, IntelligenceMonitorModel, gjson.GetBytes(upstream.lastBody, "model").String())
	require.True(t, slots.released)
	require.Len(t, upstream.requests, 1)
	upstreamDeadline, bound := upstream.lastReq.Context().Deadline()
	require.True(t, bound)
	require.Equal(t, deadline, upstreamDeadline, "the real gateway must preserve the full generation deadline")
	require.Equal(t, 15*time.Minute, HTTPUpstreamResponseHeaderTimeoutFromContext(upstream.lastReq.Context()))
}

func TestIntelligenceOAuthExecutionKeepsCapturedGenerationTimeout(t *testing.T) {
	for _, timeout := range []int{180, 240, 300, 600, 900} {
		svc, _, _ := intelligenceOAuthFixture()
		repo := &intelligenceTestRepository{}
		svc.repo = repo
		run := intelligenceOAuthRun()
		run.TimeoutSeconds = timeout
		calls := 0
		svc.oauthForward = intelligenceOAuthForwardFunc(func(ctx context.Context, c *gin.Context, _ *Account, _ []byte) (*OpenAIForwardResult, error) {
			calls++
			deadline, bound := ctx.Deadline()
			require.True(t, bound)
			require.InDelta(t, float64(timeout), time.Until(deadline).Seconds(), 2)
			c.Data(200, "application/json", []byte(`{"output_text":"<html><svg></svg></html>","status":"completed"}`))
			return &OpenAIForwardResult{UpstreamModel: IntelligenceMonitorModel}, nil
		})
		svc.execute(run)
		require.Equal(t, 1, calls)
		require.Equal(t, "succeeded", repo.completed.Status)
		require.Equal(t, timeout, repo.completed.TimeoutSeconds)
	}
}

func TestIntelligenceOAuthStopCancelsBoundUpstreamAndPersistsResult(t *testing.T) {
	svc, _, slots := intelligenceOAuthFixture()
	repo := &intelligenceTestRepository{}
	svc.repo = repo
	entered := make(chan struct{})
	svc.oauthForward = intelligenceOAuthForwardFunc(func(ctx context.Context, c *gin.Context, _ *Account, _ []byte) (*OpenAIForwardResult, error) {
		upstreamCtx, release := detachUpstreamContext(ctx)
		defer release()
		close(entered)
		<-upstreamCtx.Done()
		return nil, upstreamCtx.Err()
	})
	svc.wg.Add(1)
	go func() { defer svc.wg.Done(); svc.execute(intelligenceOAuthRun()) }()
	<-entered
	svc.Stop()
	require.NotNil(t, repo.completed)
	require.Equal(t, "failed", repo.completed.Status)
	require.Contains(t, repo.completed.Error, "cancelled")
	require.True(t, slots.released)
	// Interactive requests retain the existing detached lifecycle.
	ctx, cancel := context.WithCancel(context.Background())
	detached, release := detachUpstreamContext(ctx)
	defer release()
	cancel()
	require.NoError(t, detached.Err())
}

func TestIntelligenceOAuthRejectsUnavailableProxyAndConcurrencyWithoutForwarding(t *testing.T) {
	for _, blocked := range []string{"proxy", "pool", "concurrency"} {
		t.Run(blocked, func(t *testing.T) {
			svc, accounts, slots := intelligenceOAuthFixture()
			calls := 0
			svc.oauthForward = intelligenceOAuthForwardFunc(func(context.Context, *gin.Context, *Account, []byte) (*OpenAIForwardResult, error) {
				calls++
				return nil, nil
			})
			proxyID := int64(9)
			switch blocked {
			case "proxy":
				accounts.account.ProxyID = &proxyID // Missing relation must not silently become a direct request.
			case "pool":
				accounts.account.ProxyPool = []AccountProxyPoolEntry{{ProxyID: 9, Concurrency: 1, Proxy: &Proxy{ID: 9, Status: "disabled"}}}
			case "concurrency":
				slots.acquired = false
			}
			_, _, message := svc.generateOpenAIOAuth(context.Background(), intelligenceOAuthRun())
			require.NotEmpty(t, message)
			require.Zero(t, calls)
			require.False(t, slots.released)
		})
	}
}
