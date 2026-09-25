package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func newAPICacheFixture(t *testing.T) (*UpstreamFinanceService, *UpstreamFinanceTarget, *time.Time) {
	t.Helper()
	svc := NewUpstreamFinanceService(nil, financeTestCipher{}, nil, nil, nil)
	target := &UpstreamFinanceTarget{ID: 7, Provider: "openai", Endpoint: "https://example.com/prefix/v1", APIKeyEncrypted: "sk-key", NewAPIUserID: 42, NewAPIAccessTokenEncrypted: "console-key"}
	now := time.Date(2026, 9, 25, 0, 0, 0, 0, time.UTC)
	svc.now = func() time.Time { return now }
	return svc, target, &now
}

func newAPICacheResponse(req *http.Request, status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body)), Request: req}
}

const newAPICacheSearchFixture = `{"success":true,"data":{"total":1,"items":[{"id":13,"user_id":42,"group":"premium","remain_quota":500000,"used_quota":100000,"unlimited_quota":false}]}}`
const newAPICacheTokenFixture = `{"success":true,"data":{"id":13,"user_id":42,"group":"premium","remain_quota":250000,"used_quota":350000,"unlimited_quota":false}}`

func TestNewAPITokenCacheRefreshesLiveFieldsWithoutRepeatingSearch(t *testing.T) {
	svc, target, now := newAPICacheFixture(t)
	paths := []string{}
	svc.client.Transport = financeRoundTrip(func(req *http.Request) (*http.Response, error) {
		paths = append(paths, req.URL.Path)
		require.Equal(t, "Bearer console-key", req.Header.Get("Authorization"))
		require.Equal(t, "42", req.Header.Get("New-Api-User"))
		if strings.HasSuffix(req.URL.Path, "/search") {
			require.Equal(t, "sk-key", req.URL.Query().Get("token"))
			return newAPICacheResponse(req, 200, newAPICacheSearchFixture), nil
		}
		require.Empty(t, req.URL.RawQuery)
		return newAPICacheResponse(req, 200, newAPICacheTokenFixture), nil
	})
	first, code := svc.lookupNewAPIToken(context.Background(), "https://example.com/prefix", "sk-key", "console-key", target)
	require.Empty(t, code)
	require.Equal(t, 500000.0, *first.RemainQuota)
	second, code := svc.lookupNewAPIToken(context.Background(), "https://example.com/prefix", "sk-key", "console-key", target)
	require.Empty(t, code)
	require.Equal(t, 250000.0, *second.RemainQuota, "the cache must retain identifiers, not stale amounts")
	*now = now.Add(newAPITokenCacheTTL)
	_, code = svc.lookupNewAPIToken(context.Background(), "https://example.com/prefix", "sk-key", "console-key", target)
	require.Empty(t, code)
	require.Equal(t, []string{"/prefix/api/token/search", "/prefix/api/token/13", "/prefix/api/token/search"}, paths)
}

func TestNewAPITokenCacheDoesNotCrossCredentialOrEndpointChanges(t *testing.T) {
	for _, change := range []struct {
		name  string
		apply func(*UpstreamFinanceTarget)
	}{
		{"relay key", func(target *UpstreamFinanceTarget) { target.APIKeyEncrypted = "different-relay-key" }},
		{"console key", func(target *UpstreamFinanceTarget) { target.NewAPIAccessTokenEncrypted = "different-console-key" }},
		{"endpoint", func(target *UpstreamFinanceTarget) { target.Endpoint = "https://other.example.com/v1" }},
		{"user", func(target *UpstreamFinanceTarget) { target.NewAPIUserID = 43 }},
	} {
		t.Run(change.name, func(t *testing.T) {
			svc, target, _ := newAPICacheFixture(t)
			svc.newAPICache.rememberTokenID(upstreamBalanceIdentity(target), 13, svc.now())
			change.apply(target)
			svc.client.Transport = financeRoundTrip(func(req *http.Request) (*http.Response, error) {
				require.True(t, strings.HasSuffix(req.URL.Path, "/api/token/search"))
				body := strings.ReplaceAll(newAPICacheSearchFixture, `"user_id":42`, fmt.Sprintf(`"user_id":%d`, target.NewAPIUserID))
				return newAPICacheResponse(req, 200, body), nil
			})
			_, code := svc.lookupNewAPIToken(context.Background(), "https://example.com/prefix", "sk-key", "console-key", target)
			require.Empty(t, code)
		})
	}
}

func TestNewAPITokenCacheValidatesOwnershipAndBoundsSearch(t *testing.T) {
	for _, tc := range []struct{ name, body, code string }{
		{"no results", `{"success":true,"data":{"total":0,"items":[]}}`, "newapi_token_not_found"},
		{"multiple results", `{"success":true,"data":{"total":2,"items":[]}}`, "newapi_token_ambiguous"},
		{"missing total", `{"success":true,"data":{"items":[]}}`, "newapi_token_lookup_unsupported"},
		{"missing ID", strings.Replace(newAPICacheSearchFixture, `"id":13`, `"id":0`, 1), "newapi_account_identity_mismatch"},
		{"foreign owner", strings.Replace(newAPICacheSearchFixture, `"user_id":42`, `"user_id":8`, 1), "newapi_account_identity_mismatch"},
		{"missing group", strings.Replace(newAPICacheSearchFixture, `"group":"premium",`, "", 1), "newapi_account_identity_mismatch"},
		{"error response", `{"success":false,"message":"sk-key console-key https://private.example"}`, "newapi_token_lookup_unsupported"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, target, _ := newAPICacheFixture(t)
			svc.client.Transport = financeRoundTrip(func(req *http.Request) (*http.Response, error) { return newAPICacheResponse(req, 200, tc.body), nil })
			token, code := svc.lookupNewAPIToken(context.Background(), "https://example.com/prefix", "sk-key", "console-key", target)
			require.Nil(t, token)
			require.Equal(t, tc.code, code)
			require.Empty(t, svc.newAPICache.tokenIDs)
		})
	}
}

func TestNewAPITokenCacheInvalidatesIdentityMismatchAnd404(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		status     int
		code       string
		search     bool
	}{
		{"owner mismatch", strings.Replace(newAPICacheTokenFixture, `"user_id":42`, `"user_id":9`, 1), 200, "newapi_account_identity_mismatch", false},
		{"ID mismatch", strings.Replace(newAPICacheTokenFixture, `"id":13`, `"id":14`, 1), 200, "newapi_account_identity_mismatch", false},
		{"missing token", "sk-key console-key", 404, "", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, target, _ := newAPICacheFixture(t)
			identity := upstreamBalanceIdentity(target)
			svc.newAPICache.rememberTokenID(identity, 13, svc.now())
			searches := 0
			svc.client.Transport = financeRoundTrip(func(req *http.Request) (*http.Response, error) {
				if strings.HasSuffix(req.URL.Path, "/search") {
					searches++
					return newAPICacheResponse(req, 200, newAPICacheSearchFixture), nil
				}
				return newAPICacheResponse(req, tc.status, tc.body), nil
			})
			_, code := svc.lookupNewAPIToken(context.Background(), "https://example.com/prefix", "sk-key", "console-key", target)
			require.Equal(t, tc.code, code)
			if tc.search {
				require.Equal(t, 1, searches)
			} else {
				require.Zero(t, searches)
				require.Zero(t, svc.newAPICache.tokenID(identity, svc.now()))
			}
		})
	}
}

func TestNewAPITokenCacheFailuresNeverExposeSecretsOrRepeatSearch(t *testing.T) {
	for _, status := range []int{0, 401, 429, 502} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			svc, target, _ := newAPICacheFixture(t)
			svc.newAPICache.rememberTokenID(upstreamBalanceIdentity(target), 13, svc.now())
			svc.client.Transport = financeRoundTrip(func(req *http.Request) (*http.Response, error) {
				require.Equal(t, "/prefix/api/token/13", req.URL.Path)
				if status == 0 {
					return nil, errors.New("sk-key console-key private-host")
				}
				return newAPICacheResponse(req, status, "sk-key console-key private-host"), nil
			})
			for range 2 {
				token, code := svc.lookupNewAPIToken(context.Background(), "https://example.com/prefix", "sk-key", "console-key", target)
				require.Nil(t, token)
				require.NotEmpty(t, code)
				require.NotContains(t, code, "sk-key")
				require.NotContains(t, code, "console-key")
				require.NotContains(t, code, "private-host")
			}
		})
	}
}

func TestNewAPITokenLookupEncodesExactQueryAndRejectsWildcards(t *testing.T) {
	svc, target, _ := newAPICacheFixture(t)
	calls := 0
	svc.client.Transport = financeRoundTrip(func(req *http.Request) (*http.Response, error) {
		calls++
		require.Equal(t, "sk-a_b!+&?=#", req.URL.Query().Get("token"))
		require.Equal(t, "2", req.URL.Query().Get("page_size"))
		require.Len(t, req.URL.Query(), 3)
		require.Empty(t, req.URL.Fragment)
		return newAPICacheResponse(req, 200, newAPICacheSearchFixture), nil
	})
	token, code := svc.lookupNewAPIToken(context.Background(), "https://example.com/prefix", "sk-%", "console-key", target)
	require.Nil(t, token)
	require.Equal(t, "newapi_token_lookup_unsupported", code)
	require.Zero(t, calls)
	_, code = svc.lookupNewAPIToken(context.Background(), "https://example.com/prefix", "sk-a_b!+&?=#", "console-key", target)
	require.Empty(t, code)
	require.Equal(t, 1, calls)
}

func TestNewAPITokenLookupRejectsEmptyNormalizedSearch(t *testing.T) {
	for _, key := range []string{"", " \t\n", "sk-", "sk", "s", "k", "-", "sk-sk--"} {
		t.Run(fmt.Sprintf("%q", key), func(t *testing.T) {
			svc, target, _ := newAPICacheFixture(t)
			calls := 0
			svc.client.Transport = financeRoundTrip(func(req *http.Request) (*http.Response, error) {
				calls++
				return newAPICacheResponse(req, 200, newAPICacheSearchFixture), nil
			})
			token, code := svc.lookupNewAPIToken(context.Background(), "https://example.com/prefix", key, "console-key", target)
			require.Nil(t, token)
			require.Equal(t, "newapi_token_lookup_unsupported", code)
			require.Zero(t, calls)
			require.Empty(t, svc.newAPICache.tokenIDs)
			require.Empty(t, svc.newAPICache.searchAfter)
		})
	}
}

func TestNewAPICacheGatesQuotaPerOriginAndSearchPerUser(t *testing.T) {
	svc, _, now := newAPICacheFixture(t)
	require.True(t, svc.allowNewAPIQuota("https://EXAMPLE.com:443/prefix/"))
	require.False(t, svc.allowNewAPIQuota("https://example.com/another-prefix"))
	require.True(t, svc.allowNewAPIQuota("https://other.example.com/prefix"))
	*now = now.Add(64 * time.Second)
	require.False(t, svc.allowNewAPIQuota("https://example.com/prefix"))
	*now = now.Add(time.Second)
	require.True(t, svc.allowNewAPIQuota("https://example.com/prefix"))
	require.True(t, svc.allowNewAPISearch("https://example.com/prefix", 42))
	require.False(t, svc.allowNewAPISearch("https://EXAMPLE.com:443/other", 42))
	require.True(t, svc.allowNewAPISearch("https://example.com/prefix", 43))
	*now = now.Add(7 * time.Second)
	require.True(t, svc.allowNewAPISearch("https://example.com/prefix", 42))
	require.False(t, svc.allowNewAPIQuota("https://example.com?key=secret"))
	require.False(t, svc.allowNewAPIQuota("https://key@example.com"))
}

func TestNewAPICacheRecognitionIsBoundedAndDoesNotCrossPathPrefixes(t *testing.T) {
	svc, _, now := newAPICacheFixture(t)
	require.False(t, svc.isNewAPISite("https://example.com/newapi"))
	svc.markNewAPISite("https://EXAMPLE.com:443/newapi/")
	require.True(t, svc.isNewAPISite("https://example.com/newapi"))
	require.False(t, svc.isNewAPISite("https://example.com/sub2api"))
	*now = now.Add(newAPITokenCacheTTL)
	require.False(t, svc.isNewAPISite("https://example.com/newapi"))
	for i := range newAPIFinanceCacheLimit + 10 {
		svc.newAPICache.rememberTokenID(fmt.Sprint(i), int64(i+1), now.Add(time.Duration(i)*time.Second))
		svc.markNewAPISite(fmt.Sprintf("https://site%d.example.com", i))
	}
	require.Len(t, svc.newAPICache.tokenIDs, newAPIFinanceCacheLimit)
	require.Zero(t, svc.newAPICache.tokenID("0", *now))
	require.Len(t, svc.newAPICache.quotaSites, newAPIFinanceCacheLimit)
}

func TestNewAPICacheChurnCannotEvictActiveRateGates(t *testing.T) {
	svc, _, now := newAPICacheFixture(t)
	for i := range newAPIFinanceCacheLimit {
		require.True(t, svc.allowNewAPIQuota(fmt.Sprintf("https://site%d.example.com", i)))
		require.True(t, svc.allowNewAPISearch("https://example.com", int64(i+1)))
	}
	require.False(t, svc.allowNewAPIQuota("https://overflow.example.com"))
	require.False(t, svc.allowNewAPIQuota("https://site0.example.com"))
	require.False(t, svc.allowNewAPISearch("https://example.com", newAPIFinanceCacheLimit+1))
	*now = now.Add(newAPIQuotaRequestGap)
	require.True(t, svc.allowNewAPIQuota("https://overflow.example.com"))
	require.True(t, svc.allowNewAPISearch("https://example.com", newAPIFinanceCacheLimit+1))
	require.LessOrEqual(t, len(svc.newAPICache.quotaSites), newAPIFinanceCacheLimit)
	require.LessOrEqual(t, len(svc.newAPICache.searchAfter), newAPIFinanceCacheLimit)
}
