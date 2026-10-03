package service

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

type poolSystemOneUpstream struct {
	HTTPUpstream
	do func(*http.Request, string, int64, int) (*http.Response, error)
}

func (u *poolSystemOneUpstream) Do(req *http.Request, proxyURL string, accountID int64, concurrency int) (*http.Response, error) {
	return u.do(req, proxyURL, accountID, concurrency)
}

func TestSystemOnePoolRouteAndRecentRequest(t *testing.T) {
	for _, status := range []int{http.StatusOK, http.StatusBadRequest, http.StatusServiceUnavailable} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			c := newSystemOneTestContext()
			store := &recentObserverStore{records: make(chan RecentRequestRecord, 5)}
			proxy := &Proxy{ID: 41, Name: "selected route", Protocol: "http", Host: "proxy.example", Port: 3128, Status: StatusActive}
			account := &Account{
				ID: 27, Platform: PlatformTypeSafe, Type: AccountTypeAPIKey, Concurrency: 3,
				Credentials: map[string]any{"api_key": "ts-secret"},
				ProxyPool:   []AccountProxyPoolEntry{{ProxyID: proxy.ID, Proxy: proxy, Concurrency: 3}},
			}
			called := false
			svc := newSystemOneTestService(&poolSystemOneUpstream{do: func(req *http.Request, proxyURL string, accountID int64, concurrency int) (*http.Response, error) {
				called = true
				require.Equal(t, proxy.URL(), proxyURL)
				require.Equal(t, account.ID, accountID)
				require.Equal(t, account.Concurrency, concurrency)
				require.Equal(t, "Bearer ts-secret", req.Header.Get("Authorization"))
				body := `{"model":"jev-1.13.0","answers":{},"usage":{"input_tokens":12}}`
				if status != http.StatusOK {
					body = `{"error":{"message":"request rejected"}}`
				}
				return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}, nil
			}})
			svc.cache = store
			selected, err := svc.hydrateSelectedAccount(context.Background(), account)
			require.NoError(t, err)
			result, err := svc.ForwardSystemOne(c.Request.Context(), c, selected, []byte(`{"model":"jev-latest","state":"example","questions":{}}`))
			require.True(t, called)
			if status == http.StatusOK {
				require.NoError(t, err)
				require.Equal(t, 12, result.Usage.InputTokens)
			} else {
				require.Error(t, err)
				require.Nil(t, result)
			}
			record := takeRecentObserverRecord(t, store)
			require.Equal(t, account.ID, record.AccountID)
			require.Equal(t, PlatformTypeSafe, record.Platform)
			require.Equal(t, "jev-latest", record.Model)
			require.Equal(t, proxy.ID, record.ProxyID)
			require.Equal(t, proxy.Name, record.ProxyName)
			require.Equal(t, status == http.StatusOK, record.Success)
			require.Equal(t, status, record.StatusCode)
			require.Empty(t, store.records, "one upstream attempt produces one history entry")
		})
	}
}
