// Adapted from haowang02/cpa-plugin-codex-candy-eval at 97d73994842d3c236d39be12654d46c2fa66283c.
// Copyright (c) 2026 Hao Wang. MIT license and baseline provenance are retained in
// data/intelligence_fingerprint/LICENSE and README.md.

package service

import (
	_ "embed"
	"encoding/json"
	"hash/crc32"
	"math"
	"math/rand"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

const (
	intelligenceFingerprintMinValid         = 10
	intelligenceFingerprintMinCells         = 4
	intelligenceFingerprintPermutations     = 1000
	intelligenceFingerprintQuickSamples     = 15
	intelligenceFingerprintReasoning        = "low"
	intelligenceFingerprintTemperature      = 1.0
	IntelligenceFingerprintAlgorithmVersion = "cpa-jsd-v1"
)

type intelligenceFingerprintProbe struct {
	ID           string   `json:"id"`
	Kind         string   `json:"kind"`
	Lo           int      `json:"lo"`
	Hi           int      `json:"hi"`
	Instructions string   `json:"instructions"`
	Prompts      []string `json:"prompts"`
}

type intelligenceFingerprintBaseline struct {
	Model string              `json:"model"`
	Cells map[string][]string `json:"cells"`
}

//go:embed data/intelligence_fingerprint/probes.json
var intelligenceFingerprintProbesJSON []byte

//go:embed data/intelligence_fingerprint/baselines.json
var intelligenceFingerprintBaselinesJSON []byte

var intelligenceFingerprintProbes = mustDecodeIntelligenceFingerprint[[]intelligenceFingerprintProbe](intelligenceFingerprintProbesJSON)
var intelligenceFingerprintBaselines = mustDecodeIntelligenceFingerprint[[]intelligenceFingerprintBaseline](intelligenceFingerprintBaselinesJSON)

// The reference quick mode collects four cells with 15 independent completions
// each. Return copies so collectors cannot mutate the embedded protocol.
func intelligenceFingerprintQuickProbes() []intelligenceFingerprintProbe {
	probes := append([]intelligenceFingerprintProbe(nil), intelligenceFingerprintProbes[:intelligenceFingerprintMinCells]...)
	for i := range probes {
		probes[i].Prompts = append([]string(nil), probes[i].Prompts...)
	}
	return probes
}

func mustDecodeIntelligenceFingerprint[T any](data []byte) T {
	var value T
	if err := json.Unmarshal(data, &value); err != nil {
		panic(err)
	}
	return value
}

var intelligenceFingerprintRefusal = regexp.MustCompile(`(?i)\bas an ai\b|\bi (?:cannot|can't|can not|won't|will not)\b|\bi'?m (?:unable|not able|sorry)\b|\bsorry,? (?:i|but)\b|\bcannot (?:comply|assist|help)\b|我不能|我无法|无法回答|不能回答|抱歉|对不起|作为(?:一个)?(?:AI|人工智能)`)
var intelligenceFingerprintWord = regexp.MustCompile(`^[a-z]+$`)
var intelligenceFingerprintDigits = regexp.MustCompile(`^[0-9]+$`)
var intelligenceFingerprintLetterNames = map[string]string{"bee": "b", "cee": "c", "dee": "d", "gee": "g", "jay": "j", "kay": "k", "el": "l", "ell": "l", "em": "m", "en": "n", "oh": "o", "pee": "p", "cue": "q", "queue": "q", "ar": "r", "es": "s", "ess": "s", "tee": "t", "vee": "v", "ex": "x", "why": "y", "zee": "z", "zed": "z"}
var intelligenceFingerprintOnes = strings.Fields("zero one two three four five six seven eight nine ten eleven twelve thirteen fourteen fifteen sixteen seventeen eighteen nineteen")
var intelligenceFingerprintTens = strings.Fields("twenty thirty forty fifty sixty seventy eighty ninety")
var intelligenceFingerprintChineseDigits = map[rune]int{'零': 0, '〇': 0, '一': 1, '二': 2, '两': 2, '三': 3, '四': 4, '五': 5, '六': 6, '七': 7, '八': 8, '九': 9}

func intelligenceFingerprintNumber(s string) (int, bool) {
	if intelligenceFingerprintDigits.MatchString(s) {
		n, err := strconv.Atoi(s)
		return n, err == nil
	}
	total, current, chinese := 0, 0, true
	for _, ch := range s {
		if n, ok := intelligenceFingerprintChineseDigits[ch]; ok {
			current = n
			continue
		}
		switch ch {
		case '十':
			total += max(current, 1) * 10
			current = 0
		case '百':
			total += max(current, 1) * 100
			current = 0
		case '千':
			total += max(current, 1) * 1000
			current = 0
		default:
			chinese = false
		}
	}
	if chinese && s != "" {
		return total + current, true
	}
	for n, word := range intelligenceFingerprintOnes {
		if s == word {
			return n, true
		}
	}
	if s == "hundred" || s == "onehundred" {
		return 100, true
	}
	for i, tens := range intelligenceFingerprintTens {
		if s == tens {
			return (i + 2) * 10, true
		}
		if strings.HasPrefix(s, tens) {
			for n := 1; n <= 9; n++ {
				if strings.TrimPrefix(s, tens) == intelligenceFingerprintOnes[n] {
					return (i+2)*10 + n, true
				}
			}
		}
	}
	return 0, false
}

func intelligenceFingerprintChinese(s string, limit int) bool {
	count := 0
	for _, ch := range s {
		if ch < '一' || ch > '鿿' {
			return false
		}
		count++
	}
	return count > 0 && count <= limit
}

func normalizeIntelligenceFingerprintAnswer(raw string, probe intelligenceFingerprintProbe) (string, string) {
	raw = strings.TrimSpace(norm.NFC.String(raw))
	if raw == "" {
		return "", "empty"
	}
	if intelligenceFingerprintRefusal.MatchString(raw) {
		return "", "refusal"
	}
	clean := strings.Map(func(ch rune) rune {
		switch {
		case ch >= '０' && ch <= '９':
			return '0' + ch - '０'
		case ch >= '\u0660' && ch <= '\u0669':
			return '0' + ch - '\u0660'
		case ch >= '\u06f0' && ch <= '\u06f9':
			return '0' + ch - '\u06f0'
		case unicode.IsSpace(ch) || unicode.IsLetter(ch) || unicode.IsNumber(ch):
			return unicode.ToLower(ch)
		default:
			return -1
		}
	}, raw)
	words := strings.Fields(clean)
	if len(words) == 0 {
		return "", "empty"
	}
	first, answer, valid := words[0], words[0], false
	switch probe.Kind {
	case "int":
		if n, ok := intelligenceFingerprintNumber(first); ok {
			answer, valid = strconv.Itoa(n), n >= probe.Lo && n <= probe.Hi
		}
	case "letter":
		if mapped := intelligenceFingerprintLetterNames[first]; mapped != "" {
			answer = mapped
		}
		valid = len(answer) == 1 && answer[0] >= 'a' && answer[0] <= 'z'
		if !valid {
			answer = first
		}
	case "color":
		if first == "grey" {
			answer = "gray"
		} else if first == "aqua" {
			answer = "cyan"
		} else if len([]rune(first)) >= 2 && intelligenceFingerprintChinese(first, len([]rune(first))) && strings.HasSuffix(first, "色") {
			answer = strings.TrimSuffix(first, "色")
		}
		valid = intelligenceFingerprintWord.MatchString(answer) || intelligenceFingerprintChinese(answer, 4)
		if !valid {
			answer = first
		}
	case "coin":
		switch {
		case first == "heads" || first == "head" || first == "字" || strings.HasPrefix(first, "正"):
			answer, valid = "heads", true
		case first == "tails" || first == "tail" || first == "花" || strings.HasPrefix(first, "反"):
			answer, valid = "tails", true
		}
	default:
		valid = intelligenceFingerprintWord.MatchString(first) || intelligenceFingerprintChinese(first, 6)
	}
	if valid {
		return answer, "valid"
	}
	return answer, "invalid"
}

func intelligenceFingerprintJSD(a, b []string) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	counts := map[string][2]float64{}
	for _, v := range a {
		c := counts[v]
		c[0]++
		counts[v] = c
	}
	for _, v := range b {
		c := counts[v]
		c[1]++
		counts[v] = c
	}
	// Sorting keeps rounding and permutation decisions reproducible across Go map iteration orders.
	keys := make([]string, 0, len(counts))
	for key := range counts {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	distance := 0.0
	for _, key := range keys {
		c := counts[key]
		p, q := c[0]/float64(len(a)), c[1]/float64(len(b))
		m := (p + q) / 2
		if p > 0 {
			distance += p * math.Log2(p/m) / 2
		}
		if q > 0 {
			distance += q * math.Log2(q/m) / 2
		}
	}
	return math.Min(1, math.Max(0, distance))
}

type IntelligenceFingerprintCellComparison struct {
	Cell   string  `json:"cell"`
	JSD    float64 `json:"jsd"`
	ValidA int     `json:"valid_a"`
	ValidB int     `json:"valid_b"`
}

type IntelligenceFingerprintComparison struct {
	Model   string                                  `json:"model"`
	MeanJSD *float64                                `json:"mean_jsd"`
	PValue  *float64                                `json:"p_value"`
	Verdict string                                  `json:"verdict"`
	SelfJSD *float64                                `json:"self_jsd"`
	Cells   []IntelligenceFingerprintCellComparison `json:"cells"`
}

func compareIntelligenceFingerprintCells(a, b map[string][]string) ([]IntelligenceFingerprintCellComparison, *float64) {
	entries := []IntelligenceFingerprintCellComparison{}
	total := 0.0
	for _, probe := range intelligenceFingerprintProbes {
		x, y := a[probe.ID], b[probe.ID]
		if len(x) < intelligenceFingerprintMinValid || len(y) < intelligenceFingerprintMinValid {
			continue
		}
		d := intelligenceFingerprintJSD(x, y)
		total += d
		entries = append(entries, IntelligenceFingerprintCellComparison{probe.ID, d, len(x), len(y)})
	}
	if len(entries) < intelligenceFingerprintMinCells {
		return entries, nil
	}
	mean := total / float64(len(entries))
	return entries, &mean
}

func intelligenceFingerprintSplitHalf(cells map[string][]string) *float64 {
	total, n := 0.0, 0
	for _, probe := range intelligenceFingerprintProbes {
		answers := cells[probe.ID]
		if len(answers) < intelligenceFingerprintMinValid {
			continue
		}
		a, b := []string{}, []string{}
		for i, answer := range answers {
			if i%2 == 0 {
				a = append(a, answer)
			} else {
				b = append(b, answer)
			}
		}
		total += intelligenceFingerprintJSD(a, b)
		n++
	}
	if n == 0 {
		return nil
	}
	mean := total / float64(n)
	return &mean
}

func intelligenceFingerprintPermutation(a, b map[string][]string, seed string) *float64 {
	entries, observed := compareIntelligenceFingerprintCells(a, b)
	if observed == nil {
		return nil
	}
	rng := rand.New(rand.NewSource(int64(crc32.ChecksumIEEE([]byte(seed)))))
	pools := make([][]string, len(entries))
	for i, e := range entries {
		pools[i] = append(append([]string{}, a[e.Cell]...), b[e.Cell]...)
	}
	hits := 0
	for range intelligenceFingerprintPermutations {
		total := 0.0
		for i, e := range entries {
			pool := pools[i]
			rng.Shuffle(len(pool), func(i, j int) { pool[i], pool[j] = pool[j], pool[i] })
			total += intelligenceFingerprintJSD(pool[:e.ValidA], pool[e.ValidA:])
		}
		if total/float64(len(entries)) >= *observed-1e-12 {
			hits++
		}
	}
	p := float64(hits+1) / float64(intelligenceFingerprintPermutations+1)
	return &p
}

func intelligenceFingerprintDistanceVerdict(d *float64) string {
	if d == nil {
		return "insufficient"
	}
	if *d <= .25 {
		return "match"
	}
	if *d <= .35 {
		return "uncertain"
	}
	return "mismatch"
}

type IntelligenceFingerprintAttribution struct {
	Status          string                              `json:"status"`
	Message         string                              `json:"message"`
	Nearest         string                              `json:"nearest,omitempty"`
	Alpha           float64                             `json:"alpha"`
	SelfJSD         *float64                            `json:"self_jsd"`
	ReferencePValue *float64                            `json:"reference_p_value,omitempty"`
	Warnings        []string                            `json:"warnings"`
	Comparisons     []IntelligenceFingerprintComparison `json:"comparisons"`
}

func attributeIntelligenceFingerprint(model string, valid map[string][]string) IntelligenceFingerprintAttribution {
	return attributeIntelligenceFingerprintWithBaselines(model, valid, intelligenceFingerprintBaselines)
}

// Statistical consistency is evidence about output behavior under this probe
// protocol, not authentication of the model/provider identity. Keep uncertain
// results explicit instead of treating a nearest-neighbor guess as a pass.
func attributeIntelligenceFingerprintWithBaselines(model string, valid map[string][]string, baselines []intelligenceFingerprintBaseline) IntelligenceFingerprintAttribution {
	r := IntelligenceFingerprintAttribution{Status: "insufficient", Message: "有效样本不足", Alpha: .05 / float64(max(1, 2*len(baselines))), SelfJSD: intelligenceFingerprintSplitHalf(valid), Warnings: []string{}, Comparisons: []IntelligenceFingerprintComparison{}}
	var sameCells map[string][]string
	for _, baseline := range baselines {
		cells, mean := compareIntelligenceFingerprintCells(valid, baseline.Cells)
		p := intelligenceFingerprintPermutation(valid, baseline.Cells, model+"|"+baseline.Model)
		r.Comparisons = append(r.Comparisons, IntelligenceFingerprintComparison{Model: baseline.Model, MeanJSD: mean, PValue: p, Verdict: intelligenceFingerprintDistanceVerdict(mean), SelfJSD: intelligenceFingerprintSplitHalf(baseline.Cells), Cells: cells})
		if baseline.Model == model {
			sameCells = baseline.Cells
		}
	}
	sort.SliceStable(r.Comparisons, func(i, j int) bool {
		a, b := r.Comparisons[i].MeanJSD, r.Comparisons[j].MeanJSD
		return a != nil && (b == nil || *a < *b)
	})
	if len(r.Comparisons) == 0 || r.Comparisons[0].MeanJSD == nil {
		return r
	}
	best := r.Comparisons[0]
	r.Nearest = best.Model
	if r.SelfJSD != nil && *r.SelfJSD > .25 {
		r.Warnings = append(r.Warnings, "本次样本自一致性较低，建议使用严格模式复测")
	}
	var same *IntelligenceFingerprintComparison
	for i := range r.Comparisons {
		c := &r.Comparisons[i]
		if c.Model == model {
			same = c
		}
		if c.SelfJSD != nil && *c.SelfJSD > .25 {
			r.Warnings = append(r.Warnings, c.Model+" 基准自一致性较低")
		}
	}
	significant := func(p *float64) bool { return p != nil && *p < r.Alpha }
	if same == nil {
		r.Status, r.Message = "no_baseline", "所选模型没有同名基准，仅展示相似模型"
		return r
	}
	if same.MeanJSD == nil {
		return r
	}
	if best.Model == model {
		if significant(same.PValue) {
			r.Status, r.Message = "different", "与同名基准存在显著差异，无法归因到其他模型"
		} else {
			r.Status, r.Message = "consistent", "与同名基准未发现显著差异"
		}
	} else {
		var bestCells map[string][]string
		for _, b := range baselines {
			if b.Model == best.Model {
				bestCells = b.Cells
			}
		}
		// Restrict reference separation to the probes actually collected in this run.
		a, b := map[string][]string{}, map[string][]string{}
		for cell, answers := range valid {
			if len(answers) >= intelligenceFingerprintMinValid {
				a[cell], b[cell] = sameCells[cell], bestCells[cell]
			}
		}
		r.ReferencePValue = intelligenceFingerprintPermutation(a, b, "reference|"+model+"|"+best.Model)
		switch {
		case !significant(r.ReferencePValue):
			r.Status, r.Message = "ambiguous", "同名基准与最近基准本身无法区分"
		case significant(same.PValue) && !significant(best.PValue):
			r.Status, r.Message = "substitution", "支持替换为 "+best.Model+" 的假设"
			for _, candidate := range r.Comparisons[1:] {
				if candidate.Model != model && candidate.PValue != nil && !significant(candidate.PValue) {
					r.Status, r.Message = "ambiguous", "与同名基准不同，但多个替代模型均无法排除"
					break
				}
			}
		case !significant(same.PValue) && significant(best.PValue):
			r.Status, r.Message = "consistent", "与同名基准未发现显著差异"
		case significant(same.PValue) && significant(best.PValue):
			r.Status, r.Message = "different", "与同名及最近基准均存在显著差异"
		default:
			r.Status, r.Message = "ambiguous", "多个模型均无法排除，样本不足以区分"
		}
	}
	if r.SelfJSD != nil && *r.SelfJSD > .25 && (r.Status == "consistent" || r.Status == "substitution") {
		r.Status, r.Message = "unstable", "样本波动较大，归因不稳定，请复测"
	}
	return r
}
