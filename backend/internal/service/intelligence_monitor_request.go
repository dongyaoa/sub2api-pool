package service

// Requests use only server-owned test definitions. A captured prompt or a
// caller-supplied field must not turn the monitor into an arbitrary tool task.
func intelligenceTestRequestDefinition(run *IntelligenceMonitorRun) (prompt string, limit int, valid bool) {
	if run == nil {
		return
	}
	switch run.TestKind {
	case "", IntelligenceMonitorTestPelican:
		return IntelligenceMonitorPrompt, 24000, true
	case IntelligenceMonitorTestCandy:
		return IntelligenceMonitorCandyPrompt, IntelligenceMonitorCandyMaxOutputTokens, true
	default:
		return
	}
}
