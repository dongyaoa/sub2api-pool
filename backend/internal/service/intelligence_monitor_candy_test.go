package service

import (
	"strings"
	"testing"
)

func TestGradeIntelligenceCandyAnswer(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		answer  string
		correct bool
	}{
		{"bare integer", "21", "21", true},
		{"whitespace", " \n 21 \t", "21", true},
		{"bold", "**21**", "21", true},
		{"inline code", "`21`", "21", true},
		{"opening answer", "**21 个**。\n因为可以先考虑最不利情况……", "21", true},
		{"Chinese conclusion", "因此，最少需要取出 **21** 个糖果。", "21", true},
		{"English conclusion", "The final answer is **21** candies.", "21", true},
		{"heading and next line", "### 最终答案\n\n**21** 个糖果。", "21", true},
		{"boxed", `\boxed{21}`, "21", true},
		{"boxed nested unit", `所以答案是 **\(\boxed{21\text{个}}\)**。`, "21", true},
		{"boxed nested numeric formatting", `最终答案为 \boxed{\mathbf{21}\text{ 个糖果}}。`, "21", true},
		{"screenshot proof then conclusion and caveat", "9+12=\\boxed{21}.\n\\]\n\n### 二、为什么20个不能保证？\n- 至少需要摸到第 \\(8+1=9\\) 个圆形，以及第 \\(4+7+1=12\\) 个五角星形，共 **21个**。\n因此，20个都可能无法满足要求，故最少为 **21个**。\n*注：若不允许选择形状，只能完全随机摸取，则需要29个。*", "21", true},
		{"fullwidth digits", "最终答案：２１个。", "21", true},
		{"mathematical bold digits", "答案：𝟐𝟏", "21", true},
		{"circled compatibility number", "答案：㉑", "21", true},
		{"superscript compatibility digits", "答案：²¹", "21", true},
		// These deliberately mirror the references' digit-boundary policy.
		// The evaluator is a benchmark signal, not a semantic conclusion grader.
		{"proof alone matches", `9+12=\boxed{21}，16+5=\boxed{21}。`, "21", true},
		{"proof with later different conclusion matches", "过程中出现21。最终答案：22个。", "21", true},
		{"alternative matches", "最终答案：21或29个。", "21", true},
		{"quoted answer matches", "“最终答案：21个。”", "21", true},
		{"block quote matches", "> 最终答案：21个。", "21", true},
		{"code block matches", "```text\n最终答案：21个。\n```", "21", true},
		{"question matches", "最终答案是21个吗？", "21", true},
		{"decimal boundary matches reference", "21.5", "21", true},
		{"fraction boundary matches reference", "21/22", "21", true},
		{"positive boundary matches reference", "+21", "21", true},
		{"negative boundary matches reference", "-21", "21", true},
		{"letters are not digits", "x21abc", "21", true},
		{"multiple candidates", "121、210、21", "21", true},
		{"empty", "", "", false},
		{"no answer", "我无法确定答案。", "", false},
		{"question without answer", "最少需要取出多少个糖果？", "", false},
		{"problem table without answer", "口味 苹果 桃子 西瓜\n圆形 7 9 8\n五角星 7 6 4", "", false},
		{"wrong bare integer", "29", "29", false},
		{"wrong boxed integer", `最终答案是 \boxed{29\text{个}}。`, "29", false},
		{"wrong fullwidth integer", "答案：２９个", "29", false},
		{"wrong 121", "121", "121", false},
		{"wrong 210", "答案：210个", "210", false},
		{"leading zeros are adjacent digits", "021", "021", false},
		{"ASCII adjacent both sides", "1210", "1210", false},
		{"fullwidth adjacent prefix", "１21", "121", false},
		{"fullwidth adjacent suffix", "21０", "210", false},
		{"Arabic Indic adjacent prefix", "١21", "", false},
		{"Arabic Indic adjacent suffix", "21٠", "", false},
		{"Devanagari adjacent prefix", "१21", "", false},
		{"Devanagari adjacent suffix", "21०", "", false},
		{"astral Unicode decimal adjacent prefix", "𐒡21", "", false},
		{"astral Unicode decimal adjacent suffix", "21𐒠", "", false},
		{"markup cannot join digits", "2**1", "", false},
		{"latex cannot join digits", `\boxed{2\text{}1}`, "", false},
		{"spaces cannot join digits", "2 1", "", false},
		{"bare expression not evaluated", "20 + 1", "", false},
		{"multiple wrong numbers remain unidentified", "20 或 29", "", false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			answer, correct := gradeIntelligenceCandyAnswer(test.raw)
			if answer != test.answer || correct != test.correct {
				t.Fatalf("grade(%q) = (%q, %v), want (%q, %v)", test.raw, answer, correct, test.answer, test.correct)
			}
		})
	}
}

func TestGradeIntelligenceCandyAnswerInputBounds(t *testing.T) {
	for _, raw := range []string{
		strings.Repeat(" ", intelligenceCandyMaxInput) + "21",
		"Final answer: " + strings.Repeat("2", 129),
		// NFKC expands this Arabic compatibility ligature; bound that result too.
		strings.Repeat("ﷺ", 4096) + "21",
	} {
		if answer, correct := gradeIntelligenceCandyAnswer(raw); answer != "" || correct {
			t.Fatalf("oversized input yielded (%q, %v)", answer, correct)
		}
	}
	// Long paragraphs were rejected by the old 2 KiB statement heuristic.
	for _, raw := range []string{
		strings.Repeat("推导", 1000) + "21",
		strings.Repeat(" ", intelligenceCandyMaxInput-2) + "21",
	} {
		if answer, correct := gradeIntelligenceCandyAnswer(raw); answer != "21" || !correct {
			t.Fatalf("bounded long response yielded (%q, %v)", answer, correct)
		}
	}
}

func FuzzGradeIntelligenceCandyAnswer(f *testing.F) {
	for _, raw := range []string{"21", "最终答案：21或22。", `\boxed{21}`, "２１．５", "١21", "\xff\x00", strings.Repeat("21，", 256)} {
		f.Add(raw)
	}
	f.Fuzz(func(t *testing.T, raw string) {
		answer, correct := gradeIntelligenceCandyAnswer(raw)
		again, againCorrect := gradeIntelligenceCandyAnswer(raw)
		if answer != again || correct != againCorrect || len(answer) > 128 {
			t.Fatalf("non-deterministic or unbounded result")
		}
		if correct && answer != "21" {
			t.Fatalf("accepted a different number: %q", answer)
		}
	})
}
