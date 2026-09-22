package service

import (
	"context"
	"errors"
	"time"
)

const (
	OpenAIReauthEnabledKey  = "openai_auto_reauth_enabled"
	OpenAIReauthPendingKey  = "openai_auto_reauth_pending"
	OpenAIReauthMaxAttempts = 3
)

var (
	ErrOpenAIReauthNotConfigured  = errors.New("openai auto reauthorization is not configured")
	ErrOpenAIReauthInvalidAccount = errors.New("openai auto reauthorization requires a primary OpenAI OAuth account with one fixed active proxy")
)

// OpenAIReauthStatus is safe to expose through the administrative API. Error
// codes must be fixed application codes, never upstream response bodies.
type OpenAIReauthStatus struct {
	AccountID     int64      `json:"account_id"`
	Email         string     `json:"email,omitempty"`
	Enabled       bool       `json:"enabled"`
	Status        string     `json:"status"`
	Stage         string     `json:"stage"`
	Generation    int64      `json:"generation"`
	Attempts      int        `json:"attempts"`
	NextAttemptAt *time.Time `json:"next_attempt_at,omitempty"`
	LastErrorCode string     `json:"last_error_code,omitempty"`
	LastSuccessAt *time.Time `json:"last_success_at,omitempty"`
	TriggerReason string     `json:"trigger_reason,omitempty"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

// The encrypted secret lives separately from account credentials and extra.
type OpenAIReauthConfig struct {
	OpenAIReauthStatus
	EncryptedSecret string `json:"-"`
}

type OpenAIReauthJob struct {
	OpenAIReauthConfig
	LeaseID                 string    `json:"-"`
	LeaseUntil              time.Time `json:"-"`
	ExpectedCredentialsHash string    `json:"-"`
	ExpectedProxyHash       string    `json:"-"`
}

type OpenAIReauthRepository interface {
	// Save replaces the encrypted secret when nonempty; an empty value preserves
	// the existing secret. Every save cancels prior leases and increments generation.
	Save(context.Context, int64, string, bool) error
	// SaveBinding changes login configuration without scheduling a login or
	// changing existing tokens. The account snapshot prevents an identity race.
	SaveBinding(context.Context, *Account, string, string, bool) error
	Get(context.Context, int64) (*OpenAIReauthConfig, error)
	ListStatuses(context.Context, int) ([]OpenAIReauthStatus, error)
	// Enqueue coalesces jobs and returns true if this configuration owns an
	// authorization fault, including an already queued or blocked job.
	Enqueue(context.Context, int64, string) (bool, error)
	// Claim returns nil, nil when no eligible job exists. Expired leases are
	// recovered subject to the same finite attempt limit as regular retries.
	Claim(context.Context, string, time.Duration) (*OpenAIReauthJob, error)
	// Validate checks the lease and immutable account/proxy snapshots immediately
	// before each network stage, not only when new tokens are committed.
	Validate(context.Context, *OpenAIReauthJob) (bool, error)
	SetStage(context.Context, *OpenAIReauthJob, string) (bool, error)
	// Fail releases the lease. retryAfter <= 0 makes the failure terminal.
	Fail(context.Context, *OpenAIReauthJob, string, time.Duration, bool) (bool, error)
	// Complete atomically merges an allowlisted token patch and publishes a
	// scheduler outbox event. A stale lease/account snapshot returns false, nil.
	Complete(context.Context, *OpenAIReauthJob, *Account, map[string]any) (bool, error)
	// CompleteManual applies tokens obtained by a verified OAuth session and
	// cancels the pending job without changing manual controls or cooldowns.
	CompleteManual(context.Context, *Account, int64, map[string]any) (bool, error)
}
