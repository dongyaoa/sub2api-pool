package service

import (
	"context"
	"errors"
	"log/slog"
	"math/rand"
	"strings"
	"time"
)

// Comparison is CPU work, unlike model requests. Bound its transient memory
// independently without occupying additional request slots.
var intelligenceFingerprintComparisonSlots = make(chan struct{}, 2)

// collectIntelligenceFingerprint keeps the original candy reply and its source
// binding intact. Each worker sends one independent probe at a time: increasing
// candy concurrency never secretly multiplies active upstream requests by 60.
// All 61 requests share the existing run deadline and lease.
func (s *IntelligenceMonitorService) collectIntelligenceFingerprint(ctx context.Context, run *IntelligenceMonitorRun, key string) {
	if run.TestKind != IntelligenceMonitorTestCandy {
		return
	}
	started := time.Now()
	probes := intelligenceFingerprintQuickProbes()
	fingerprint := &IntelligenceFingerprintResult{
		Method: IntelligenceFingerprintAlgorithmVersion, Mode: "quick", Status: "collecting",
		Model: IntelligenceMonitorModel, ReasoningEffort: intelligenceFingerprintReasoning,
		Total: len(probes) * intelligenceFingerprintQuickSamples,
	}
	run.Fingerprint = fingerprint
	finishFailed := func(status, message string) {
		passed := false
		fingerprint.Status, fingerprint.Error, fingerprint.Passed = status, message, &passed
		fingerprint.DurationMS = time.Since(started).Milliseconds()
	}
	if run.Error != "" || strings.TrimSpace(run.RawText) == "" {
		finishFailed("failed", "fingerprint collection skipped because the candy request did not complete")
		return
	}
	answer, correct := gradeIntelligenceCandyAnswer(run.RawText)
	run.Answer, run.Correct = answer, &correct
	lastPublished := time.Time{}
	publish := func(force bool) bool {
		fingerprint.DurationMS = time.Since(started).Milliseconds()
		if !force && time.Since(lastPublished) < time.Second {
			return true
		}
		repo, ok := s.repo.(IntelligenceMonitorCandyProgressRepository)
		if !ok {
			return true
		}
		lastPublished = time.Now()
		progressCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		defer cancel()
		err := repo.SaveCandyProgress(progressCtx, run)
		if errors.Is(err, ErrIntelligenceNotFound) {
			finishFailed("failed", "monitoring run is no longer active")
			return false
		}
		if err != nil && ctx.Err() == nil {
			slog.Warn("intelligence fingerprint progress persistence failed", "run_id", run.ID, "error", err)
		}
		return true
	}
	if !publish(true) {
		return
	}
	// Randomize probe order and phrasing as in the reference collector. Never
	// reuse a response or compare the candy answer to a model fingerprint.
	jobs := make([]int, 0, fingerprint.Total)
	for i := range probes {
		for sample := 0; sample < intelligenceFingerprintQuickSamples; sample++ {
			jobs = append(jobs, i)
		}
	}
	rand.Shuffle(len(jobs), func(i, j int) { jobs[i], jobs[j] = jobs[j], jobs[i] })
	valid := make(map[string][]string, len(probes))
	consecutiveErrors := 0
	for _, index := range jobs {
		if ctx.Err() != nil {
			finishFailed("timeout", "fingerprint collection cancelled or exceeded this round's configured time limit")
			return
		}
		probe := probes[index]
		probeRun := *run
		probeRun.RawText, probeRun.HTML, probeRun.Fingerprint = "", "", nil
		probeRun.SourceSnapshot = make(map[string]any, len(run.SourceSnapshot))
		for k, value := range run.SourceSnapshot {
			probeRun.SourceSnapshot[k] = value
		}
		probeRun.fingerprintProbeID = probe.ID
		probeRun.fingerprintPromptIndex = rand.Intn(len(probe.Prompts))
		var status *int
		var text, message string
		if run.SourceType == "openai_oauth" {
			status, text, message = s.generateOpenAIOAuth(ctx, &probeRun)
		} else {
			status, text, message = s.generate(ctx, &probeRun, key)
		}
		sample := IntelligenceFingerprintSample{Probe: probe.ID, HTTPStatus: status}
		if message != "" {
			sample.Category, sample.Error = "error", message
			consecutiveErrors++
		} else if len(text) > 4096 {
			consecutiveErrors = 0
			// Probe answers are tiny tokens. Bound normalization/storage for an
			// upstream that ignores the fixed task and returns a large document.
			sample.Category, sample.Error = "invalid", "probe response exceeded the short-answer limit"
		} else {
			consecutiveErrors = 0
			sample.Answer, sample.Category = normalizeIntelligenceFingerprintAnswer(text, probe)
		}
		fingerprint.Samples = append(fingerprint.Samples, sample)
		fingerprint.Done++
		if sample.Category == "valid" {
			valid[probe.ID] = append(valid[probe.ID], sample.Answer)
			fingerprint.Valid++
		} else {
			fingerprint.Errors++
		}
		if consecutiveErrors >= 8 {
			// Match the reference collector's failure circuit breaker. A
			// rejected credential or unsupported probe parameter should not
			// hold a request slot through all remaining requests.
			finishFailed("failed", "fingerprint collection stopped after 8 consecutive request errors")
			return
		}
		if !publish(false) {
			return
		}
	}
	fingerprint.Status = "comparing"
	if !publish(true) {
		return
	}
	select {
	case intelligenceFingerprintComparisonSlots <- struct{}{}:
		defer func() { <-intelligenceFingerprintComparisonSlots }()
	case <-ctx.Done():
		finishFailed("timeout", "fingerprint comparison cancelled or exceeded this round's configured time limit")
		return
	}
	attribution := attributeIntelligenceFingerprint(fingerprint.Model, valid)
	fingerprint.Attribution = &attribution
	passed := attribution.Status == "consistent"
	fingerprint.Status, fingerprint.Passed = "completed", &passed
	fingerprint.DurationMS = time.Since(started).Milliseconds()
	if ctx.Err() != nil {
		finishFailed("timeout", "fingerprint comparison exceeded this round's configured time limit")
	}
}
