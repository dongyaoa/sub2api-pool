package service

import (
	"regexp"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/unicode/norm"
)

const intelligenceCandyMaxInput = 64 * 1024

var intelligenceCandyNumber = regexp.MustCompile(`[+-]?[0-9]+(?:\.[0-9]+)?(?:[eE][+-]?[0-9]+)?`)

// gradeIntelligenceCandyAnswer follows the reference candy evaluators: an
// independent 21 anywhere in the visible response passes. It does not attempt
// to judge conclusions or interpret the proof. In particular, Markdown, LaTeX,
// quotations and explanatory text must not hide an otherwise matching number.
// NFKC additionally accepts fullwidth and compatibility digits. Adjacent Unicode
// decimal digits are excluded just as Python's (?<!\d)21(?!\d) excludes them.
func gradeIntelligenceCandyAnswer(raw string) (answer string, correct bool) {
	if len(raw) == 0 || len(raw) > intelligenceCandyMaxInput {
		return "", false
	}
	text := norm.NFKC.String(raw)
	if len(text) > intelligenceCandyMaxInput {
		return "", false
	}
	for i := 0; i+1 < len(text); i++ {
		if text[i] == '2' && text[i+1] == '1' && !intelligenceCandyAdjacentDigits(text, i, i+2) {
			return "21", true
		}
	}
	// Keep a recognizable single numeric reply for the details dialog. This
	// fallback is for display only and can never turn a non-match into a pass.
	numbers := intelligenceCandyNumber.FindAllStringIndex(text, 2)
	if len(numbers) == 1 {
		start, end := numbers[0][0], numbers[0][1]
		if end-start <= 128 && !intelligenceCandyAdjacentDigits(text, start, end) {
			return text[start:end], false
		}
	}
	return "", false
}

func intelligenceCandyAdjacentDigits(text string, start, end int) bool {
	if start > 0 {
		previous, _ := utf8.DecodeLastRuneInString(text[:start])
		if unicode.IsDigit(previous) {
			return true
		}
	}
	if end < len(text) {
		next, _ := utf8.DecodeRuneInString(text[end:])
		return unicode.IsDigit(next)
	}
	return false
}
