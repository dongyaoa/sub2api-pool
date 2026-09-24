package repository

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestIntelligenceMonitorTransportSeparatesHeaderBudgetFromInteractiveClients(t *testing.T) {
	for _, tls := range []bool{false, true} {
		t.Run(map[bool]string{false: "normal", true: "TLS fingerprint"}[tls], func(t *testing.T) {
			cfg := &config.Config{Gateway: config.GatewayConfig{ResponseHeaderTimeout: 600, OpenAIResponseHeaderTimeout: 300, OpenAIHTTP2: config.GatewayOpenAIHTTP2Config{Enabled: true}}}
			upstream, ok := NewHTTPUpstream(cfg).(*httpUpstreamService)
			require.True(t, ok)
			get := func(budget ...time.Duration) *upstreamClientEntry {
				var entry *upstreamClientEntry
				var err error
				if tls {
					entry, err = upstream.getClientEntryWithTLS("http://proxy.example:3128", 55, 2, &tlsfingerprint.Profile{Name: "monitor fixture"}, service.HTTPUpstreamProfileOpenAI, false, false, budget...)
				} else {
					entry, err = upstream.getClientEntry("http://proxy.example:3128", 55, 2, service.HTTPUpstreamProfileOpenAI, false, false, budget...)
				}
				require.NoError(t, err)
				return entry
			}
			ordinary := get()
			monitor := get(15 * time.Minute)
			require.NotSame(t, ordinary, monitor)
			ordinaryTransport, ok := ordinary.client.Transport.(*http.Transport)
			require.True(t, ok)
			monitorTransport, ok := monitor.client.Transport.(*http.Transport)
			require.True(t, ok)
			require.Equal(t, 5*time.Minute, ordinaryTransport.ResponseHeaderTimeout)
			require.Equal(t, 15*time.Minute, monitorTransport.ResponseHeaderTimeout)
			require.Same(t, ordinary, get(), "monitor creation must not replace the interactive cached client")
			require.Same(t, monitor, get(15*time.Minute))
			require.Equal(t, ordinary.proxyKey, monitor.proxyKey)
			require.Equal(t, ordinary.protocolMode, monitor.protocolMode)
			require.Equal(t, ordinaryTransport.ForceAttemptHTTP2, monitorTransport.ForceAttemptHTTP2)
			require.Zero(t, monitor.client.Timeout, "the immutable run deadline governs full request duration")
			require.Equal(t, 300, cfg.Gateway.OpenAIResponseHeaderTimeout)
		})
	}
}

func TestIntelligenceMonitorTransportReadsBoundInternalBudgetAtDispatch(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Empty(t, r.Header.Get("X-Response-Header-Timeout"))
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(target.Close)
	cfg := &config.Config{Gateway: config.GatewayConfig{OpenAIResponseHeaderTimeout: 300}}
	upstream, ok := NewHTTPUpstream(cfg).(*httpUpstreamService)
	require.True(t, ok)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Minute)
	defer cancel()
	ctx = service.WithHTTPUpstreamResponseHeaderTimeout(ctx, 15*time.Minute)
	ctx = service.WithHTTPUpstreamProfile(ctx, service.HTTPUpstreamProfileOpenAI)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target.URL, nil)
	require.NoError(t, err)
	resp, err := upstream.Do(req, "", 55, 1)
	require.NoError(t, err)
	_, err = io.Copy(io.Discard, resp.Body)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	require.Len(t, upstream.clients, 1)
	for _, entry := range upstream.clients {
		transport, ok := entry.client.Transport.(*http.Transport)
		require.True(t, ok)
		require.Equal(t, 15*time.Minute, transport.ResponseHeaderTimeout)
	}
}
