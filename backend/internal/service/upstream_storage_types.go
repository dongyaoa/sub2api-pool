package service

import (
	"context"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

var (
	ErrUpstreamStoragePolicy        = infraerrors.BadRequest("UPSTREAM_STORAGE_POLICY_INVALID", "history retention must be 30–365 days and snapshot retention 1–90 days")
	ErrUpstreamStorageUnavailable   = infraerrors.ServiceUnavailable("UPSTREAM_STORAGE_UNAVAILABLE", "upstream storage management is unavailable")
	ErrUpstreamStorageCleanupBusy   = infraerrors.Conflict("UPSTREAM_STORAGE_CLEANUP_BUSY", "storage cleanup is already running; retry later")
	ErrUpstreamFinanceArchivedRange = infraerrors.BadRequest("UPSTREAM_FINANCE_ARCHIVED_RANGE", "older monitoring costs are hourly summaries; select complete hours or dates")
)

type UpstreamStorageCleanupResult struct {
	HistoryDeleted int64 `json:"history_deleted"`
	BalanceDeleted int64 `json:"balance_deleted"`
	BillingDeleted int64 `json:"billing_deleted"`
	HasMore        bool  `json:"has_more"`
}

type UpstreamStoragePolicy struct {
	Enabled               bool                          `json:"enabled"`
	HistoryRetentionDays  int                           `json:"history_retention_days"`
	SnapshotRetentionDays int                           `json:"snapshot_retention_days"`
	LastCleanupAt         *time.Time                    `json:"last_cleanup_at"`
	LastResult            *UpstreamStorageCleanupResult `json:"last_result"`
}

type UpstreamStoragePolicyInput struct {
	Enabled               *bool `json:"enabled"`
	HistoryRetentionDays  *int  `json:"history_retention_days"`
	SnapshotRetentionDays *int  `json:"snapshot_retention_days"`
}

type UpstreamStorageArchiveItem struct {
	Kind         string    `json:"kind"`
	ID           int64     `json:"id"`
	Name         string    `json:"name"`
	DeletedAt    time.Time `json:"deleted_at"`
	SourceType   string    `json:"source_type"`
	SupplierName string    `json:"supplier_name"`
}

type UpstreamStorageArchivePage struct {
	Items []*UpstreamStorageArchiveItem `json:"items"`
	Total int64                         `json:"total"`
}

type UpstreamStoragePurgeInput struct {
	Kind        string `json:"kind"`
	ID          int64  `json:"id"`
	ConfirmName string `json:"confirm_name"`
}

// Optional capability keeps ordinary monitoring repository consumers isolated.
type UpstreamStorageRepository interface {
	GetStoragePolicy(context.Context) (*UpstreamStoragePolicy, error)
	SaveStoragePolicy(context.Context, UpstreamStoragePolicyInput) (*UpstreamStoragePolicy, error)
	RecordStorageCleanup(context.Context, time.Time, *UpstreamStorageCleanupResult) error
	CleanupStorage(context.Context, int, int, time.Time) (*UpstreamStorageCleanupResult, error)
}
