package service

import (
	"context"
	"strings"
	"time"
	"unicode/utf8"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const IntelligencePublicDisplaySettingKey = "intelligence_monitor_public_display"
const IntelligencePublicDisplayMaxPlans = 200

var ErrPublicPelicanUnavailable = infraerrors.ServiceUnavailable("PELICAN_MONITOR_UNAVAILABLE", "pelican monitoring is temporarily unavailable")

type PublicPelicanConfig struct {
	Enabled     bool   `json:"enabled"`
	Title       string `json:"title"`
	Description string `json:"description"`
	Notice      string `json:"notice"`
}

type IntelligencePublicDisplay struct {
	PublicPelicanConfig
	PlanIDs []int64 `json:"plan_ids"`
}

func DefaultIntelligencePublicDisplay() IntelligencePublicDisplay {
	return IntelligencePublicDisplay{PublicPelicanConfig: PublicPelicanConfig{Title: "鹈鹕监测"}, PlanIDs: []int64{}}
}

func (cfg *IntelligencePublicDisplay) Normalize() error {
	cfg.Title, cfg.Description, cfg.Notice = strings.TrimSpace(cfg.Title), strings.TrimSpace(cfg.Description), strings.TrimSpace(cfg.Notice)
	if cfg.Title == "" {
		cfg.Title = "鹈鹕监测"
	}
	if !utf8.ValidString(cfg.Title) || !utf8.ValidString(cfg.Description) || !utf8.ValidString(cfg.Notice) || utf8.RuneCountInString(cfg.Title) > 60 || utf8.RuneCountInString(cfg.Description) > 240 || utf8.RuneCountInString(cfg.Notice) > 1000 || len(cfg.PlanIDs) > IntelligencePublicDisplayMaxPlans {
		return ErrIntelligenceInvalid
	}
	ids := make([]int64, 0, len(cfg.PlanIDs))
	seen := make(map[int64]bool, len(cfg.PlanIDs))
	for _, id := range cfg.PlanIDs {
		if id <= 0 {
			return ErrIntelligenceInvalid
		}
		if !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	cfg.PlanIDs = ids
	return nil
}

// These public DTOs deliberately have no credentials, source identities,
// snapshots, raw model text, internal plan names, or upstream diagnostics.
type PublicPelicanRun struct {
	ID              int64      `json:"id"`
	PlanID          int64      `json:"plan_id"`
	Status          string     `json:"status"`
	Model           string     `json:"model"`
	ReasoningEffort string     `json:"reasoning_effort"`
	CreatedAt       time.Time  `json:"created_at"`
	StartedAt       *time.Time `json:"started_at"`
	FinishedAt      *time.Time `json:"finished_at"`
	DurationMS      *int64     `json:"duration_ms"`
	Error           string     `json:"error"`
}

type PublicPelicanRunDetail struct {
	PublicPelicanRun
	HTML string `json:"html"`
}

type PublicPelicanPlan struct {
	ID                  int64               `json:"id"`
	GroupName           string              `json:"group_name"`
	GroupRateMultiplier *float64            `json:"group_rate_multiplier"`
	Enabled             bool                `json:"enabled"`
	IntervalSeconds     int                 `json:"interval_seconds"`
	NextRunAt           *time.Time          `json:"next_run_at"`
	LastRunAt           *time.Time          `json:"last_run_at"`
	Model               string              `json:"model"`
	ReasoningEffort     string              `json:"reasoning_effort"`
	LatestRun           *PublicPelicanRun   `json:"latest_run"`
	RecentRuns          []*PublicPelicanRun `json:"recent_runs"`
}

type PublicPelicanPage struct {
	Config     PublicPelicanConfig  `json:"config"`
	ServerTime time.Time            `json:"server_time"`
	Items      []*PublicPelicanPlan `json:"items"`
}

type IntelligencePublicDisplayRepository interface {
	GetPublicDisplay(context.Context) (*IntelligencePublicDisplay, error)
	SavePublicDisplay(context.Context, IntelligencePublicDisplay) error
	ListPublicPelican(context.Context, []int64) (*PublicPelicanPage, error)
	GetPublicPelicanRun(context.Context, int64, []int64) (*PublicPelicanRunDetail, error)
}

type publicPelicanGroupAuthorizer interface {
	GetAvailableGroups(context.Context, int64) ([]Group, error)
}

func (s *IntelligenceMonitorService) GetPublicDisplay(ctx context.Context) (*IntelligencePublicDisplay, error) {
	repo, ok := s.repo.(IntelligencePublicDisplayRepository)
	if !ok {
		return nil, ErrPublicPelicanUnavailable
	}
	cfg, err := repo.GetPublicDisplay(ctx)
	if err != nil || cfg == nil {
		return nil, ErrPublicPelicanUnavailable
	}
	if cfg.Normalize() != nil {
		return nil, ErrPublicPelicanUnavailable
	}
	return cfg, nil
}

func (s *IntelligenceMonitorService) UpdatePublicDisplay(ctx context.Context, cfg IntelligencePublicDisplay) (*IntelligencePublicDisplay, error) {
	if err := cfg.Normalize(); err != nil {
		return nil, err
	}
	repo, ok := s.repo.(IntelligencePublicDisplayRepository)
	if !ok {
		return nil, ErrPublicPelicanUnavailable
	}
	if err := repo.SavePublicDisplay(ctx, cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func (s *IntelligenceMonitorService) PublicPelicanConfig(ctx context.Context) (*PublicPelicanConfig, error) {
	cfg, err := s.GetPublicDisplay(ctx)
	if err != nil {
		return nil, err
	}
	return &cfg.PublicPelicanConfig, nil
}

func (s *IntelligenceMonitorService) publicPelicanAllowedGroups(ctx context.Context, userID int64) ([]int64, error) {
	if userID <= 0 || s.publicGroups == nil {
		return nil, ErrPublicPelicanUnavailable
	}
	groups, err := s.publicGroups.GetAvailableGroups(ctx, userID)
	if err != nil {
		return nil, ErrPublicPelicanUnavailable
	}
	ids := make([]int64, 0, len(groups))
	for _, group := range groups {
		if group.ID > 0 && group.Status == StatusActive {
			ids = append(ids, group.ID)
		}
	}
	return ids, nil
}

func (s *IntelligenceMonitorService) ListPublicPelican(ctx context.Context, userID int64) (*PublicPelicanPage, error) {
	cfg, err := s.PublicPelicanConfig(ctx)
	if err != nil {
		return nil, err
	}
	if !cfg.Enabled {
		return &PublicPelicanPage{Config: *cfg, ServerTime: time.Now().UTC(), Items: []*PublicPelicanPlan{}}, nil
	}
	ids, err := s.publicPelicanAllowedGroups(ctx, userID)
	if err != nil {
		return nil, err
	}
	repo, ok := s.repo.(IntelligencePublicDisplayRepository)
	if !ok {
		return nil, ErrPublicPelicanUnavailable
	}
	page, err := repo.ListPublicPelican(ctx, ids)
	if err != nil || page == nil {
		return nil, ErrPublicPelicanUnavailable
	}
	return page, nil
}

func (s *IntelligenceMonitorService) GetPublicPelicanRun(ctx context.Context, userID, id int64) (*PublicPelicanRunDetail, error) {
	if id <= 0 {
		return nil, ErrIntelligenceNotFound
	}
	cfg, err := s.PublicPelicanConfig(ctx)
	if err != nil {
		return nil, err
	}
	if !cfg.Enabled {
		return nil, ErrIntelligenceNotFound
	}
	ids, err := s.publicPelicanAllowedGroups(ctx, userID)
	if err != nil {
		return nil, err
	}
	repo, ok := s.repo.(IntelligencePublicDisplayRepository)
	if !ok {
		return nil, ErrPublicPelicanUnavailable
	}
	return repo.GetPublicPelicanRun(ctx, id, ids)
}
