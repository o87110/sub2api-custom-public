package moderation

import (
	"strings"
	"unicode/utf8"
)

const (
	// ActionCyberPolicyOutOfScope records an upstream cyber policy event without local enforcement.
	ActionCyberPolicyOutOfScope = "cyber_policy_out_of_scope"
	// KeywordContextSeparator separates the preserved input head from the matched-keyword context.
	KeywordContextSeparator = "\n…已省略中间内容…\n"
	// MaxKeywordExcerptRunes bounds custom keyword-hit excerpts without changing upstream log limits.
	MaxKeywordExcerptRunes = 1000
	// MaxErrorExcerptRunes bounds stored upstream error excerpts.
	MaxErrorExcerptRunes = 960
)

// BuildKeywordExcerptFromRedacted keeps the input head and a late keyword match.
// The caller must redact secrets before invoking this function.
func BuildKeywordExcerptFromRedacted(redacted, keyword string, maxRunes int) string {
	if maxRunes <= 0 {
		return ""
	}

	textRunes := []rune(redacted)
	if len(textRunes) <= maxRunes {
		return redacted
	}

	keywordStart, keywordEnd, found := findKeywordRuneRange(redacted, keyword)
	if !found || keywordEnd <= maxRunes {
		return string(textRunes[:maxRunes])
	}

	separator := []rune(KeywordContextSeparator)
	if maxRunes <= len(separator) {
		return string(textRunes[:maxRunes])
	}

	const preferredHeadRunes = 350
	headRunes := preferredHeadRunes
	if headRunes > maxRunes-len(separator) {
		headRunes = maxRunes - len(separator)
	}

	contextBudget := maxRunes - headRunes - len(separator)
	keywordRunes := keywordEnd - keywordStart
	if contextBudget <= 0 || keywordRunes > contextBudget {
		return string(textRunes[:maxRunes])
	}

	contextStart := keywordStart - (contextBudget-keywordRunes)/2
	if contextStart < headRunes {
		contextStart = headRunes
	}
	contextEnd := contextStart + contextBudget
	if contextEnd > len(textRunes) {
		contextEnd = len(textRunes)
		contextStart = contextEnd - contextBudget
		if contextStart < headRunes {
			contextStart = headRunes
		}
	}

	excerpt := make([]rune, 0, maxRunes)
	excerpt = append(excerpt, textRunes[:headRunes]...)
	excerpt = append(excerpt, separator...)
	excerpt = append(excerpt, textRunes[contextStart:contextEnd]...)
	return string(excerpt)
}

func findKeywordRuneRange(text, keyword string) (int, int, bool) {
	lowerKeyword := strings.ToLower(keyword)
	if lowerKeyword == "" {
		return 0, 0, false
	}
	lowerText := strings.ToLower(text)
	byteStart := strings.Index(lowerText, lowerKeyword)
	if byteStart < 0 {
		return 0, 0, false
	}
	runeStart := utf8.RuneCountInString(lowerText[:byteStart])
	runeEnd := runeStart + utf8.RuneCountInString(lowerKeyword)
	return runeStart, runeEnd, true
}
