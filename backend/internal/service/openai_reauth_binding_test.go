package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func reauthBindingInput() OpenAIReauthCredentialsInput {
	enabled := true
	return OpenAIReauthCredentialsInput{Email: "fixture@example.test", Password: "fixture-password", TOTPSecret: "JBSWY3DPEHPK3PXP", Enabled: &enabled}
}

func reauthBindingAccounts(t *testing.T, s *OpenAIReauthService) *reauthAccountsStub {
	t.Helper()
	accounts, ok := s.accounts.(*reauthAccountsStub)
	require.True(t, ok)
	return accounts
}

func TestOpenAIReauthBindingDoesNotInterruptHealthyOrDisabledAccounts(t *testing.T) {
	for _, disabled := range []bool{false, true} {
		t.Run(map[bool]string{false: "healthy", true: "manual-disabled"}[disabled], func(t *testing.T) {
			s, _, store, oauth, _ := reauthFixture(t)
			a := reauthBindingAccounts(t, s).account
			delete(a.Extra, OpenAIReauthPendingKey)
			if disabled {
				a.Status = "disabled"
				a.Schedulable = false
			}
			before, _ := json.Marshal(a)
			input := reauthBindingInput()
			input.Password = " p\\@ss----word "
			status, err := s.BindCredentials(context.Background(), a.ID, input)
			require.NoError(t, err)
			require.Equal(t, "idle", status.Status)
			require.Equal(t, "fixture@example.test", status.Email)
			require.Equal(t, "ciphertext-only", store.savedCipher)
			require.Equal(t, 1, store.bound)
			require.Zero(t, store.enqueued)
			require.Zero(t, oauth.refreshes)
			require.Zero(t, oauth.exchanges)
			after, _ := json.Marshal(a)
			require.JSONEq(t, string(before), string(after), "binding must preserve tokens, proxy, groups and manual controls")
			var secret openAIReauthSecret
			cipher, ok := s.encryptor.(*reauthCipherStub)
			require.True(t, ok)
			require.NoError(t, json.Unmarshal([]byte(cipher.plain), &secret))
			require.Equal(t, input.Password, secret.Password)
		})
	}
}

func TestOpenAIReauthBindingRefusesAnotherOrUnknownIdentity(t *testing.T) {
	for _, tc := range []struct{ email, want string }{{"other@example.test", "identity_mismatch"}, {"", "identity_unknown"}} {
		s, _, store, _, _ := reauthFixture(t)
		reauthBindingAccounts(t, s).account.Credentials["email"] = tc.email
		_, err := s.BindCredentials(context.Background(), 1, reauthBindingInput())
		require.EqualError(t, err, tc.want)
		require.Zero(t, store.bound)
	}
}

func TestOpenAIReauthBindingAcceptsStableIdentityWithoutStoredEmail(t *testing.T) {
	s, _, store, _, _ := reauthFixture(t)
	a := reauthBindingAccounts(t, s).account
	delete(a.Credentials, "email")
	a.Name = "legacy-account"
	a.Credentials["chatgpt_user_id"] = "original-user"
	status, err := s.BindCredentials(context.Background(), a.ID, reauthBindingInput())
	require.NoError(t, err)
	require.Equal(t, "fixture@example.test", status.Email)
	require.Equal(t, 1, store.bound)
	require.NotContains(t, a.Credentials, "email")
	require.False(t, reauthIdentityMatches(a, "fixture@example.test", &OpenAITokenInfo{
		AccessToken: "other", Email: "fixture@example.test", ExpiresAt: time.Now().Add(time.Hour).Unix(), ChatGPTUserID: "another-user",
	}), "new authorization must still match the original stable identity")
}

func TestOpenAIReauthExplicitImportModesDoNotReauthorizeExisting(t *testing.T) {
	s, _, store, oauth, _ := reauthFixture(t)
	content := "fixture@example.test----fixture-password----JBSWY3DPEHPK3PXP"
	results, err := s.Import(context.Background(), OpenAIReauthImportInput{Mode: "create", Content: content})
	require.NoError(t, err)
	require.Equal(t, "account_exists", results[0].ErrorCode)
	require.Zero(t, store.bound)
	results, err = s.Import(context.Background(), OpenAIReauthImportInput{Mode: "bind", Content: content})
	require.NoError(t, err)
	require.Equal(t, "bound", results[0].Status)
	require.Zero(t, store.enqueued)
	require.Zero(t, oauth.refreshes)
	reauthBindingAccounts(t, s).account = nil
	results, err = s.Import(context.Background(), OpenAIReauthImportInput{Mode: "bind", Content: content})
	require.NoError(t, err)
	require.Equal(t, "account_not_found", results[0].ErrorCode)
}

func TestOpenAIReauthCreateFindsExistingIdentityInIDToken(t *testing.T) {
	s, _, store, _, _ := reauthFixture(t)
	a := reauthBindingAccounts(t, s).account
	delete(a.Credentials, "email")
	a.Name = "legacy-account"
	claims, err := json.Marshal(map[string]any{"email": "fixture@example.test"})
	require.NoError(t, err)
	a.Credentials["id_token"] = "e30." + base64.RawURLEncoding.EncodeToString(claims) + ".test"
	results, err := s.Import(context.Background(), OpenAIReauthImportInput{Mode: "create", Content: "fixture@example.test----fixture-password----JBSWY3DPEHPK3PXP"})
	require.NoError(t, err)
	require.Equal(t, "account_exists", results[0].ErrorCode)
	require.Zero(t, store.bound)
}

type reauthCreateAdminStub struct {
	AdminService
	input *CreateAccountInput
}

func (s *reauthCreateAdminStub) CreateAccount(_ context.Context, input *CreateAccountInput) (*Account, error) {
	s.input = input
	return &Account{ID: 99, Platform: input.Platform, Type: input.Type, Credentials: input.Credentials, Extra: input.Extra, ProxyID: input.ProxyID}, nil
}

func TestOpenAIReauthCreateWorksWithoutAnyExistingAccounts(t *testing.T) {
	s, _, store, _, _ := reauthFixture(t)
	reauthBindingAccounts(t, s).account = nil
	admin := &reauthCreateAdminStub{}
	s.admin = admin
	proxyID := int64(2)
	results, err := s.Import(context.Background(), OpenAIReauthImportInput{Mode: "create", Content: "fixture@example.test----fixture-password----JBSWY3DPEHPK3PXP", ProxyID: &proxyID, GroupIDs: []int64{3}})
	require.NoError(t, err)
	require.Equal(t, "queued", results[0].Status)
	require.Equal(t, int64(99), results[0].AccountID)
	require.Equal(t, "ciphertext-only", store.savedCipher)
	require.Equal(t, proxyID, *admin.input.ProxyID)
	require.Equal(t, []int64{3}, admin.input.GroupIDs)
	require.Equal(t, true, admin.input.Extra[OpenAIReauthPendingKey])
	require.NotContains(t, admin.input.Credentials, "password")
	require.NotContains(t, admin.input.Credentials, "totp_secret")
}

func TestOpenAIReauthEnableOnlyBindsAndRunRespectsManualDisable(t *testing.T) {
	s, _, store, _, _ := reauthFixture(t)
	a := reauthBindingAccounts(t, s).account
	a.Schedulable = false
	require.NoError(t, s.SetEnabled(context.Background(), a.ID, true))
	require.Equal(t, 1, store.bound)
	require.Zero(t, store.enqueued)
	require.EqualError(t, s.Retry(context.Background(), a.ID), "account_disabled")
}
