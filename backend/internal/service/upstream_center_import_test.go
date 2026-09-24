//go:build unit

package service

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type upstreamImportAccounts struct {
	AccountRepository
	accounts map[int64]*Account
	calls    int
}

func (r *upstreamImportAccounts) GetByID(_ context.Context, id int64) (*Account, error) {
	r.calls++
	a, exists := r.accounts[id]
	if !exists {
		return nil, ErrAccountNotFound
	}
	return a, nil
}

func upstreamImportTestAccount() *Account {
	return &Account{ID: 9, Name: "Existing API key", Type: AccountTypeAPIKey, Platform: PlatformOpenAI, Status: StatusActive, Schedulable: true,
		Credentials: map[string]any{"base_url": "https://8.8.8.8/relay/v1", "api_key": "imported-private-key"}, GroupIDs: []int64{42}, RateMultiplier: financeFloat(.4)}
}

func TestUpstreamIndependentAccountImportCopiesCredentialsWithoutBinding(t *testing.T) {
	account := upstreamImportTestAccount()
	accounts := &upstreamImportAccounts{accounts: map[int64]*Account{account.ID: account}}
	repo := &upstreamTestRepo{}
	svc := NewUpstreamCenterService(repo, upstreamTestEncryptor{}, accounts, nil)
	name := "Independent imported monitor"
	result, err := svc.SaveTarget(context.Background(), 0, UpstreamTargetInput{Name: &name, SourceAccountID: &account.ID})
	require.NoError(t, err)
	require.Equal(t, account.Platform, result.Provider)
	require.Equal(t, account.GetCredential("base_url"), result.Endpoint)
	require.Equal(t, "encrypted:imported-private-key", repo.saved.APIKeyEncrypted)
	require.Empty(t, repo.saved.AccountIDs)
	require.Empty(t, repo.saved.BindingCredentials)
	require.Nil(t, repo.saved.SupplierID)
	require.Equal(t, []string{"gpt-5.6-sol"}, result.Models)
	require.Equal(t, 30, result.IntervalSeconds)
	require.Equal(t, []int64{42}, account.GroupIDs, "import must not mutate business groups")
	require.Equal(t, .4, *account.RateMultiplier, "import must not mutate account procurement rates")
	encoded, err := json.Marshal(result)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), "imported-private-key")
	require.NotContains(t, string(encoded), "source_account_id")

	// It is a snapshot, not a hidden permanent source-account relationship.
	repo.target = repo.saved
	repo.target.ID = 7
	account.Status = "disabled"
	account.Credentials["api_key"] = "rotated-after-import"
	blank, rename := "", "Renamed independent"
	result, err = svc.SaveTarget(context.Background(), 7, UpstreamTargetInput{Name: &rename, APIKey: &blank})
	require.NoError(t, err)
	require.Equal(t, "encrypted:imported-private-key", result.APIKeyEncrypted)
	require.Equal(t, 1, accounts.calls, "ordinary edits must not reimport or depend on source status")
	require.Empty(t, result.AccountIDs)

	manualKey := "manually-replaced-key"
	result, err = svc.SaveTarget(context.Background(), 7, UpstreamTargetInput{APIKey: &manualKey})
	require.NoError(t, err)
	require.Equal(t, "encrypted:"+manualKey, result.APIKeyEncrypted)
	require.Equal(t, 1, accounts.calls)
}

func TestUpstreamIndependentUpdateCanReplaceSourceAndPreserveMonitorSettings(t *testing.T) {
	account := upstreamImportTestAccount()
	accounts := &upstreamImportAccounts{accounts: map[int64]*Account{account.ID: account}}
	repo := &upstreamTestRepo{target: validUpstreamTestTarget()}
	svc := NewUpstreamCenterService(repo, upstreamTestEncryptor{}, accounts, nil)
	result, err := svc.SaveTarget(context.Background(), 7, UpstreamTargetInput{SourceAccountID: &account.ID})
	require.NoError(t, err)
	require.Equal(t, PlatformOpenAI, result.Provider)
	require.Equal(t, account.GetCredential("base_url"), result.Endpoint)
	require.Equal(t, "encrypted:imported-private-key", result.APIKeyEncrypted)
	require.Equal(t, []string{"claude-test"}, result.Models)
	require.Equal(t, 300, result.IntervalSeconds)
	require.True(t, repo.saved.ResetBindings, "identity changes must invalidate prior target identity")
	require.Empty(t, result.AccountIDs)
	// Another explicit import replaces the previous copied credentials.
	repo.target = repo.saved
	other := upstreamImportTestAccount()
	other.ID = 10
	other.Credentials = map[string]any{"base_url": "https://1.1.1.1/v1", "api_key": "second-source-key"}
	accounts.accounts[other.ID] = other
	endpoint, provider, blank := "https://1.1.1.1/v1/", PlatformOpenAI, ""
	result, err = svc.SaveTarget(context.Background(), 7, UpstreamTargetInput{SourceAccountID: &other.ID, Endpoint: &endpoint, Provider: &provider, APIKey: &blank})
	require.NoError(t, err)
	require.Equal(t, "https://1.1.1.1/v1", result.Endpoint)
	require.Equal(t, "encrypted:second-source-key", result.APIKeyEncrypted)
	require.Equal(t, []string{"claude-test"}, result.Models)
	require.Equal(t, 300, result.IntervalSeconds)
}

func TestUpstreamIndependentAccountImportRejectsInvalidSources(t *testing.T) {
	for _, test := range []struct {
		name   string
		change func(*Account, *UpstreamTargetInput)
	}{
		{"invalid ID", func(a *Account, in *UpstreamTargetInput) { zero := int64(0); in.SourceAccountID = &zero }},
		{"missing account", func(a *Account, in *UpstreamTargetInput) { missing := int64(100); in.SourceAccountID = &missing }},
		{"wrong type", func(a *Account, in *UpstreamTargetInput) { a.Type = AccountTypeOAuth }},
		{"disabled", func(a *Account, in *UpstreamTargetInput) { a.Status = "disabled" }},
		{"unschedulable", func(a *Account, in *UpstreamTargetInput) { a.Schedulable = false }},
		{"expired", func(a *Account, in *UpstreamTargetInput) {
			expired := time.Now().Add(-time.Hour)
			a.AutoPauseOnExpired = true
			a.ExpiresAt = &expired
		}},
		{"missing key", func(a *Account, in *UpstreamTargetInput) { delete(a.Credentials, "api_key") }},
		{"blank key", func(a *Account, in *UpstreamTargetInput) { a.Credentials["api_key"] = "  " }},
		{"invalid key", func(a *Account, in *UpstreamTargetInput) { a.Credentials["api_key"] = "key\r\nheader" }},
		{"unsupported provider", func(a *Account, in *UpstreamTargetInput) { a.Platform = "unsupported" }},
		{"shadow account", func(a *Account, in *UpstreamTargetInput) { parent := int64(123); a.ParentAccountID = &parent }},
		{"synthetic account", func(a *Account, in *UpstreamTargetInput) { a.Extra = map[string]any{"synthetic_ui_test": true} }},
		{"different endpoint", func(a *Account, in *UpstreamTargetInput) { endpoint := "https://1.1.1.1"; in.Endpoint = &endpoint }},
		{"different provider", func(a *Account, in *UpstreamTargetInput) { provider := PlatformAnthropic; in.Provider = &provider }},
		{"private source endpoint", func(a *Account, in *UpstreamTargetInput) { a.Credentials["base_url"] = "https://127.0.0.1" }},
		{"explicit key conflict", func(a *Account, in *UpstreamTargetInput) { key := "explicit-private-key"; in.APIKey = &key }},
		{"supplier group", func(a *Account, in *UpstreamTargetInput) { in.SupplierID = json.RawMessage("2") }},
		{"business account binding", func(a *Account, in *UpstreamTargetInput) { ids := []int64{9}; in.AccountIDs = &ids }},
	} {
		t.Run(test.name, func(t *testing.T) {
			account := upstreamImportTestAccount()
			accounts := &upstreamImportAccounts{accounts: map[int64]*Account{account.ID: account}}
			repo := &upstreamTestRepo{}
			svc := NewUpstreamCenterService(repo, upstreamTestEncryptor{}, accounts, nil)
			name := "Imported monitor"
			input := UpstreamTargetInput{Name: &name, SourceAccountID: &account.ID}
			test.change(account, &input)
			_, err := svc.SaveTarget(context.Background(), 0, input)
			require.Error(t, err)
			require.Nil(t, repo.saved, "rejected imports must not persist a target")
			require.NotContains(t, err.Error(), "imported-private-key")
		})
	}
}

func TestUpstreamSourceAccountProviderDefaults(t *testing.T) {
	for _, test := range []struct{ provider, endpoint string }{{PlatformOpenAI, "https://api.openai.com"}, {PlatformAnthropic, "https://api.anthropic.com"}, {PlatformGemini, "https://generativelanguage.googleapis.com"}} {
		account := upstreamImportTestAccount()
		account.Platform = test.provider
		delete(account.Credentials, "base_url")
		svc := NewUpstreamCenterService(nil, upstreamTestEncryptor{}, upstreamTestAccounts{account: account}, nil)
		provider, endpoint, key, err := svc.sourceAccountCredentials(context.Background(), account.ID, "source_account_id")
		require.NoError(t, err)
		require.Equal(t, test.provider, provider)
		require.Equal(t, test.endpoint, endpoint)
		require.Equal(t, "imported-private-key", key)
	}
}

func TestUpstreamModelDiscoveryUsesNewSourceDuringEdit(t *testing.T) {
	account := upstreamImportTestAccount()
	repo := &upstreamTestRepo{target: validUpstreamTestTarget()}
	svc := NewUpstreamCenterService(repo, upstreamTestEncryptor{}, upstreamTestAccounts{account: account}, nil)
	calls := 0
	svc.modelsClient.Transport = upstreamModelsTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		require.Equal(t, "https://8.8.8.8/relay/v1/models", r.URL.String())
		require.Equal(t, "Bearer imported-private-key", r.Header.Get("Authorization"))
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"data":[{"id":"source-model"}]}`))}, nil
	})
	models, err := svc.Models(context.Background(), UpstreamModelsInput{TargetID: &repo.target.ID, AccountID: &account.ID})
	require.NoError(t, err)
	require.Equal(t, []string{"source-model"}, models)
	require.Equal(t, 1, calls)
	for _, input := range []UpstreamModelsInput{
		{AccountID: &account.ID, Endpoint: "https://1.1.1.1"},
		{AccountID: &account.ID, Provider: PlatformAnthropic},
		{AccountID: &account.ID, APIKey: "explicit-key"},
	} {
		_, err = svc.Models(context.Background(), input)
		require.ErrorIs(t, err, ErrUpstreamInvalid)
	}
	account.Status = "disabled"
	_, err = svc.Models(context.Background(), UpstreamModelsInput{AccountID: &account.ID})
	require.ErrorIs(t, err, ErrUpstreamInvalid)
	require.Equal(t, 1, calls, "invalid source discovery must fail before external I/O")
}

func TestUpstreamUpdateNeverReusesSavedKeyForChangedRecipient(t *testing.T) {
	for _, test := range []struct {
		name     string
		endpoint string
		provider string
	}{
		{"endpoint", "https://1.1.1.1", PlatformAnthropic},
		{"provider", "https://8.8.8.8", PlatformOpenAI},
		{"cleared new source", "https://1.1.1.1", PlatformOpenAI},
	} {
		for _, key := range []*string{nil, new(string)} {
			t.Run(test.name, func(t *testing.T) {
				repo := &upstreamTestRepo{target: validUpstreamTestTarget()}
				svc := NewUpstreamCenterService(repo, upstreamTestEncryptor{}, nil, nil)
				_, err := svc.SaveTarget(context.Background(), 7, UpstreamTargetInput{Endpoint: &test.endpoint, Provider: &test.provider, APIKey: key})
				require.ErrorIs(t, err, ErrUpstreamInvalid)
				require.Nil(t, repo.saved)
				require.Equal(t, "encrypted:secret-testing-key", repo.target.APIKeyEncrypted)
			})
		}
	}
}

func TestUpstreamUpdateRetainsSameRecipientAndAcceptsExplicitReplacement(t *testing.T) {
	repo := &upstreamTestRepo{target: validUpstreamTestTarget()}
	svc := NewUpstreamCenterService(repo, upstreamTestEncryptor{}, nil, nil)
	name, interval, paused, equivalentEndpoint := "Renamed", 30, false, "https://8.8.8.8/"
	result, err := svc.SaveTarget(context.Background(), 7, UpstreamTargetInput{Name: &name, IntervalSeconds: &interval, Enabled: &paused, Endpoint: &equivalentEndpoint})
	require.NoError(t, err)
	require.Equal(t, "encrypted:secret-testing-key", result.APIKeyEncrypted)
	require.Equal(t, "https://8.8.8.8", result.Endpoint)
	require.Equal(t, 30, result.IntervalSeconds)
	require.False(t, result.Enabled)
	require.False(t, repo.saved.ResetBindings)
	endpoint, provider, key := "https://1.1.1.1", PlatformOpenAI, "replacement-private-key"
	result, err = svc.SaveTarget(context.Background(), 7, UpstreamTargetInput{Endpoint: &endpoint, Provider: &provider, APIKey: &key})
	require.NoError(t, err)
	require.Equal(t, endpoint, result.Endpoint)
	require.Equal(t, provider, result.Provider)
	require.Equal(t, "encrypted:"+key, result.APIKeyEncrypted)
}

func TestUpstreamChangedRecipientRequiresExactLinkedAccountCredentials(t *testing.T) {
	for _, mismatch := range []string{"", "endpoint", "provider", "explicit key"} {
		t.Run(mismatch, func(t *testing.T) {
			account := upstreamImportTestAccount()
			repo := &upstreamTestRepo{target: validUpstreamTestTarget()}
			supplier := int64(2)
			repo.target.SupplierID = &supplier
			accounts := &upstreamImportAccounts{accounts: map[int64]*Account{account.ID: account}}
			svc := NewUpstreamCenterService(repo, upstreamTestEncryptor{}, accounts, nil)
			endpoint, provider, ids := account.GetCredential("base_url"), account.Platform, []int64{account.ID}
			input := UpstreamTargetInput{Endpoint: &endpoint, Provider: &provider, AccountIDs: &ids}
			switch mismatch {
			case "endpoint":
				endpoint = "https://1.1.1.1"
			case "provider":
				provider = PlatformAnthropic
			case "explicit key":
				key := "different-explicit-key"
				input.APIKey = &key
			}
			result, err := svc.SaveTarget(context.Background(), 7, input)
			if mismatch != "" {
				require.ErrorIs(t, err, ErrUpstreamInvalid)
				require.Nil(t, repo.saved)
				return
			}
			require.NoError(t, err)
			require.Equal(t, "encrypted:imported-private-key", result.APIKeyEncrypted)
			require.Equal(t, account.GetCredential("base_url"), result.Endpoint)
			require.Equal(t, account.Platform, result.Provider)
			require.Equal(t, ids, result.AccountIDs)
		})
	}
}

func TestUpstreamSourceImportResetsIncompatibleImplicitAPIMode(t *testing.T) {
	account := upstreamImportTestAccount()
	account.Platform = PlatformAnthropic
	for _, provider := range []*string{nil, &account.Platform} {
		repo := &upstreamTestRepo{target: validUpstreamTestTarget()}
		repo.target.Provider, repo.target.APIMode = PlatformOpenAI, MonitorAPIModeResponses
		svc := NewUpstreamCenterService(repo, upstreamTestEncryptor{}, upstreamTestAccounts{account: account}, nil)
		result, err := svc.SaveTarget(context.Background(), 7, UpstreamTargetInput{SourceAccountID: &account.ID, Provider: provider})
		require.NoError(t, err)
		require.Equal(t, PlatformAnthropic, result.Provider)
		require.Equal(t, MonitorAPIModeChatCompletions, result.APIMode)
		require.Equal(t, 300, result.IntervalSeconds)
	}
}

func TestUpstreamModelDiscoveryNeverReusesStoredKeyAcrossRecipients(t *testing.T) {
	repo := &upstreamTestRepo{target: validUpstreamTestTarget()}
	svc := NewUpstreamCenterService(repo, upstreamTestEncryptor{}, nil, nil)
	calls := 0
	svc.modelsClient.Transport = upstreamModelsTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"data":[]}`))}, nil
	})
	for _, input := range []UpstreamModelsInput{
		{TargetID: &repo.target.ID, Endpoint: "https://1.1.1.1"},
		{TargetID: &repo.target.ID, Provider: PlatformOpenAI},
	} {
		_, err := svc.Models(context.Background(), input)
		require.ErrorIs(t, err, ErrUpstreamInvalid)
	}
	require.Zero(t, calls)
}
