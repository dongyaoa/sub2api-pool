package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
	"github.com/Wei-Shaw/sub2api/internal/pkg/timezone"
)

var ErrUpstreamAccountMonitorAmbiguous = infraerrors.Conflict("UPSTREAM_ACCOUNT_MONITOR_AMBIGUOUS", "multiple independent monitors use this account's credentials; link the intended group in the upstream center")

// UpstreamAccountMonitor is an account-scoped view of the existing inventory.
// Reading this projection never creates a monitor, starts a check or syncs a wallet.
type UpstreamAccountMonitor struct {
	AccountID        int64             `json:"account_id"`
	AccountName      string            `json:"account_name"`
	Provider         string            `json:"provider"`
	PelicanSupported bool              `json:"pelican_supported"`
	Target           *UpstreamTarget   `json:"target"`
	Supplier         *UpstreamSupplier `json:"supplier"`
}

// This identity is internal only. The exact credential snapshot is rechecked
// under a database lock before any binding or inventory entry is written.
type UpstreamAccountMonitorIdentity struct {
	AccountID   int64
	AccountName string
	Provider    string
	Endpoint    string
	Fingerprint string
	Credentials UpstreamBindingCredential
}

type UpstreamAccountMonitorRepository interface {
	FindAccountMonitor(context.Context, UpstreamAccountMonitorIdentity) (*UpstreamTarget, error)
	EnsureAccountMonitor(context.Context, UpstreamAccountMonitorIdentity, *UpstreamTarget) (*UpstreamTarget, error)
}

func (s *UpstreamCenterService) accountMonitorIdentity(ctx context.Context, id int64) (UpstreamAccountMonitorIdentity, error) {
	identity := UpstreamAccountMonitorIdentity{}
	invalid := func() (UpstreamAccountMonitorIdentity, error) {
		return identity, ErrUpstreamInvalid.WithMetadata(map[string]string{"field": "account_id", "detail": "select an API key account with its own supported credentials"})
	}
	if id <= 0 || s.accounts == nil {
		return invalid()
	}
	a, err := s.accounts.GetByID(ctx, id)
	if err != nil {
		return identity, err
	}
	if a == nil || a.Type != AccountTypeAPIKey || a.ParentAccountID != nil || a.IsSyntheticUITest() {
		return invalid()
	}
	endpoint := normalizeEndpoint(a.GetCredential("base_url"))
	var defaultEndpoint string
	switch a.Platform {
	case MonitorProviderOpenAI:
		defaultEndpoint = "https://api.openai.com"
	case MonitorProviderAnthropic:
		defaultEndpoint = "https://api.anthropic.com"
	case MonitorProviderGemini:
		defaultEndpoint = "https://generativelanguage.googleapis.com"
	default:
		return invalid()
	}
	if endpoint == "" {
		endpoint = defaultEndpoint
	}
	key := strings.TrimSpace(a.GetCredential("api_key"))
	if key == "" || len(key) > 2000 || strings.ContainsAny(key, "\r\n") {
		return invalid()
	}
	fingerprint := sha256.Sum256([]byte(endpoint + "\x00" + key))
	return UpstreamAccountMonitorIdentity{AccountID: id, AccountName: a.Name, Provider: a.Platform, Endpoint: endpoint,
		Fingerprint: hex.EncodeToString(fingerprint[:]), Credentials: UpstreamBindingCredential{APIKey: a.GetCredential("api_key"), BaseURL: a.GetCredential("base_url")}}, nil
}

func (s *UpstreamCenterService) AccountMonitor(ctx context.Context, id int64, ensure bool) (*UpstreamAccountMonitor, error) {
	identity, err := s.accountMonitorIdentity(ctx, id)
	if err != nil {
		return nil, err
	}
	repo, ok := s.repo.(UpstreamAccountMonitorRepository)
	if !ok {
		return nil, fmt.Errorf("upstream account monitoring repository is unavailable")
	}
	target, err := repo.FindAccountMonitor(ctx, identity)
	if err != nil {
		return nil, err
	}
	if ensure {
		var candidate *UpstreamTarget
		if target == nil {
			name := strings.TrimSpace(identity.AccountName)
			if name == "" {
				name = fmt.Sprintf("API Key %d", id)
			}
			nameRunes := []rune(name)
			if len(nameRunes) > 100 {
				name = string(nameRunes[:100])
			}
			candidate = &UpstreamTarget{Name: name, Provider: identity.Provider, Endpoint: identity.Endpoint, APIMode: MonitorAPIModeChatCompletions,
				Models: []string{"gpt-5.6-sol"}, Enabled: false, IntervalSeconds: 30, TimeoutSeconds: 45, DegradedThresholdMs: 6000,
				WalletRef: "default", APIKeyFingerprint: identity.Fingerprint, AccountIDs: []int64{id}}
			switch identity.Provider {
			case MonitorProviderAnthropic:
				candidate.Models = []string{"claude-haiku-4-5"}
			case MonitorProviderGemini:
				candidate.Models = []string{"gemini-2.5-flash"}
			}
			key := strings.TrimSpace(identity.Credentials.APIKey)
			if err := validateUpstreamTargetConfig(candidate, key, true); err != nil {
				return nil, err
			}
			candidate.APIKeyEncrypted, err = s.encryptor.Encrypt(key)
			if err != nil {
				return nil, fmt.Errorf("encrypt upstream credentials: %w", err)
			}
		}
		target, err = repo.EnsureAccountMonitor(ctx, identity, candidate)
		if err != nil {
			return nil, err
		}
	}
	out := &UpstreamAccountMonitor{AccountID: id, AccountName: identity.AccountName, Provider: identity.Provider, PelicanSupported: identity.Provider == MonitorProviderOpenAI, Target: target}
	if target == nil {
		return out, nil
	}
	if err := s.repo.PopulateStatistics(ctx, []*UpstreamTarget{target}, time.Now().Add(-24*time.Hour)); err != nil {
		return nil, err
	}
	if s.finance != nil {
		// Use the same daily ledger scope as this group in the upstream center.
		// One target-scoped aggregate keeps account-dialog polling local and avoids
		// loading the whole inventory or issuing any upstream balance requests.
		from := timezone.Today()
		target.Finance, err = s.finance.Summary(ctx, target.SupplierID, &target.ID, from, from.AddDate(0, 0, 1))
		if err != nil {
			return nil, err
		}
		target.Balance, err = s.finance.LatestBalance(ctx, target.ID)
		if err != nil {
			return nil, err
		}
	}
	s.maskTarget(target)
	if target.SupplierID != nil {
		out.Supplier, err = s.repo.GetSupplier(ctx, *target.SupplierID)
		if err != nil {
			return nil, err
		}
		out.Supplier.Targets = []*UpstreamTarget{target}
		out.Supplier.Wallets = upstreamSupplierWallets(out.Supplier.Targets)
	}
	return out, nil
}
