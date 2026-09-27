package service

import (
	"context"
	"net/url"
	"strings"
	"sync"
	"time"
)

type intelligenceExecutionTraceContextKey struct{}

// This collector is shared only through the authenticated, one-use loopback
// permit. The worker owns the run; gateway goroutines never mutate its snapshot.
type intelligenceExecutionTrace struct {
	mu       sync.Mutex
	account  intelligenceExecutionAccount
	attempts int
}

type intelligenceExecutionAccount struct {
	id          int64
	name        string
	accountType string
	platform    string
	baseOrigin  string
	startedAt   string
}

func newIntelligenceExecutionTrace(ctx context.Context) (context.Context, *intelligenceExecutionTrace) {
	trace := &intelligenceExecutionTrace{}
	return context.WithValue(ctx, intelligenceExecutionTraceContextKey{}, trace), trace
}

// RecordIntelligenceExecutionAccount is called immediately before a gateway
// forwarding attempt, after scheduling/profit/concurrency admission. Merely
// selecting an account must not identify it as this generation's actual source.
// Ordinary requests and forged monitoring headers cannot create a collector.
func RecordIntelligenceExecutionAccount(ctx context.Context, account *Account) {
	if ctx == nil || account == nil || account.ID <= 0 || ctx.Value(intelligenceGenerationContextKey{}) != true {
		return
	}
	trace, _ := ctx.Value(intelligenceExecutionTraceContextKey{}).(*intelligenceExecutionTrace)
	if trace == nil {
		return
	}
	current := intelligenceExecutionAccount{
		id: account.ID, name: account.Name, accountType: account.Type, platform: account.Platform,
		baseOrigin: intelligenceExecutionBaseOrigin(account.GetCredential("base_url")),
		startedAt:  time.Now().UTC().Format(time.RFC3339Nano),
	}
	trace.mu.Lock()
	trace.account = current
	trace.attempts++
	trace.mu.Unlock()
}

// Preserve only an origin from explicitly configured account metadata. Paths,
// userinfo, query strings and fragments can contain credentials and never enter
// the source snapshot. An absent/invalid base_url remains unknown, not guessed.
func intelligenceExecutionBaseOrigin(raw string) string {
	if len(raw) > 8192 {
		return ""
	}
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || parsed == nil || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.Hostname() == "" {
		return ""
	}
	return (&url.URL{Scheme: parsed.Scheme, Host: parsed.Host}).String()
}

func (t *intelligenceExecutionTrace) sourceSnapshot(snapshot map[string]any, completed bool) map[string]any {
	if t == nil {
		return snapshot
	}
	t.mu.Lock()
	account, attempts := t.account, t.attempts
	t.mu.Unlock()
	if account.id <= 0 || attempts == 0 {
		return snapshot
	}
	if snapshot == nil {
		snapshot = make(map[string]any)
	}
	snapshot["execution_account_id"] = account.id
	snapshot["execution_account_name"] = account.name
	snapshot["execution_account_type"] = account.accountType
	snapshot["execution_account_platform"] = account.platform
	snapshot["execution_account_base_origin"] = account.baseOrigin
	snapshot["execution_started_at"] = account.startedAt
	snapshot["execution_attempt_count"] = attempts
	snapshot["execution_source_status"] = "attempted"
	if completed {
		snapshot["execution_source_status"] = "completed"
	}
	return snapshot
}
