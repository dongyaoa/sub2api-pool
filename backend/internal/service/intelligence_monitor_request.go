package service

// Requests use only server-owned test definitions. A captured prompt or a
// caller-supplied field must not turn the monitor into an arbitrary tool task.
func intelligenceTestRequestDefinition(run *IntelligenceMonitorRun) (prompt, instructions, effort string, limit int, temperature *float64, valid bool) {
	if run == nil {
		return
	}
	if run.fingerprintProbeID != "" {
		if run.TestKind != IntelligenceMonitorTestCandy {
			return
		}
		for _, probe := range intelligenceFingerprintQuickProbes() {
			if probe.ID == run.fingerprintProbeID && run.fingerprintPromptIndex >= 0 && run.fingerprintPromptIndex < len(probe.Prompts) {
				temp := intelligenceFingerprintTemperature
				return probe.Prompts[run.fingerprintPromptIndex], probe.Instructions, intelligenceFingerprintReasoning, 0, &temp, true
			}
		}
		return
	}
	switch run.TestKind {
	case "", IntelligenceMonitorTestPelican:
		return IntelligenceMonitorPrompt, "", IntelligenceMonitorReasoning, 24000, nil, true
	case IntelligenceMonitorTestCandy:
		return IntelligenceMonitorCandyPrompt, "", IntelligenceMonitorReasoning, IntelligenceMonitorCandyMaxOutputTokens, nil, true
	default:
		return
	}
}
