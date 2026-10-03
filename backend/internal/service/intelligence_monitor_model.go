package service

import "strings"

// Omitted models retain the original comparison model for legacy plans/runs.
func intelligenceMonitorModel(model string) string {
	model = strings.TrimSpace(model)
	if model == "" {
		return IntelligenceMonitorModel
	}
	return model
}

func validateIntelligenceMonitorModel(model string) error {
	switch model {
	case IntelligenceMonitorModel, IntelligenceMonitorSolModel:
		return nil
	default:
		return ErrIntelligenceInvalid.WithMetadata(map[string]string{"field": "model", "detail": "choose gpt-6-astra or gpt-6.1-sol"})
	}
}
