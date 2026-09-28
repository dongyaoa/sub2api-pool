package service

import "context"

type IntelligenceFingerprintSample struct {
	Probe      string `json:"probe"`
	Answer     string `json:"answer,omitempty"`
	Category   string `json:"category"`
	Error      string `json:"error,omitempty"`
	HTTPStatus *int   `json:"http_status,omitempty"`
}

type IntelligenceFingerprintResult struct {
	Method          string                              `json:"method"`
	Mode            string                              `json:"mode"`
	Status          string                              `json:"status"`
	Passed          *bool                               `json:"passed"`
	Model           string                              `json:"model"`
	ReasoningEffort string                              `json:"reasoning_effort"`
	Total           int                                 `json:"total"`
	Done            int                                 `json:"done"`
	Valid           int                                 `json:"valid"`
	Errors          int                                 `json:"errors"`
	DurationMS      int64                               `json:"duration_ms"`
	Error           string                              `json:"error,omitempty"`
	Attribution     *IntelligenceFingerprintAttribution `json:"attribution,omitempty"`
	Samples         []IntelligenceFingerprintSample     `json:"samples,omitempty"`
}

// Summary omits probe evidence and per-cell comparisons from frequent list
// reads, retaining just the declared and closest models for hover feedback.
func (r *IntelligenceFingerprintResult) Summary() *IntelligenceFingerprintResult {
	if r == nil {
		return nil
	}
	out := *r
	out.Samples = nil
	if r.Attribution != nil {
		attribution := *r.Attribution
		attribution.Warnings = nil
		attribution.Comparisons = nil
		for _, comparison := range r.Attribution.Comparisons {
			if comparison.Model == r.Model || comparison.Model == r.Attribution.Nearest {
				comparison.Cells = nil
				attribution.Comparisons = append(attribution.Comparisons, comparison)
			}
		}
		out.Attribution = &attribution
	}
	return &out
}

type IntelligenceMonitorCandyProgressRepository interface {
	SaveCandyProgress(context.Context, *IntelligenceMonitorRun) error
}
