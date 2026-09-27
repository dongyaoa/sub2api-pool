package service

import (
	"regexp"
	"strings"
	"unicode"
)

const intelligenceCandyMaxInput = 64 * 1024
const intelligenceCandyMaxStatement = 2048

// Go's RE2 expressions are linear-time. Input, statement and numeric-token
// limits also bound normalization, candidate collection and displayed answers.
var (
	intelligenceCandyNumber        = regexp.MustCompile(`[+-]?[0-9]+(?:\.[0-9]+)?(?:e[+-]?[0-9]+)?`)
	intelligenceCandyCorrectNumber = regexp.MustCompile(`^\+?0*21(?:\.0+)?$`)
	intelligenceCandyLatexText     = regexp.MustCompile(`\\(?:text|textrm|textnormal|textbf|textit|mathrm|mathbf|mathit|mathsf|mathnormal|mbox)\s*\{([^{}\n]{0,256})\}`)
	intelligenceCandyBox           = regexp.MustCompile(`\\boxed\s*\{([^{}\n]{1,128})\}`)
	intelligenceCandyFinalCue      = regexp.MustCompile(`^(?:最终(?:的)?(?:答案|结果|结论)|最后(?:的)?(?:答案|结果|结论)|(?:the\s+)?final\s+(?:answer|result)\b|最终|最后)\s*(?:(?:为|是|is)\s*)?[:=]?\s*`)
	intelligenceCandyAnswerCue     = regexp.MustCompile(`^(?:(?:正确)?答案|答|结论|结果|(?:the\s+)?(?:correct\s+)?answer\b)\s*(?:(?:为|是|is)\s*)?[:=]?\s*`)
	intelligenceCandyInferenceCue  = regexp.MustCompile(`^(?:综上(?:所述)?|因此|所以|故|由此可得|由此可知|总之|therefore\b|thus\b|hence\b|in\s+conclusion\b|so\b)\s*[,=:]?\s*`)
	intelligenceCandyChineseCount  = regexp.MustCompile(`^(?:(?:(?:最少|至少)(?:需要|需|要|应当|应|必须)?|(?:需要|需|必须|应当|应)(?:最少|至少)?)(?:取出|抽出|摸出|拿出|取|抽|摸)?(?:的)?(?:糖果)?(?:数目|数量|个数)?(?:是|为)?|(?:最少|最小)(?:的)?(?:取出|摸出|抽出)?(?:糖果)?(?:数目|数量|个数)(?:是|为)?)$`)
	intelligenceCandyEnglishCount  = regexp.MustCompile(`^(?:(?:the\s+)?(?:minimum|smallest)(?:\s+number(?:\s+of\s+candies)?)?(?:\s+(?:required|needed))?\s*(?:is|=|:)?|(?:(?:you|we)\s+)?(?:need|must|should)(?:\s+to)?\s*(?:draw|take|choose)?\s*(?:at\s+least)?|at\s+least|a\s+minimum\s+of|no\s+fewer\s+than)$`)
	intelligenceCandySuffix        = regexp.MustCompile(`^(?:(?:个|颗|粒|枚|块)?\s*(?:糖果|糖)|(?:个|颗|粒|枚|块)|(?:pieces?\s+of\s+)?cand(?:y|ies))?(?:\s*(?:即可|才行|(?:(?:即可|才能|才可|可以|能够|就能|足以)\s*)?(?:保证|确保)(?:\s*(?:满足|符合)(?:条件|要求|题意|题目要求|上述条件|全部条件|题目条件))?|are\s+(?:required|needed|necessary|sufficient)|is\s+(?:required|needed|necessary|sufficient)))?$`)
	intelligenceCandyHedge         = regexp.MustCompile(`(?:[?？]|可能|大约|约为|似乎|也许|应该是|不确定|未知|无法|尚未|未给出|不是|并非|不应|不对|不正确|错误|或者|或|如果|假设|题干|题目说|例如|举例|\b(?:or|maybe|perhaps|probably|approximately|about|not|unknown|uncertain|assuming|if|example|question|prompt)\b)`)
	intelligenceCandyAlternative   = regexp.MustCompile(`^(?:(?:但(?:是)?|不过)\s*)?(?:或(?:者|许)?|还是|也可能|也可以是|另一(?:个|种)答案|可能是|也许|(?:我)?不确定|无法确定|不能确定|不对|不正确|有误|这(?:个答案)?(?:是|并)?(?:不对|不正确|错误|错的)|更正)|^(?:(?:but|however)[, ]+)?(?:or\b|maybe\b|perhaps\b|(?:i am |i'm )?not sure\b|i\s+(?:think|guess|am unsure)\b|uncertain\b|actually\b|correction\b|alternatively\b|another\s+answer\b)`)
)

type intelligenceCandyConclusion struct {
	answer   string
	priority int
}

// gradeIntelligenceCandyAnswer grades only a recognizable visible conclusion;
// numbers in the proof are not votes. Explicit final answers supersede earlier
// weaker conclusions, but conflicting final claims or alternatives fail closed.
// The original response remains available to users when identification fails.
func gradeIntelligenceCandyAnswer(raw string) (answer string, correct bool) {
	if len(raw) == 0 || len(raw) > intelligenceCandyMaxInput {
		return "", false
	}
	statements := intelligenceCandyStatements(raw)
	best := intelligenceCandyConclusion{}
	ambiguous := false
	for i := 0; i < len(statements); i++ {
		first := i == 0
		statement := statements[i]
		if len(statement) > intelligenceCandyMaxStatement {
			return "", false
		}
		// Markdown headings and labels commonly put the value on the next line.
		if rest, priority := intelligenceCandyStripCues(intelligenceCandyFormatting(statement)); priority > 0 && rest == "" && i+1 < len(statements) {
			statement += " " + statements[i+1]
			i++
		}
		if len(statement) > intelligenceCandyMaxStatement {
			return "", false
		}
		// Models often lead with "21 candies" and explain afterward. Bare
		// numbers at either edge are weak conclusions; isolated proof numbers
		// in the middle are not. Explicit final answers still supersede them.
		candidate := intelligenceCandyParseConclusion(statement, first || i == len(statements)-1)
		if candidate.priority == 0 {
			if best.priority > 0 && intelligenceCandyAlternative.MatchString(intelligenceCandyFormatting(statement)) {
				ambiguous = true
			}
			continue
		}
		if candidate.priority > best.priority {
			best = candidate
			ambiguous = candidate.answer == ""
			continue
		}
		if candidate.answer == "" || !intelligenceCandySameNumber(candidate.answer, best.answer) {
			ambiguous = true
		}
	}
	if ambiguous || best.answer == "" {
		return "", false
	}
	return best.answer, intelligenceCandyCorrectNumber.MatchString(best.answer)
}

func intelligenceCandySameNumber(a, b string) bool {
	return a == b || intelligenceCandyCorrectNumber.MatchString(a) && intelligenceCandyCorrectNumber.MatchString(b)
}

func intelligenceCandyParseConclusion(statement string, allowStandalone bool) intelligenceCandyConclusion {
	if len(statement) > intelligenceCandyMaxStatement {
		return intelligenceCandyConclusion{}
	}
	boxed := intelligenceCandyBox.MatchString(intelligenceCandyLatexFormatting(statement))
	text := intelligenceCandyFormatting(statement)
	body, priority := intelligenceCandyStripCues(text)
	invalid := func() intelligenceCandyConclusion { return intelligenceCandyConclusion{priority: priority} }
	numbers := intelligenceCandyNumber.FindAllStringIndex(body, 2)
	if len(numbers) == 0 {
		if priority <= 1 {
			return intelligenceCandyConclusion{}
		}
		return invalid()
	}
	prefix := strings.TrimSpace(body[:numbers[0][0]])
	countPhrase := intelligenceCandyCountPrefix(prefix)
	if countPhrase && priority < 2 {
		priority = 2
	}
	if prefix != "" && !countPhrase {
		if priority <= 1 {
			return intelligenceCandyConclusion{}
		}
		return invalid()
	}
	// A box inside a proof, such as 9+12=\boxed{21}, is not a final
	// conclusion. In particular, do not turn it into a high-priority invalid
	// answer that prevents a later, clearly worded conclusion from being read.
	if priority == 0 && len(numbers) > 1 && strings.Contains(body, "=") {
		return intelligenceCandyConclusion{}
	}
	if boxed && priority < 3 {
		priority = 3
	}
	if priority == 0 {
		if !allowStandalone {
			return intelligenceCandyConclusion{}
		}
		priority = 1
	}
	if len(numbers) != 1 || intelligenceCandyHedge.MatchString(text) {
		return invalid()
	}
	number := body[numbers[0][0]:numbers[0][1]]
	if len(number) > 128 || !intelligenceCandySuffix.MatchString(strings.TrimSpace(body[numbers[0][1]:])) {
		return invalid()
	}
	return intelligenceCandyConclusion{answer: number, priority: priority}
}

func intelligenceCandyStripCues(text string) (string, int) {
	priority := 0
	// A contrast immediately before an explicit answer is a conclusion, whereas
	// a free-standing "but perhaps ..." must remain an ambiguous alternative.
	for _, prefix := range []string{"但是", "但", "but ", "however "} {
		if strings.HasPrefix(text, prefix) {
			rest := strings.TrimSpace(strings.TrimPrefix(text, prefix))
			number := intelligenceCandyNumber.FindStringIndex(rest)
			countPhrase := number != nil && intelligenceCandyCountPrefix(strings.TrimSpace(rest[:number[0]]))
			if intelligenceCandyFinalCue.MatchString(rest) || intelligenceCandyAnswerCue.MatchString(rest) || countPhrase {
				text = rest
			}
			break
		}
	}
	for range 3 {
		var cue *regexp.Regexp
		rank := 0
		switch {
		case intelligenceCandyFinalCue.MatchString(text):
			cue, rank = intelligenceCandyFinalCue, 3
		case intelligenceCandyAnswerCue.MatchString(text):
			cue, rank = intelligenceCandyAnswerCue, 2
		case intelligenceCandyInferenceCue.MatchString(text):
			cue, rank = intelligenceCandyInferenceCue, 1
		default:
			return text, priority
		}
		if rank > priority {
			priority = rank
		}
		text = strings.TrimSpace(cue.ReplaceAllString(text, ""))
	}
	return text, priority
}

func intelligenceCandyCountPrefix(prefix string) bool {
	return intelligenceCandyChineseCount.MatchString(strings.ReplaceAll(prefix, " ", "")) || intelligenceCandyEnglishCount.MatchString(prefix)
}

func intelligenceCandyFormatting(text string) string {
	text = strings.TrimSpace(text)
	text = strings.TrimLeft(text, "#")
	text = strings.TrimSpace(text)
	text = strings.TrimPrefix(text, "- ")
	text = intelligenceCandyLatexFormatting(text)
	text = intelligenceCandyBox.ReplaceAllString(text, " $1 ")
	// Replace markup with spaces, never concatenate separated digits (2**1).
	text = strings.NewReplacer("*", " ", "_", " ", "`", " ", "$", " ", `\(`, " ", `\)`, " ", `\[`, " ", `\]`, " ").Replace(text)
	text = strings.Join(strings.Fields(text), " ")
	return strings.Trim(text, " \t.,，:：;；!！。")
}

// Remove only text/style wrappers, starting with the innermost ones. This
// accepts common model output such as \boxed{21\text{个}} without interpreting
// arbitrary LaTeX as a numeric answer. Spaces preserve numeric token boundaries
// so 2\text{}1 cannot silently become 21. The depth and input size are bounded.
func intelligenceCandyLatexFormatting(text string) string {
	for range 8 {
		next := intelligenceCandyLatexText.ReplaceAllString(text, " $1 ")
		if next == text {
			break
		}
		text = next
	}
	return text
}

func intelligenceCandyStatements(raw string) []string {
	text := strings.Map(func(r rune) rune {
		if r >= '\uff01' && r <= '\uff5e' {
			return r - 0xfee0
		}
		if r == '\u3000' || r == '\u00a0' {
			return ' '
		}
		return unicode.ToLower(r)
	}, raw)
	text = strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n")
	var statements []string
	fenced := false
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			fenced = !fenced
			continue
		}
		if fenced || strings.HasPrefix(trimmed, ">") || strings.ContainsAny(trimmed, `"“”「」『』`) {
			continue
		}
		runes := []rune(line)
		start := 0
		appendStatement := func(end int) {
			statement := strings.TrimSpace(string(runes[start:end]))
			if statement != "" {
				statements = append(statements, statement)
			}
			start = end
		}
		for i, r := range runes {
			separator := strings.ContainsRune("。!?;，,", r)
			if r == '.' {
				separator = i+1 == len(runes) || unicode.IsSpace(runes[i+1])
			}
			// Neither decimals nor comma-delimited numbers may become a trailing
			// standalone 21 (for example 1,021 or the ambiguous list 21,22).
			if (r == ',' || r == '.' || r == '，') && i > 0 && i+1 < len(runes) && unicode.IsDigit(runes[i-1]) && unicode.IsDigit(runes[i+1]) {
				separator = false
			}
			if separator {
				appendStatement(i + 1)
			}
		}
		appendStatement(len(runes))
		if len(statements) > 2048 {
			return nil
		}
	}
	return statements
}
