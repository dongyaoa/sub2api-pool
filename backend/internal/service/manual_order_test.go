package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestManualOrderInputValidation(t *testing.T) {
	one, zero := int64(1), int64(0)
	for _, in := range []UpstreamOrderInput{
		{}, {Scope: "other", IDs: []int64{}}, {Scope: "suppliers"},
		{Scope: "suppliers", IDs: []int64{1, 1}}, {Scope: "monitors", IDs: []int64{0}},
		{Scope: "monitors", IDs: []int64{-2}}, {Scope: "groups", IDs: []int64{}},
		{Scope: "groups", SupplierID: &zero, IDs: []int64{}},
		{Scope: "suppliers", SupplierID: &one, IDs: []int64{1}},
		{Scope: "monitors", SupplierID: &one, IDs: []int64{}},
	} {
		require.ErrorIs(t, in.Validate(), ErrManualOrderInvalid, "%+v", in)
		// Invalid requests cannot reach a repository or trigger any side effect.
		require.ErrorIs(t, (*UpstreamCenterService)(nil).SaveOrder(context.Background(), in), ErrManualOrderInvalid)
	}
	for _, in := range []UpstreamOrderInput{
		{Scope: "suppliers", IDs: []int64{}}, {Scope: "monitors", IDs: []int64{2, 1}},
		{Scope: "groups", SupplierID: &one, IDs: []int64{2, 1}},
	} {
		require.NoError(t, in.Validate())
	}
	for _, in := range []IntelligenceOrderInput{
		{}, {Scope: "external", IDs: []int64{}}, {Scope: "oauth"},
		{Scope: "oauth", IDs: []int64{1, 1}}, {Scope: "intelligence", IDs: []int64{-1}},
	} {
		require.ErrorIs(t, in.Validate(), ErrManualOrderInvalid)
		require.ErrorIs(t, (*IntelligenceMonitorService)(nil).SaveOrder(context.Background(), in), ErrManualOrderInvalid)
	}
	require.NoError(t, (IntelligenceOrderInput{Scope: "oauth", IDs: []int64{}}).Validate())
	require.NoError(t, (IntelligenceOrderInput{Scope: "intelligence", IDs: []int64{2, 1}}).Validate())
}
