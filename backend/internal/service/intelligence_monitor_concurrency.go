package service

import (
	"context"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

const IntelligenceMonitorMaxConcurrency = 256
const IntelligenceMonitorCandyMaxConcurrency = 128
const IntelligenceMonitorConcurrencySettingKey = "intelligence_monitor_concurrency"

var ErrIntelligenceConcurrencyUnavailable = infraerrors.ServiceUnavailable("INTELLIGENCE_CONCURRENCY_UNAVAILABLE", "intelligence monitoring concurrency settings are unavailable")

type IntelligenceMonitorConcurrency struct {
	MaxConcurrency      int `json:"max_concurrency"`
	CandyMaxConcurrency int `json:"candy_max_concurrency"`
}

func (limits IntelligenceMonitorConcurrency) Validate() error {
	if limits.MaxConcurrency < 1 || limits.MaxConcurrency > IntelligenceMonitorMaxConcurrency {
		return ErrIntelligenceInvalid.WithMetadata(map[string]string{"field": "max_concurrency", "detail": "choose an integer concurrency between 1 and 256"})
	}
	if limits.CandyMaxConcurrency < 1 || limits.CandyMaxConcurrency > IntelligenceMonitorCandyMaxConcurrency {
		return ErrIntelligenceInvalid.WithMetadata(map[string]string{"field": "candy_max_concurrency", "detail": "choose an integer concurrency between 1 and 128"})
	}
	return nil
}

type IntelligenceMonitorConcurrencySettings struct {
	IntelligenceMonitorConcurrency
	Source         string `json:"source"`
	PelicanRunning int    `json:"pelican_running"`
	PelicanPending int    `json:"pelican_pending"`
	CandyRunning   int    `json:"candy_running"`
	CandyPending   int    `json:"candy_pending"`
}

type IntelligenceMonitorConcurrencyRepository interface {
	GetConcurrency(context.Context) (*IntelligenceMonitorConcurrencySettings, error)
	SaveConcurrency(context.Context, IntelligenceMonitorConcurrency) error
}

func (s *IntelligenceMonitorService) GetConcurrency(ctx context.Context) (*IntelligenceMonitorConcurrencySettings, error) {
	repo, ok := s.repo.(IntelligenceMonitorConcurrencyRepository)
	if !ok {
		return nil, ErrIntelligenceConcurrencyUnavailable
	}
	settings, err := repo.GetConcurrency(ctx)
	if err != nil {
		return nil, err
	}
	if settings == nil {
		return nil, ErrIntelligenceConcurrencyUnavailable
	}
	if settings.Source == "deployment" {
		settings.MaxConcurrency, settings.CandyMaxConcurrency = intelligenceMonitorConcurrency(s.cfg)
	}
	if err := settings.Validate(); err != nil {
		return nil, err
	}
	return settings, nil
}

func (s *IntelligenceMonitorService) UpdateConcurrency(ctx context.Context, limits IntelligenceMonitorConcurrency) (*IntelligenceMonitorConcurrencySettings, error) {
	if err := limits.Validate(); err != nil {
		return nil, err
	}
	repo, ok := s.repo.(IntelligenceMonitorConcurrencyRepository)
	if !ok {
		return nil, ErrIntelligenceConcurrencyUnavailable
	}
	if err := repo.SaveConcurrency(ctx, limits); err != nil {
		return nil, err
	}
	// Existing executions retain their slots. The authoritative database limit
	// is checked at every claim, so lowering never cancels or restarts a request.
	s.notify()
	return s.GetConcurrency(ctx)
}
