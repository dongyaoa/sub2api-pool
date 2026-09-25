//go:build unit

package service

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUpstreamNewAPICredentialsEncryptPreserveClearAndRedact(t *testing.T) {
	repo := &upstreamTestRepo{target: validUpstreamTestTarget()}
	svc := NewUpstreamCenterService(repo, upstreamTestEncryptor{}, nil, nil)
	userID, token := int64(42), " console-private-token "
	result, err := svc.SaveTarget(context.Background(), 7, UpstreamTargetInput{NewAPIUserID: &userID, NewAPIAccessToken: &token})
	require.NoError(t, err)
	require.Equal(t, int64(42), repo.saved.NewAPIUserID)
	require.Equal(t, "encrypted:console-private-token", repo.saved.NewAPIAccessTokenEncrypted)
	require.True(t, result.NewAPIAccessTokenConfigured)
	require.False(t, repo.saved.ResetBindings, "console credentials do not change business attribution")
	encoded, err := json.Marshal(result)
	require.NoError(t, err)
	require.Contains(t, string(encoded), `"newapi_user_id":42`)
	require.Contains(t, string(encoded), `"newapi_access_token_configured":true`)
	require.NotContains(t, string(encoded), "console-private-token")
	require.NotContains(t, string(encoded), "newapi_access_token_encrypted")

	repo.target = repo.saved
	for _, update := range []UpstreamTargetInput{{}, {NewAPIAccessToken: new(string)}, {NewAPIAccessToken: &token}} {
		result, err = svc.SaveTarget(context.Background(), 7, update)
		require.NoError(t, err)
		require.Equal(t, repo.target.NewAPIAccessTokenEncrypted, repo.saved.NewAPIAccessTokenEncrypted)
		require.True(t, result.NewAPIAccessTokenConfigured)
		require.False(t, repo.saved.ResetBindings)
	}
	zero := int64(0)
	result, err = svc.SaveTarget(context.Background(), 7, UpstreamTargetInput{NewAPIUserID: &zero})
	require.NoError(t, err)
	require.Zero(t, result.NewAPIUserID)
	require.Empty(t, repo.saved.NewAPIAccessTokenEncrypted)
	require.False(t, result.NewAPIAccessTokenConfigured)
	require.False(t, repo.saved.ResetBindings)
}

func TestUpstreamNewAPICredentialsRejectInvalidPairs(t *testing.T) {
	positive, negative := int64(42), int64(-1)
	validToken, tooLong, newline := "private", strings.Repeat("t", 4097), "private\r\n"
	for _, input := range []UpstreamTargetInput{
		{NewAPIUserID: &positive},
		{NewAPIUserID: &negative, NewAPIAccessToken: &validToken},
		{NewAPIAccessToken: &validToken},
		{NewAPIUserID: &positive, NewAPIAccessToken: &tooLong},
		{NewAPIUserID: &positive, NewAPIAccessToken: &newline},
	} {
		repo := &upstreamTestRepo{target: validUpstreamTestTarget()}
		svc := NewUpstreamCenterService(repo, upstreamTestEncryptor{}, nil, nil)
		_, err := svc.SaveTarget(context.Background(), 7, input)
		require.ErrorIs(t, err, ErrUpstreamInvalid)
		require.Nil(t, repo.saved)
	}
}

func TestUpstreamNewAPICredentialsCannotFollowRecipientChanges(t *testing.T) {
	endpoint, provider, inferenceKey := "https://1.1.1.1", "openai", "replacement-inference-key"
	userID, zero, token := int64(84), int64(0), "replacement-console-token"
	for _, tc := range []struct {
		name  string
		input UpstreamTargetInput
	}{
		{"endpoint", UpstreamTargetInput{Endpoint: &endpoint, APIKey: &inferenceKey}},
		{"provider", UpstreamTargetInput{Provider: &provider, APIKey: &inferenceKey}},
		{"user ID", UpstreamTargetInput{NewAPIUserID: &userID}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &upstreamTestRepo{target: validUpstreamTestTarget()}
			repo.target.NewAPIUserID = 42
			repo.target.NewAPIAccessTokenEncrypted = "encrypted:old-console-token"
			svc := NewUpstreamCenterService(repo, upstreamTestEncryptor{}, nil, nil)
			_, err := svc.SaveTarget(context.Background(), 7, tc.input)
			require.ErrorIs(t, err, ErrUpstreamInvalid)
			require.Nil(t, repo.saved)

			blank := " "
			tc.input.NewAPIAccessToken = &blank
			_, err = svc.SaveTarget(context.Background(), 7, tc.input)
			require.ErrorIs(t, err, ErrUpstreamInvalid, "blank token is not an explicit replacement credential")

			tc.input.NewAPIAccessToken = &token
			result, err := svc.SaveTarget(context.Background(), 7, tc.input)
			require.NoError(t, err)
			require.Equal(t, "encrypted:replacement-console-token", repo.saved.NewAPIAccessTokenEncrypted)
			require.True(t, result.NewAPIAccessTokenConfigured)

			tc.input.NewAPIUserID = &zero
			tc.input.NewAPIAccessToken = nil
			result, err = svc.SaveTarget(context.Background(), 7, tc.input)
			require.NoError(t, err)
			require.Zero(t, result.NewAPIUserID)
			require.Empty(t, repo.saved.NewAPIAccessTokenEncrypted)
		})
	}
}

func TestUpstreamFinanceTargetDoesNotSerializeNewAPICredentials(t *testing.T) {
	encoded, err := json.Marshal(&UpstreamFinanceTarget{NewAPIUserID: 42, NewAPIAccessTokenEncrypted: "secret-cipher"})
	require.NoError(t, err)
	require.JSONEq(t, `{}`, string(encoded))
}

func TestUpstreamNewAPIConsoleChangesPreserveActiveBindings(t *testing.T) {
	repo := &upstreamTestRepo{target: validUpstreamTestTarget()}
	supplierID := int64(2)
	repo.target.SupplierID = &supplierID
	repo.target.AccountIDs = []int64{9}
	accounts := upstreamTestAccounts{account: &Account{ID: 9, Platform: PlatformAnthropic, Type: AccountTypeAPIKey, Credentials: map[string]any{"base_url": repo.target.Endpoint, "api_key": "secret-testing-key"}}}
	svc := NewUpstreamCenterService(repo, upstreamTestEncryptor{}, accounts, nil)
	userID, token := int64(42), "console-token"
	_, err := svc.SaveTarget(context.Background(), 7, UpstreamTargetInput{NewAPIUserID: &userID, NewAPIAccessToken: &token})
	require.NoError(t, err)
	require.Equal(t, []int64{9}, repo.saved.AccountIDs)
	require.False(t, repo.saved.ResetBindings)
	require.Equal(t, UpstreamBindingCredential{APIKey: "secret-testing-key", BaseURL: repo.target.Endpoint}, repo.saved.BindingCredentials[9])
}

func TestUpstreamNewAPICredentialsCannotFollowImportedAccountRecipient(t *testing.T) {
	repo := &upstreamTestRepo{target: validUpstreamTestTarget()}
	repo.target.NewAPIUserID = 42
	repo.target.NewAPIAccessTokenEncrypted = "encrypted:old-console-token"
	account := upstreamImportTestAccount()
	accounts := &upstreamImportAccounts{accounts: map[int64]*Account{account.ID: account}}
	svc := NewUpstreamCenterService(repo, upstreamTestEncryptor{}, accounts, nil)
	_, err := svc.SaveTarget(context.Background(), 7, UpstreamTargetInput{SourceAccountID: &account.ID})
	require.ErrorIs(t, err, ErrUpstreamInvalid)
	require.Nil(t, repo.saved)
	zero := int64(0)
	result, err := svc.SaveTarget(context.Background(), 7, UpstreamTargetInput{SourceAccountID: &account.ID, NewAPIUserID: &zero})
	require.NoError(t, err)
	require.Empty(t, result.NewAPIAccessTokenEncrypted)
	require.False(t, result.NewAPIAccessTokenConfigured)
}
