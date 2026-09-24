package service

import (
	"context"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

var (
	ErrManualOrderInvalid  = infraerrors.BadRequest("MANUAL_ORDER_INVALID", "invalid manual order; provide a valid scope and a complete list of unique positive IDs")
	ErrManualOrderConflict = infraerrors.Conflict("MANUAL_ORDER_CONFLICT", "this list has changed; refresh it and try ordering again")
)

type UpstreamOrderInput struct {
	Scope      string  `json:"scope"`
	SupplierID *int64  `json:"supplier_id"`
	IDs        []int64 `json:"ids"`
}

func (in UpstreamOrderInput) Validate() error {
	switch in.Scope {
	case "suppliers", "monitors":
		if in.SupplierID != nil {
			return ErrManualOrderInvalid
		}
	case "groups":
		if in.SupplierID == nil || *in.SupplierID <= 0 {
			return ErrManualOrderInvalid
		}
	default:
		return ErrManualOrderInvalid
	}
	return validateManualOrderIDs(in.IDs)
}

type IntelligenceOrderInput struct {
	Scope string  `json:"scope"`
	IDs   []int64 `json:"ids"`
}

func (in IntelligenceOrderInput) Validate() error {
	if in.Scope != "intelligence" && in.Scope != "oauth" {
		return ErrManualOrderInvalid
	}
	return validateManualOrderIDs(in.IDs)
}

func validateManualOrderIDs(ids []int64) error {
	// Distinguish an explicitly empty list from a missing/null request property.
	if ids == nil {
		return ErrManualOrderInvalid
	}
	seen := make(map[int64]struct{}, len(ids))
	for _, id := range ids {
		if id <= 0 {
			return ErrManualOrderInvalid
		}
		if _, exists := seen[id]; exists {
			return ErrManualOrderInvalid
		}
		seen[id] = struct{}{}
	}
	return nil
}

func (s *UpstreamCenterService) SaveOrder(ctx context.Context, in UpstreamOrderInput) error {
	if err := in.Validate(); err != nil {
		return err
	}
	return s.repo.SaveOrder(ctx, in)
}

func (s *IntelligenceMonitorService) SaveOrder(ctx context.Context, in IntelligenceOrderInput) error {
	if err := in.Validate(); err != nil {
		return err
	}
	return s.repo.SaveOrder(ctx, in)
}
