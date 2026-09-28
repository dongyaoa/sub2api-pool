//go:build unit

package service

import (
	"encoding/json"
	"math"
	"reflect"
	"testing"
)

func TestIntelligenceFingerprintReferenceNormalization(t *testing.T) {
	for _, tc := range []struct{ kind, raw, answer, category string }{
		{"int", "**４７**", "47", "valid"},
		{"int", "٤٧", "47", "valid"},
		{"int", "۴۷", "47", "valid"},
		{"int", "四十七。", "47", "valid"},
		{"int", "一百", "100", "valid"},
		{"int", "十", "10", "valid"},
		{"int", "forty-seven", "47", "valid"},
		{"int", "one hundred", "1", "valid"},
		{"int", "101", "101", "invalid"},
		{"int", "0", "0", "invalid"},
		{"int", "A number: 47", "a", "invalid"},
		{"letter", "Zed!", "z", "valid"},
		{"letter", "QUEUE", "q", "valid"},
		{"letter", "abc", "abc", "invalid"},
		{"color", "Grey", "gray", "valid"},
		{"color", "蓝色", "蓝", "valid"},
		{"color", "Aqua", "cyan", "valid"},
		{"coin", "正面！", "heads", "valid"},
		{"coin", "花", "tails", "valid"},
		{"coin", "HEAD", "heads", "valid"},
		{"word", "Snow leopard", "snow", "valid"},
		{"word", "“上海”", "上海", "valid"},
		{"word", "🦊", "", "empty"},
		{"int", "", "", "empty"},
		{"int", "As an AI, I cannot pick", "", "refusal"},
		{"word", "抱歉，不能回答", "", "refusal"},
	} {
		t.Run(tc.kind+"/"+tc.raw, func(t *testing.T) {
			got, category := normalizeIntelligenceFingerprintAnswer(tc.raw, intelligenceFingerprintProbe{Kind: tc.kind, Lo: 1, Hi: 100})
			if got != tc.answer || category != tc.category {
				t.Fatalf("normalization = (%q,%q), want (%q,%q)", got, category, tc.answer, tc.category)
			}
		})
	}
}

func repeatedIntelligenceFingerprint(answer string, count int) map[string][]string {
	cells := make(map[string][]string)
	for _, probe := range intelligenceFingerprintQuickProbes() {
		for range count {
			cells[probe.ID] = append(cells[probe.ID], answer)
		}
	}
	return cells
}

func TestIntelligenceFingerprintStatisticsAndSampleFloor(t *testing.T) {
	if got := intelligenceFingerprintJSD([]string{"a", "a"}, []string{"b"}); got != 1 {
		t.Fatalf("disjoint distance = %v", got)
	}
	if got := intelligenceFingerprintJSD([]string{"a", "b"}, []string{"a", "a", "b", "b"}); got != 0 {
		t.Fatalf("equal distributions distance = %v", got)
	}
	if got := intelligenceFingerprintJSD([]string{"a"}, []string{"a", "b"}); math.Abs(got-0.31127812445913283) > 1e-12 {
		t.Fatalf("unequal distributions distance = %v", got)
	}
	a, b := repeatedIntelligenceFingerprint("a", 25), repeatedIntelligenceFingerprint("b", 25)
	entries, mean := compareIntelligenceFingerprintCells(a, b)
	if len(entries) != 4 || mean == nil || *mean != 1 {
		t.Fatalf("comparison = %+v, %v", entries, mean)
	}
	if p := intelligenceFingerprintPermutation(a, b, "different"); p == nil || *p > 0.004 {
		t.Fatalf("disjoint p = %v", p)
	}
	if p := intelligenceFingerprintPermutation(a, a, "equal"); p == nil || *p != 1 {
		t.Fatalf("identical p = %v", p)
	}
	if d := intelligenceFingerprintSplitHalf(a); d == nil || *d != 0 {
		t.Fatalf("self distance = %v", d)
	}
	delete(a, intelligenceFingerprintProbes[0].ID)
	if _, mean := compareIntelligenceFingerprintCells(a, b); mean != nil {
		t.Fatal("three cells must not produce attribution")
	}
	if _, mean := compareIntelligenceFingerprintCells(repeatedIntelligenceFingerprint("a", 9), b); mean != nil {
		t.Fatal("nine samples per cell must not produce attribution")
	}
}

func TestIntelligenceFingerprintAttributionOutcomes(t *testing.T) {
	a, b := repeatedIntelligenceFingerprint("a", 25), repeatedIntelligenceFingerprint("b", 25)
	baselines := []intelligenceFingerprintBaseline{{Model: "original", Cells: a}, {Model: "substitute", Cells: b}}
	for _, tc := range []struct {
		name, model string
		samples     map[string][]string
		status      string
	}{
		{"consistent", "original", a, "consistent"},
		{"substitution", "original", b, "substitution"},
		{"unrecognized difference", "original", repeatedIntelligenceFingerprint("c", 25), "different"},
		{"unknown model", "unknown", a, "no_baseline"},
		{"too few samples", "original", repeatedIntelligenceFingerprint("a", 9), "insufficient"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := attributeIntelligenceFingerprintWithBaselines(tc.model, tc.samples, baselines)
			if r.Status != tc.status {
				t.Fatalf("result = %+v, want %s", r, tc.status)
			}
		})
	}
	ambiguous := []intelligenceFingerprintBaseline{{Model: "other", Cells: a}, {Model: "original", Cells: a}}
	if r := attributeIntelligenceFingerprintWithBaselines("original", a, ambiguous); r.Status != "ambiguous" {
		t.Fatalf("indistinguishable references must remain ambiguous: %+v", r)
	}
	unstable := repeatedIntelligenceFingerprint("a", 24)
	for cell, answers := range unstable {
		for i := range answers {
			if i%2 == 1 {
				unstable[cell][i] = "b"
			}
		}
	}
	if r := attributeIntelligenceFingerprintWithBaselines("original", unstable, []intelligenceFingerprintBaseline{{Model: "original", Cells: unstable}}); r.Status != "unstable" {
		t.Fatalf("unstable samples must not pass: %+v", r)
	}
}

func TestIntelligenceFingerprintBaselineCoverageAndProtocol(t *testing.T) {
	if len(intelligenceFingerprintProbes) != 16 {
		t.Fatal("reference must have 16 cells")
	}
	want := map[string]bool{"gpt-6-astra": true, "gpt-6-sol": true, "gpt-6-luna": true, "gpt-5.6-sol": true, "gpt-5.6-luna": true, "gpt-5.6-terra": true, "gpt-5.5": true}
	for _, baseline := range intelligenceFingerprintBaselines {
		if !want[baseline.Model] {
			t.Fatalf("unknown/duplicate baseline: %s", baseline.Model)
		}
		delete(want, baseline.Model)
		for _, probe := range intelligenceFingerprintProbes {
			if got := len(baseline.Cells[probe.ID]); got < intelligenceFingerprintMinValid || got > 25 {
				t.Fatalf("%s/%s reference size = %d", baseline.Model, probe.ID, got)
			}
		}
	}
	if len(want) != 0 {
		t.Fatalf("missing baselines: %v", want)
	}
	probes := intelligenceFingerprintQuickProbes()
	if len(probes)*intelligenceFingerprintQuickSamples != 60 || intelligenceFingerprintReasoning != "low" || intelligenceFingerprintTemperature != 1 {
		t.Fatal("quick collection protocol changed without updating baselines")
	}
	if !intelligenceFingerprintHasBaseline(IntelligenceMonitorModel) || intelligenceFingerprintHasBaseline("uncollected-model") {
		t.Fatal("monitor model baseline availability incorrect")
	}
	probes[0].Prompts[0] = "mutated"
	if intelligenceFingerprintQuickProbes()[0].Prompts[0] == "mutated" {
		t.Fatal("caller mutated reference protocol")
	}
}

func TestIntelligenceFingerprintActualReferenceIsDeterministic(t *testing.T) {
	var baseline intelligenceFingerprintBaseline
	for _, candidate := range intelligenceFingerprintBaselines {
		if candidate.Model == IntelligenceMonitorModel {
			baseline = candidate
			break
		}
	}
	cells := make(map[string][]string)
	for _, probe := range intelligenceFingerprintQuickProbes() {
		cells[probe.ID] = baseline.Cells[probe.ID]
	}
	first := attributeIntelligenceFingerprint(IntelligenceMonitorModel, cells)
	second := attributeIntelligenceFingerprint(IntelligenceMonitorModel, cells)
	if !reflect.DeepEqual(first, second) {
		t.Fatal("same samples produced different attribution")
	}
	if first.Status != "consistent" || first.Nearest != IntelligenceMonitorModel {
		t.Fatalf("self-reference result = %+v", first)
	}
	encoded, err := json.Marshal(first)
	if err != nil {
		t.Fatal(err)
	}
	var decoded IntelligenceFingerprintAttribution
	if err = json.Unmarshal(encoded, &decoded); err != nil || !reflect.DeepEqual(first, decoded) {
		t.Fatalf("result JSON roundtrip failed: %v", err)
	}
}

func BenchmarkIntelligenceFingerprintQuickAttribution(b *testing.B) {
	cells := make(map[string][]string)
	for _, baseline := range intelligenceFingerprintBaselines {
		if baseline.Model == IntelligenceMonitorModel {
			for _, probe := range intelligenceFingerprintQuickProbes() {
				cells[probe.ID] = baseline.Cells[probe.ID][:intelligenceFingerprintQuickSamples]
			}
		}
	}
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		attributeIntelligenceFingerprint(IntelligenceMonitorModel, cells)
	}
}
