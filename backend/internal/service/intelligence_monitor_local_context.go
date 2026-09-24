package service

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

const intelligenceLocalRequestHeader = "X-Sub2api-Intelligence-Request"

type intelligenceGenerationContextKey struct{}

type intelligenceLocalPermit struct {
	ctx         context.Context
	deadline    time.Time
	keyID       int64
	fingerprint [sha256.Size]byte
	method      string
	path        string
}

var intelligenceLocalPermits = struct {
	sync.Mutex
	entries map[string]intelligenceLocalPermit
}{entries: make(map[string]intelligenceLocalPermit)}

// AuthorizeIntelligenceLocalRequest transfers one worker's bounded generation
// context across its loopback HTTP request. The opaque token is useful only in
// this process and with the actual monitoring key after normal authentication.
// Callers must defer the returned cleanup even when HTTP dispatch fails.
func AuthorizeIntelligenceLocalRequest(request *http.Request, keyID int64, key string) (func(), error) {
	if request == nil || request.URL == nil || request.Method != http.MethodPost ||
		(request.URL.Path != "/v1/responses" && request.URL.Path != "/v1/chat/completions") ||
		request.URL.RawQuery != "" || keyID <= 0 || key == "" {
		return nil, errors.New("invalid internal intelligence request")
	}
	address := net.ParseIP(request.URL.Hostname())
	deadline, bounded := request.Context().Deadline()
	if address == nil || !address.IsLoopback() || !bounded || request.Context().Err() != nil || !time.Now().Before(deadline) {
		return nil, errors.New("internal intelligence request requires loopback and a live deadline")
	}
	if time.Until(deadline) > time.Duration(IntelligenceMonitorMaxTimeoutSeconds)*time.Second {
		return nil, errors.New("internal intelligence deadline exceeds the generation limit")
	}
	var random [32]byte
	if _, err := rand.Read(random[:]); err != nil {
		return nil, err
	}
	token := hex.EncodeToString(random[:])
	permit := intelligenceLocalPermit{ctx: request.Context(), deadline: deadline, keyID: keyID, fingerprint: sha256.Sum256([]byte(key)), method: request.Method, path: request.URL.Path}
	intelligenceLocalPermits.Lock()
	intelligenceLocalPermits.entries[token] = permit
	intelligenceLocalPermits.Unlock()
	remove := func() {
		intelligenceLocalPermits.Lock()
		delete(intelligenceLocalPermits.entries, token)
		intelligenceLocalPermits.Unlock()
	}
	stop := context.AfterFunc(request.Context(), remove)
	if request.Header == nil {
		request.Header = make(http.Header)
	}
	request.Header.Set(intelligenceLocalRequestHeader, token)
	return func() { stop(); remove() }, nil
}

// BindIntelligenceLocalRequest must run only after ordinary key, IP, user,
// group and billing authentication succeeds. Client headers alone never grant
// a timeout override. Consume and strip the token before gateway forwarding.
func BindIntelligenceLocalRequest(request *http.Request, key *APIKey) (context.Context, context.CancelFunc) {
	if request == nil {
		return context.Background(), func() {}
	}
	ctx := request.Context()
	token := request.Header.Get(intelligenceLocalRequestHeader)
	request.Header.Del(intelligenceLocalRequestHeader)
	if len(token) != 64 {
		return ctx, func() {}
	}
	intelligenceLocalPermits.Lock()
	permit, exists := intelligenceLocalPermits.entries[token]
	delete(intelligenceLocalPermits.entries, token)
	intelligenceLocalPermits.Unlock()
	host, _, err := net.SplitHostPort(request.RemoteAddr)
	address := net.ParseIP(host)
	if !exists || err != nil || address == nil || !address.IsLoopback() || key == nil ||
		permit.keyID != key.ID || permit.fingerprint != sha256.Sum256([]byte(key.Key)) ||
		request.URL == nil || permit.method != request.Method || permit.path != request.URL.Path || request.URL.RawQuery != "" ||
		permit.ctx.Err() != nil || !time.Now().Before(permit.deadline) {
		return ctx, func() {}
	}
	ctx, cancel := context.WithDeadline(ctx, permit.deadline)
	stop := context.AfterFunc(permit.ctx, cancel)
	ctx = context.WithValue(ctx, intelligenceGenerationContextKey{}, true)
	ctx = context.WithValue(ctx, boundUpstreamLifecycleContextKey{}, true)
	ctx = WithHTTPUpstreamResponseHeaderTimeout(ctx, time.Duration(IntelligenceMonitorMaxTimeoutSeconds)*time.Second)
	return ctx, func() { stop(); cancel() }
}

// Only an authenticated local IQ request replaces the ordinary stream-idle
// guard. Its original worker deadline still bounds the entire HTTP lifecycle.
func intelligenceMonitorStreamInterval(c *gin.Context, ordinary time.Duration) time.Duration {
	if c == nil || c.Request == nil || c.Request.Context().Value(intelligenceGenerationContextKey{}) != true {
		return ordinary
	}
	deadline, ok := c.Request.Context().Deadline()
	if !ok {
		return ordinary
	}
	if remaining := time.Until(deadline); remaining > 0 {
		return remaining
	}
	return time.Nanosecond
}
