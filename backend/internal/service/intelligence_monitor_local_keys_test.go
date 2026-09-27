//go:build unit

package service

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/stretchr/testify/require"
)

type intelligenceLocalKeysRepo struct {
	APIKeyRepository
	keys             map[int64]*APIKey
	created, deleted []int64
}

func (r *intelligenceLocalKeysRepo) GetByID(_ context.Context, id int64) (*APIKey, error) {
	if k := r.keys[id]; k != nil {
		c := *k
		return &c, nil
	}
	return nil, ErrAPIKeyNotFound
}
func (r *intelligenceLocalKeysRepo) Create(_ context.Context, key *APIKey) error {
	key.ID = int64(100 + len(r.created))
	r.keys[key.ID] = key
	r.created = append(r.created, key.ID)
	return nil
}
func (r *intelligenceLocalKeysRepo) GetKeyAndOwnerID(ctx context.Context, id int64) (string, int64, error) {
	k, err := r.GetByID(ctx, id)
	if err != nil {
		return "", 0, err
	}
	return k.Key, k.UserID, nil
}
func (r *intelligenceLocalKeysRepo) Delete(_ context.Context, id int64) error {
	r.deleted = append(r.deleted, id)
	return nil
}
func (r *intelligenceLocalKeysRepo) DeleteWithAudit(ctx context.Context, id int64) error {
	return r.Delete(ctx, id)
}

type intelligenceLocalGroupRepo struct {
	GroupRepository
	group *Group
}

func (r intelligenceLocalGroupRepo) GetByID(context.Context, int64) (*Group, error) {
	return r.group, nil
}

type intelligenceLocalUserRepo struct{ UserRepository }

func (intelligenceLocalUserRepo) GetByID(_ context.Context, id int64) (*User, error) {
	return &User{ID: id, Role: RoleAdmin, Status: StatusActive}, nil
}

type intelligenceLocalMonitorRepo struct {
	intelligenceTestRepository
	managed bool
	saveErr error
}

func (r *intelligenceLocalMonitorRepo) SavePlan(ctx context.Context, p *IntelligenceMonitorPlan) error {
	if r.saveErr != nil {
		return r.saveErr
	}
	return r.intelligenceTestRepository.SavePlan(ctx, p)
}
func (r *intelligenceLocalMonitorRepo) IsManagedIntelligenceKey(context.Context, int64, int64) (bool, error) {
	return r.managed, nil
}
func (r *intelligenceLocalMonitorRepo) LoadLocalPlanMetadata(context.Context, []int64) (map[int64]IntelligenceLocalPlanMetadata, error) {
	return nil, nil
}

func newLocalIntelligenceKeyTestService() (*IntelligenceMonitorService, *intelligenceLocalMonitorRepo, *intelligenceLocalKeysRepo) {
	group := &Group{ID: 8, Name: "Local group", Platform: PlatformOpenAI, Status: StatusActive, RateMultiplier: 0.8}
	key := &APIKey{ID: 41, UserID: 7, Key: "sk-private-monitor-key", Name: "Admin primary", GroupID: &group.ID, Status: StatusActive}
	keys := &intelligenceLocalKeysRepo{keys: map[int64]*APIKey{41: key}}
	repo := &intelligenceLocalMonitorRepo{}
	groups := intelligenceLocalGroupRepo{group: group}
	svc := NewIntelligenceMonitorService(repo, upstreamTestEncryptor{}, nil, groups, NewAPIKeyService(keys, intelligenceLocalUserRepo{}, groups, nil, nil, nil, &config.Config{}), nil, nil)
	return svc, repo, keys
}
func localIntelligenceInput() IntelligenceMonitorInput {
	name, source := "Group watch", "local_group"
	return IntelligenceMonitorInput{Name: &name, SourceType: &source, GroupID: json.RawMessage(`8`), LocalAPIKeyID: json.RawMessage(`41`)}
}

func TestIntelligenceLocalExistingKeyIsBorrowedAndNotExposedOrDeleted(t *testing.T) {
	svc, repo, keys := newLocalIntelligenceKeyTestService()
	plan, err := svc.SavePlan(context.Background(), 0, 7, localIntelligenceInput())
	require.NoError(t, err)
	require.True(t, plan.LocalAPIKeyBorrowed)
	require.False(t, plan.LocalAPIKeyManaged)
	require.Equal(t, int64(41), *plan.LocalAPIKeyID)
	require.Equal(t, "Admin primary", plan.LocalAPIKeyName)
	require.Equal(t, 0.8, *plan.LocalGroupRateMultiplier)
	require.Empty(t, keys.created)
	encoded, err := json.Marshal(plan)
	require.NoError(t, err)
	require.NotContains(t, string(encoded), keys.keys[41].Key)
	require.NotContains(t, string(encoded), "local_key_owner_id")
	repo.plan = plan
	require.NoError(t, svc.DeletePlan(context.Background(), plan.ID))
	require.Empty(t, keys.deleted)
}
func TestIntelligenceLocalExistingKeyValidation(t *testing.T) {
	for _, tc := range []struct {
		name   string
		mutate func(*IntelligenceMonitorInput, *intelligenceLocalMonitorRepo, *APIKey)
	}{
		{"other owner", func(_ *IntelligenceMonitorInput, _ *intelligenceLocalMonitorRepo, k *APIKey) { k.UserID = 8 }},
		{"different group", func(_ *IntelligenceMonitorInput, _ *intelligenceLocalMonitorRepo, k *APIKey) {
			id := int64(9)
			k.GroupID = &id
		}},
		{"unassigned", func(_ *IntelligenceMonitorInput, _ *intelligenceLocalMonitorRepo, k *APIKey) { k.GroupID = nil }},
		{"disabled", func(_ *IntelligenceMonitorInput, _ *intelligenceLocalMonitorRepo, k *APIKey) {
			k.Status = StatusDisabled
		}},
		{"expired", func(_ *IntelligenceMonitorInput, _ *intelligenceLocalMonitorRepo, k *APIKey) {
			at := time.Now().Add(-time.Minute)
			k.ExpiresAt = &at
		}},
		{"quota exhausted", func(_ *IntelligenceMonitorInput, _ *intelligenceLocalMonitorRepo, k *APIKey) {
			k.Quota = 1
			k.QuotaUsed = 1
		}},
		{"IP restricted", func(_ *IntelligenceMonitorInput, _ *intelligenceLocalMonitorRepo, k *APIKey) {
			k.IPWhitelist = []string{"8.8.8.8"}
		}},
		{"other plan dedicated key", func(_ *IntelligenceMonitorInput, r *intelligenceLocalMonitorRepo, _ *APIKey) { r.managed = true }},
		{"invalid ID", func(in *IntelligenceMonitorInput, _ *intelligenceLocalMonitorRepo, _ *APIKey) {
			in.LocalAPIKeyID = []byte(`-1`)
		}},
		{"fractional ID", func(in *IntelligenceMonitorInput, _ *intelligenceLocalMonitorRepo, _ *APIKey) {
			in.LocalAPIKeyID = []byte(`41.5`)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, repo, keys := newLocalIntelligenceKeyTestService()
			in := localIntelligenceInput()
			tc.mutate(&in, repo, keys.keys[41])
			_, err := svc.SavePlan(context.Background(), 0, 7, in)
			require.ErrorIs(t, err, ErrIntelligenceInvalid)
			require.Nil(t, repo.saved)
			require.Empty(t, keys.created)
			require.Empty(t, keys.deleted)
		})
	}
}
func TestIntelligenceLocalSwitchToDedicatedKeepsAdministratorKey(t *testing.T) {
	svc, repo, keys := newLocalIntelligenceKeyTestService()
	plan, err := svc.SavePlan(context.Background(), 0, 7, localIntelligenceInput())
	require.NoError(t, err)
	plan.ID = 3
	repo.plan = plan
	// Omitted selector preserves the old borrowed key without creating one.
	unchanged, err := svc.SavePlan(context.Background(), 3, 7, IntelligenceMonitorInput{})
	require.NoError(t, err)
	require.True(t, unchanged.LocalAPIKeyBorrowed)
	require.Empty(t, keys.created)
	// Explicit automatic selection creates a dedicated loopback-only key.
	dedicated, err := svc.SavePlan(context.Background(), 3, 7, IntelligenceMonitorInput{LocalAPIKeyID: []byte(`null`)})
	require.NoError(t, err)
	require.False(t, dedicated.LocalAPIKeyBorrowed)
	require.True(t, dedicated.LocalAPIKeyManaged)
	require.Equal(t, []string{"127.0.0.1/32", "::1/128"}, keys.keys[*dedicated.LocalAPIKeyID].IPWhitelist)
	require.Empty(t, keys.deleted)
	repo.plan = dedicated
	borrowed, err := svc.SavePlan(context.Background(), 3, 7, IntelligenceMonitorInput{LocalAPIKeyID: []byte(`41`)})
	require.NoError(t, err)
	require.True(t, borrowed.LocalAPIKeyBorrowed)
	require.Equal(t, []int64{*dedicated.LocalAPIKeyID}, keys.deleted)
}
func TestIntelligenceLocalFailedSaveNeverDeletesBorrowedKey(t *testing.T) {
	svc, repo, keys := newLocalIntelligenceKeyTestService()
	repo.saveErr = errors.New("conflict")
	_, err := svc.SavePlan(context.Background(), 0, 7, localIntelligenceInput())
	require.Error(t, err)
	require.Empty(t, keys.deleted)
	require.False(t, intelligenceEnabledOnly(IntelligenceMonitorInput{Enabled: candyFlowBool(false), LocalAPIKeyID: []byte(`41`)}))
}
