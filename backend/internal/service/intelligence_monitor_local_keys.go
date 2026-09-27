package service

import (
	"context"
	"encoding/json"

	"github.com/Wei-Shaw/sub2api/internal/pkg/ip"
)

type IntelligenceLocalPlanMetadata struct {
	KeyName             string
	KeyMasked           string
	GroupName           string
	GroupRateMultiplier *float64
	GroupStatus         string
}

type IntelligenceLocalPlanRepository interface {
	LoadLocalPlanMetadata(context.Context, []int64) (map[int64]IntelligenceLocalPlanMetadata, error)
	IsManagedIntelligenceKey(context.Context, int64, int64) (bool, error)
}

// A null key selects a plan-owned credential. An explicit ID borrows the
// current administrator's existing key without changing its group or limits.
func (s *IntelligenceMonitorService) configureLocalIntelligenceKey(ctx context.Context, p, old *IntelligenceMonitorPlan, actorID int64, raw json.RawMessage) (*APIKey, error) {
	var selected *int64
	if len(raw) > 0 {
		if json.Unmarshal(raw, &selected) != nil || (selected != nil && *selected <= 0) {
			return nil, invalidLocalIntelligenceKey("choose an existing administrator key or automatic dedicated key")
		}
	}
	if selected != nil {
		key, err := s.keys.GetByID(ctx, *selected)
		if err != nil || key == nil || key.UserID != actorID {
			return nil, invalidLocalIntelligenceKey("choose an API key owned by the current administrator")
		}
		if !key.IsActive() || key.IsExpired() || key.IsQuotaExhausted() || key.GroupID == nil || !sameUpstreamSupplier(key.GroupID, p.GroupID) {
			return nil, invalidLocalIntelligenceKey("the key must be active, unexpired, within quota and assigned to this group")
		}
		if allowed, _ := ip.CheckIPRestriction("127.0.0.1", key.IPWhitelist, key.IPBlacklist); !allowed {
			return nil, invalidLocalIntelligenceKey("the key's IP rules must allow local monitoring from 127.0.0.1")
		}
		if repo, ok := s.repo.(IntelligenceLocalPlanRepository); ok {
			managed, err := repo.IsManagedIntelligenceKey(ctx, key.ID, p.ID)
			if err != nil {
				return nil, err
			}
			if managed {
				return nil, invalidLocalIntelligenceKey("this dedicated key belongs to another monitoring plan")
			}
		}
		// Keeping a plan's own dedicated key does not turn it into a borrowed key.
		p.LocalAPIKeyBorrowed = old == nil || old.SourceType != "local_group" || old.LocalAPIKeyBorrowed || !sameUpstreamSupplier(old.LocalAPIKeyID, selected)
		p.LocalAPIKeyID, p.LocalKeyOwnerID = &key.ID, &key.UserID
		p.LocalAPIKeyName, p.APIKeyMasked = key.Name, maskIntelligenceLocalKey(key.Key)
		return nil, nil
	}
	if old != nil && old.SourceType == "local_group" && sameUpstreamSupplier(old.GroupID, p.GroupID) && old.LocalAPIKeyID != nil && (len(raw) == 0 || !old.LocalAPIKeyBorrowed) {
		return nil, nil
	}
	created, err := s.keys.Create(ctx, actorID, CreateAPIKeyRequest{Name: "智商监控 · " + p.Name, GroupID: p.GroupID, IPWhitelist: []string{"127.0.0.1/32", "::1/128"}})
	if err != nil {
		return nil, err
	}
	p.LocalAPIKeyID, p.LocalKeyOwnerID = &created.ID, &created.UserID
	p.LocalAPIKeyBorrowed = false
	p.LocalAPIKeyName, p.APIKeyMasked = created.Name, maskIntelligenceLocalKey(created.Key)
	return created, nil
}

func invalidLocalIntelligenceKey(detail string) error {
	return ErrIntelligenceInvalid.WithMetadata(map[string]string{"field": "local_api_key_id", "detail": detail})
}

func maskIntelligenceLocalKey(key string) string {
	if len(key) <= 8 {
		return "***"
	}
	return key[:4] + "••••" + key[len(key)-4:]
}

func (s *IntelligenceMonitorService) populateLocalIntelligenceMetadata(ctx context.Context, plans []*IntelligenceMonitorPlan) error {
	ids := make([]int64, 0)
	for _, p := range plans {
		if p.SourceType == "local_group" {
			ids = append(ids, p.ID)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	repo, ok := s.repo.(IntelligenceLocalPlanRepository)
	if !ok {
		return nil
	}
	metadata, err := repo.LoadLocalPlanMetadata(ctx, ids)
	if err != nil {
		return err
	}
	for _, p := range plans {
		if p.SourceType != "local_group" {
			continue
		}
		meta := metadata[p.ID]
		p.LocalAPIKeyName, p.APIKeyMasked = meta.KeyName, meta.KeyMasked
		p.LocalGroupName, p.LocalGroupRateMultiplier, p.LocalGroupStatus = meta.GroupName, meta.GroupRateMultiplier, meta.GroupStatus
	}
	return nil
}
