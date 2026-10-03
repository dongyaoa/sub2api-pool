package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

type publicPelicanTestRepository struct {
	IntelligenceMonitorRepository
	cfg                   IntelligencePublicDisplay
	getErr                error
	saves, lists, details int
	groups                []int64
}

func (r *publicPelicanTestRepository) GetPublicDisplay(context.Context) (*IntelligencePublicDisplay, error) {
	cfg := r.cfg
	return &cfg, r.getErr
}
func (r *publicPelicanTestRepository) SavePublicDisplay(_ context.Context, cfg IntelligencePublicDisplay) error {
	r.cfg, r.saves = cfg, r.saves+1
	return nil
}
func (r *publicPelicanTestRepository) ListPublicPelican(_ context.Context, groups []int64) (*PublicPelicanPage, error) {
	r.lists++
	r.groups = groups
	return &PublicPelicanPage{Config: r.cfg.PublicPelicanConfig, Items: []*PublicPelicanPlan{}}, nil
}
func (r *publicPelicanTestRepository) GetPublicPelicanRun(_ context.Context, id int64, groups []int64) (*PublicPelicanRunDetail, error) {
	r.details++
	r.groups = groups
	return &PublicPelicanRunDetail{PublicPelicanRun: PublicPelicanRun{ID: id}}, nil
}

type publicPelicanTestGroups struct {
	groups []Group
	err    error
	userID int64
	calls  int
}

func (g *publicPelicanTestGroups) GetAvailableGroups(_ context.Context, userID int64) ([]Group, error) {
	g.calls++
	g.userID = userID
	return g.groups, g.err
}

func TestIntelligencePublicDisplayDefaultsAndValidation(t *testing.T) {
	repo := &publicPelicanTestRepository{cfg: DefaultIntelligencePublicDisplay()}
	svc := NewIntelligenceMonitorService(repo, nil, nil, nil, nil, nil, nil)
	cfg, err := svc.GetPublicDisplay(context.Background())
	require.NoError(t, err)
	require.False(t, cfg.Enabled)
	require.False(t, cfg.HideFailed)
	require.Equal(t, "鹈鹕监测", cfg.Title)
	require.NotNil(t, cfg.PlanIDs)
	input := IntelligencePublicDisplay{PublicPelicanConfig: PublicPelicanConfig{Enabled: true, Title: "  展示标题  ", Description: " 描述 ", Notice: " 公告 "}, PlanIDs: []int64{7, 7, 8}}
	got, err := svc.UpdatePublicDisplay(context.Background(), input)
	require.NoError(t, err)
	require.Equal(t, []int64{7, 8}, got.PlanIDs)
	require.Equal(t, "展示标题", got.Title)
	require.Equal(t, "描述", got.Description)
	require.Equal(t, "公告", got.Notice)
	for _, bad := range []IntelligencePublicDisplay{
		{PublicPelicanConfig: PublicPelicanConfig{Title: strings.Repeat("字", 61)}},
		{PublicPelicanConfig: PublicPelicanConfig{Description: strings.Repeat("字", 241)}},
		{PublicPelicanConfig: PublicPelicanConfig{Notice: strings.Repeat("字", 1001)}},
		{PlanIDs: []int64{0}}, {PlanIDs: []int64{-1}}, {PlanIDs: make([]int64, 201)},
	} {
		_, err = svc.UpdatePublicDisplay(context.Background(), bad)
		require.ErrorIs(t, err, ErrIntelligenceInvalid)
	}
	require.Equal(t, 1, repo.saves)
}

func TestIntelligencePublicPelicanGateAndGroupAuthorization(t *testing.T) {
	repo := &publicPelicanTestRepository{cfg: DefaultIntelligencePublicDisplay()}
	svc := NewIntelligenceMonitorService(repo, nil, nil, nil, nil, nil, nil)
	groups := &publicPelicanTestGroups{groups: []Group{{ID: 3, Status: StatusActive}, {ID: 4, Status: "disabled"}}}
	svc.publicGroups = groups
	page, err := svc.ListPublicPelican(context.Background(), 42)
	require.NoError(t, err)
	require.Empty(t, page.Items)
	require.NotNil(t, page.Items)
	require.Zero(t, groups.calls)
	require.Zero(t, repo.lists)
	_, err = svc.GetPublicPelicanRun(context.Background(), 42, 7)
	require.ErrorIs(t, err, ErrIntelligenceNotFound)
	require.Zero(t, repo.details)
	repo.cfg.Enabled = true
	_, err = svc.ListPublicPelican(context.Background(), 42)
	require.NoError(t, err)
	require.Equal(t, int64(42), groups.userID)
	require.Equal(t, []int64{3}, repo.groups)
	_, err = svc.GetPublicPelicanRun(context.Background(), 42, 7)
	require.NoError(t, err)
	require.Equal(t, []int64{3}, repo.groups)
	groups.err = errors.New("private upstream diagnostic")
	_, err = svc.ListPublicPelican(context.Background(), 42)
	require.ErrorIs(t, err, ErrPublicPelicanUnavailable)
	require.NotContains(t, err.Error(), "private upstream")
	_, err = svc.GetPublicPelicanRun(context.Background(), 42, 7)
	require.ErrorIs(t, err, ErrPublicPelicanUnavailable)
	require.Equal(t, 1, repo.details)
	require.Equal(t, 1, repo.lists)
	repo.getErr = errors.New("settings password=secret")
	_, err = svc.PublicPelicanConfig(context.Background())
	require.ErrorIs(t, err, ErrPublicPelicanUnavailable)
	require.NotContains(t, err.Error(), "secret")
}
