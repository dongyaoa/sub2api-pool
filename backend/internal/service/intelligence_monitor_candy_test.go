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
		{"opening bold answer then explanation", "**21 个**。\n因为可以先考虑最不利情况……", "21", true},
		{"opening bare answer then explanation", "21\n理由如下：……", "21", true},
		{"opening English answer then explanation", "21 candies.\nConsider the worst case first.", "21", true},
		{"opening answer after quoted problem", "> 圆形苹果7桃子9西瓜8\n> 五角星苹果7桃子6西瓜4\n\n21个糖果。\n理由如下：……", "21", true},
		{"opening and final bare answer agree", "21\n理由如下：先考虑最不利情况。\n21", "21", true},
		{"opening wrong answer corrected explicitly", "22\n重新计算最不利情况。\n最终答案是21个糖果。", "21", true},
		{"fullwidth digits", "最终答案：２１个。", "21", true},
		{"fullwidth decimal", "答案：２１．５个。", "21.5", false},
		{"boxed", `\boxed{21}`, "21", true},
		{"display math", `\[\boxed{21}\]`, "21", true},
		{"dollar math", `$\boxed{21}$`, "21", true},
		{"boxed final", `最终答案是 $\boxed{21}$ 个糖果。`, "21", true},
		{"Chinese conclusion", "最终答案：21个糖果。", "21", true},
		{"Chinese minimum", "最少需要取出21个糖果。", "21", true},
		{"Chinese minimum with reasoning cue", "因此，最少需要取出 **21** 个糖果。", "21", true},
		{"Chinese guarantee", "因此，最少取出 21 个糖果即可保证满足条件。", "21", true},
		{"Chinese guarantee question requirements", "至少需要摸出21颗糖果才能保证符合题意。", "21", true},
		{"Chinese guarantee all conditions", "最少取出21个糖果可以确保满足全部条件。", "21", true},
		{"bold number and unit", "答案为 **21 个**。", "21", true},
		{"Chinese abbreviated minimum", "至少取21颗糖。", "21", true},
		{"Chinese minimum count", "最少需要取出的糖果数目为21。", "21", true},
		{"Chinese answer units", "21个糖果", "21", true},
		{"Chinese explanation afterward", "最少需要取出21个糖果，因为20个还不能保证。", "21", true},
		{"English final", "The final answer is **21** candies.", "21", true},
		{"English answer", "The answer is 21.", "21", true},
		{"English correct answer", "The correct answer is 21.", "21", true},
		{"Chinese correct answer", "正确答案是21个糖果。", "21", true},
		{"English minimum", "Therefore, at least 21 candies are required.", "21", true},
		{"English draw", "You need to draw at least 21 candies.", "21", true},
		{"English minimum count", "The minimum number of candies is 21.", "21", true},
		{"English units", "21 candies", "21", true},
		{"zero decimal", "Final answer: 21.0", "21.0", true},
		{"positive integer", "最终答案：+21个", "+21", true},
		{"leading zeros", "最终答案：021个", "021", true},
		{"heading and next line", "### 最终答案\n\n**21** 个糖果。", "21", true},
		{"proof numbers then final", "先算出7、9、8以及7、6、4。\n所以答案为21个糖果。", "21", true},
		{"intermediate answer overridden", "答案是22。\n最终答案是21。", "21", true},
		{"same conclusion repeated", "最终答案：21。\n答案为21个。", "21", true},
		{"proof after conclusion", "Final answer: 21. Therefore the partial sum is 20.", "21", true},
		{"non numeric proof after conclusion", "Final answer: 21. Therefore the requirement is satisfied.", "21", true},
		{"equivalent conclusion repeated", "最终答案：21.0。\n21", "21.0", true},
		{"empty", "", "", false},
		{"no answer", "我无法确定答案。", "", false},
		{"unanswered question", "最少需要取出多少个糖果？", "", false},
		{"quoted problem", "题干：圆形苹果7桃子9西瓜8，五角星苹果7桃子6西瓜4。最少取出多少个？", "", false},
		{"quoted answer", "“最终答案：21个。”", "", false},
		{"block quote", "> 最终答案：21个。", "", false},
		{"code block", "```text\n最终答案：21个。\n```", "", false},
		{"question", "最终答案是21个吗？", "", false},
		{"guarantee question", "最少取出21个糖果即可保证满足条件？", "", false},
		{"negated guarantee", "最少取出21个糖果即可保证不满足条件。", "", false},
		{"arbitrary guarantee tail", "最少取出21个糖果即可保证赢得比赛。", "", false},
		{"guarantee with extra number", "最少取出21个糖果即可保证满足22个条件。", "", false},
		{"English question", "Final answer: 21?", "", false},
		{"uncertain", "最终答案可能是21个。", "", false},
		{"negated", "最终答案不是21个。", "", false},
		{"English negated", "The final answer is not 21.", "", false},
		{"conditional", "如果答案是21，则条件成立。", "", false},
		{"process mention", "过程里曾经计算出21个。", "", false},
		{"process sum", "Therefore the partial sum is 21.", "", false},
		{"isolated intermediate number", "先考虑一个中间值。\n21\n还需要继续推导。", "", false},
		{"problem table without answer", "口味 苹果 桃子 西瓜\n圆形 7 9 8\n五角星 7 6 4", "", false},
		{"bare problem table rows", "7 9 8\n7 6 4", "", false},
		{"table rows surrounding an intermediate number", "7 9 8\n21\n7 6 4", "", false},
		{"markdown table with an intermediate number", "|形状|苹果|桃子|西瓜|\n|圆形|7|9|8|\n|五角星|7|6|4|\n21\n尚未得出答案。", "", false},
		{"wrong 121", "121", "121", false},
		{"wrong 210", "答案：210个", "210", false},
		{"wrong decimal", "最终答案：21.5个", "21.5", false},
		{"negative", "最终答案：-21个", "-21", false},
		{"percent", "最终答案：21%", "", false},
		{"thousands separator", "1,021", "", false},
		{"embedded word", "Final answer: x21", "", false},
		{"embedded suffix", "Final answer: 21abc", "", false},
		{"numeric list", "21,22", "", false},
		{"fraction", "最终答案：21/22", "", false},
		{"bare expression", "20 + 1", "", false},
		{"markdown cannot join digits", "2**1", "", false},
		{"process 21 final 22", "过程中出现21。最终答案：22个。", "22", false},
		{"opening answer overridden by final 22", "21\n重新计算最不利情况。\n最终答案是22个糖果。", "22", false},
		{"opening and final bare answers conflict", "21\n理由如下：先考虑最不利情况。\n22", "", false},
		{"same line explicit final", "过程中计算出21，但最终答案是22个。", "22", false},
		{"final required count", "答案是21，但最终需要取出22个糖果。", "22", false},
		{"multiple final answers", "最终答案：21。\n最终答案：22。", "", false},
		{"later conflicting answer", "最终答案：21。\n答案：22。", "", false},
		{"later conflicting guarantee", "最终答案：21。至少需要取出22个糖果才能保证符合题意。", "", false},
		{"later conflicting contrast", "最终答案：21，但至少需要取出22个糖果才能保证符合题意。", "", false},
		{"English later conflicting contrast", "Final answer: 21. But at least 22 candies are required.", "", false},
		{"ambiguous either", "最终答案：21或22个。", "", false},
		{"ambiguous English", "Final answer: either 21 or 22.", "", false},
		{"alternative new clause", "最终答案：21，或者22。", "", false},
		{"alternative question", "最终答案：21，还是22？", "", false},
		{"another answer", "最终答案：21，另一种答案是22。", "", false},
		{"alternative new line", "最终答案：21。\n也可能是22。", "", false},
		{"uncertain afterward", "最终答案：21。\n但是我不确定。", "", false},
		{"disavowed afterward", "最终答案：21。\n但不正确。", "", false},
		{"invalid final supersedes process", "答案是21。最终答案无法确定。", "", false},
		{"ambiguous final supersedes process", "答案是21。最终答案：21或者22。", "", false},
		{"unfinished final supersedes process", "答案是21。最终答案：", "", false},
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
		strings.Repeat(" ", 64*1024) + "21",
		"Final answer: " + strings.Repeat("2", 129),
		"Final answer: " + strings.Repeat(" ", 2048) + "21",
		"答案21。Final answer: " + strings.Repeat(" ", 2048) + "22",
		"答案21。\n最终答案：\n" + strings.Repeat("a", 2034) + "22",
	} {
		if answer, correct := gradeIntelligenceCandyAnswer(raw); answer != "" || correct {
			t.Fatalf("oversized input yielded (%q, %v)", answer, correct)
		}
	}
}

func FuzzGradeIntelligenceCandyAnswer(f *testing.F) {
	for _, raw := range []string{"21", "最终答案：21或22。", `\boxed{21}`, "２１．５", "\xff\x00", strings.Repeat("21，", 256)} {
		f.Add(raw)
	}
	f.Fuzz(func(t *testing.T, raw string) {
		answer, correct := gradeIntelligenceCandyAnswer(raw)
		again, againCorrect := gradeIntelligenceCandyAnswer(raw)
		if answer != again || correct != againCorrect || len(answer) > 128 {
			t.Fatalf("non-deterministic or unbounded result")
		}
		if correct && !intelligenceCandyCorrectNumber.MatchString(answer) {
			t.Fatalf("accepted a different number: %q", answer)
		}
	})
}
