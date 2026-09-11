package aieval

import (
	"errors"
	"strings"
	"unicode"
)

// FactCheck is a code-owned, bounded assertion rule. Aliases are alternatives
// for the same assertion; opposed aliases express a contradiction. This is not
// a general semantic judge, and automatic success always requires human review.
type FactCheck struct {
	ID          string   `json:"id"`
	Aliases     []string `json:"aliases"`
	Opposed     []string `json:"opposed,omitempty"`
	Affirmative bool     `json:"affirmative,omitempty"`
}

type Assessment struct {
	Method              string `json:"method"`
	KeywordChecks       int    `json:"keyword_checks"`
	FactChecks          int    `json:"fact_checks"`
	HumanReviewRequired bool   `json:"human_review_required"`
}

func validateFactChecks(checks []FactCheck) error {
	if len(checks) > 8 {
		return errors.New("too many fact checks")
	}
	seen := map[string]bool{}
	for _, check := range checks {
		if strings.TrimSpace(check.ID) == "" || seen[check.ID] || len(check.Aliases) == 0 || len(check.Aliases) > 16 || len(check.Opposed) > 16 {
			return errors.New("invalid fact check")
		}
		seen[check.ID] = true
		for _, phrase := range append(append([]string{}, check.Aliases...), check.Opposed...) {
			if strings.TrimSpace(phrase) == "" || len(phrase) > 200 {
				return errors.New("invalid fact alias")
			}
		}
	}
	return nil
}

func evaluateFactChecks(checks []FactCheck, answer string) []Failure {
	failures := []Failure{}
	for _, check := range checks {
		matched, contradicted := false, false
		for _, alias := range check.Aliases {
			positive, negative := assertionOccurrences(answer, alias)
			matched = matched || positive || (!check.Affirmative && negative)
			contradicted = contradicted || (check.Affirmative && negative)
		}
		for _, opposed := range check.Opposed {
			positive, _ := assertionOccurrences(answer, opposed)
			contradicted = contradicted || positive
		}
		if contradicted {
			failures = append(failures, Failure{Code: "FACT_CONTRADICTED", Detail: check.ID})
		} else if !matched {
			failures = append(failures, Failure{Code: "FACT_MISSING", Detail: check.ID})
		}
	}
	return failures
}

// Negation is restricted to the containing clause and a short prefix. English
// aliases require word boundaries; Chinese aliases do not have whitespace word
// boundaries. Unknown paraphrases are deliberately not treated as proven facts.
func assertionOccurrences(answer, alias string) (positive, negative bool) {
	text, term := strings.ToLower(answer), strings.ToLower(alias)
	for start := 0; start < len(text); {
		index := strings.Index(text[start:], term)
		if index < 0 {
			break
		}
		index += start
		end := index + len(term)
		if isASCIIWord(term) && ((index > 0 && isASCIIWordByte(text[index-1])) || (end < len(text) && isASCIIWordByte(text[end]))) {
			start = end
			continue
		}
		prefix := text[:index]
		if boundary := strings.LastIndexFunc(prefix, func(r rune) bool { return strings.ContainsRune(".。！？!?;；,，\n", r) }); boundary >= 0 {
			prefix = prefix[boundary+len(string([]rune(prefix[boundary:])[0])):]
		}
		runes := []rune(prefix)
		if len(runes) > 32 {
			prefix = string(runes[len(runes)-32:])
		}
		negated := false
		for _, marker := range []string{"不需要", "无需", "不要", "不用", "不必", "不得", "不能", "禁止", "不应", "不该", "没有必要", "不存在", "没有", "并非", "don't ", "do not ", "does not ", "should not ", "must not ", "not required to ", "no need to ", "never ", "not ", "no "} {
			if offset := strings.LastIndex(prefix, marker); offset >= 0 && len([]rune(prefix[offset+len(marker):])) <= 12 {
				negated = true
				break
			}
		}
		trimmed := strings.TrimSpace(prefix)
		if strings.HasSuffix(trimmed, "不") || strings.HasSuffix(trimmed, "未") || strings.HasSuffix(trimmed, "not") {
			negated = true
		}
		if negated {
			negative = true
		} else {
			positive = true
		}
		start = end
	}
	return
}

func isASCIIWord(value string) bool {
	for _, r := range value {
		if r > unicode.MaxASCII || (!unicode.IsLetter(r) && !unicode.IsSpace(r)) {
			return false
		}
	}
	return true
}
func isASCIIWordByte(value byte) bool {
	return value >= 'a' && value <= 'z' || value >= '0' && value <= '9' || value == '_'
}
