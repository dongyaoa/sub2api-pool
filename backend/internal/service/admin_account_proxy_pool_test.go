//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUpdateAccountClearsPoolAndHonorsSingleProxyFields(t *testing.T) {
	for _, explicit := range []bool{false, true} {
		t.Run(map[bool]string{false: "clear to direct", true: "switch to single proxy"}[explicit], func(t *testing.T) {
			id := int64(88)
			pool := []AccountProxyPoolEntry{{ProxyID: id, Concurrency: 20}}
			extra := map[string]any{}
			SetAccountProxyPoolExtra(extra, pool)
			repo := &updateAccountCredsRepoStub{account: &Account{
				ID: 201, Platform: PlatformAnthropic, Type: AccountTypeAPIKey, Status: StatusActive,
				ProxyID: &id, Proxy: &Proxy{ID: id}, ProxyPool: pool, Extra: extra, Concurrency: 20,
			}}
			svc := &adminServiceImpl{accountRepo: repo}
			empty := []AccountProxyPoolEntry{}
			input := &UpdateAccountInput{ProxyPool: &empty}
			if explicit {
				newID, concurrency := int64(99), 7
				input.ProxyID, input.Concurrency = &newID, &concurrency
			}
			updated, err := svc.UpdateAccount(context.Background(), 201, input)
			require.NoError(t, err)
			require.Empty(t, updated.ProxyPool)
			require.NotContains(t, updated.Extra, AccountProxyPoolExtraKey)
			if explicit {
				require.Equal(t, int64(99), *updated.ProxyID)
				require.Equal(t, 7, updated.Concurrency)
			} else {
				require.Nil(t, updated.ProxyID)
				require.Equal(t, 20, updated.Concurrency, "omitted concurrency must not become unlimited")
			}
		})
	}
}

func TestCreateAccountEmptyPoolHonorsExplicitSingleProxy(t *testing.T) {
	id := int64(99)
	empty := []AccountProxyPoolEntry{}
	repo := &accountRepoStubForBulkUpdate{createID: 202}
	svc := &adminServiceImpl{accountRepo: repo}
	created, err := svc.CreateAccount(context.Background(), &CreateAccountInput{
		Name: "single proxy", Platform: PlatformAnthropic, Type: AccountTypeAPIKey,
		Credentials: map[string]any{"api_key": "test"}, ProxyPool: &empty,
		ProxyID: &id, Concurrency: 7, SkipDefaultGroupBind: true,
	})
	require.NoError(t, err)
	require.Equal(t, id, *created.ProxyID)
	require.Equal(t, 7, created.Concurrency)
	require.Empty(t, AccountProxyPoolFromExtra(created.Extra))
}

func TestUpdateAccountPoolCapacityOverridesLegacyConcurrency(t *testing.T) {
	for _, supplied := range []bool{false, true} {
		t.Run(map[bool]string{false: "existing pool unchanged", true: "new pool supplied"}[supplied], func(t *testing.T) {
			pool := []AccountProxyPoolEntry{{ProxyID: 88, Concurrency: 20}, {ProxyID: 89, Concurrency: 20}}
			extra := map[string]any{}
			if !supplied {
				SetAccountProxyPoolExtra(extra, pool)
			}
			repo := &updateAccountCredsRepoStub{account: &Account{
				ID: 203, Platform: PlatformAnthropic, Type: AccountTypeAPIKey, Status: StatusActive,
				Extra: extra, Concurrency: 10,
			}}
			legacyConcurrency := 999
			input := &UpdateAccountInput{Concurrency: &legacyConcurrency}
			if supplied {
				input.ProxyPool = &pool
			}
			updated, err := (&adminServiceImpl{accountRepo: repo}).UpdateAccount(context.Background(), 203, input)
			require.NoError(t, err)
			require.Equal(t, 40, updated.Concurrency)
			require.Equal(t, pool, AccountProxyPoolFromExtra(updated.Extra))
		})
	}
}

func TestBulkUpdateAccountEmptyPoolHonorsSingleProxyFields(t *testing.T) {
	for _, explicit := range []bool{false, true} {
		empty := []AccountProxyPoolEntry{}
		repo := &accountRepoStubForBulkUpdate{}
		svc := &adminServiceImpl{accountRepo: repo}
		input := &BulkUpdateAccountsInput{AccountIDs: []int64{201}, ProxyPool: &empty}
		if explicit {
			id, concurrency := int64(99), 7
			input.ProxyID, input.Concurrency = &id, &concurrency
		}
		result, err := svc.BulkUpdateAccounts(context.Background(), input)
		require.NoError(t, err)
		require.Equal(t, []int64{201}, result.SuccessIDs)
		require.Contains(t, repo.lastBulkUpdate.Extra, AccountProxyPoolExtraKey)
		require.Nil(t, repo.lastBulkUpdate.Extra[AccountProxyPoolExtraKey])
		if explicit {
			require.Equal(t, int64(99), *repo.lastBulkUpdate.ProxyID)
			require.Equal(t, 7, *repo.lastBulkUpdate.Concurrency)
		} else {
			require.Equal(t, int64(0), *repo.lastBulkUpdate.ProxyID)
			require.Nil(t, repo.lastBulkUpdate.Concurrency)
		}
	}
}
