package text

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// EstimateTokens guesses how many gateway tokens a text costs without a
// model-specific tokenizer. English prose runs near four characters per
// token, while terse or punctuation-heavy text runs nearer 1.3 tokens per
// word; the larger of the two estimates keeps scene splitting on the safe
// side. Non-ASCII text (accents, other scripts) is counted per rune, which
// also errs high.
func EstimateTokens(s string) int {
	if s == "" {
		return 0
	}
	runes := utf8.RuneCountInString(s)
	byChars := (runes + 3) / 4
	words := 0
	inWord := false
	punct := 0
	for _, r := range s {
		switch {
		case unicode.IsSpace(r):
			inWord = false
		case unicode.IsPunct(r) || unicode.IsSymbol(r):
			punct++
			inWord = false
		default:
			if !inWord {
				words++
				inWord = true
			}
		}
	}
	byWords := (words*13+9)/10 + punct/2
	if byWords > byChars {
		return byWords
	}
	return byChars
}

// Words counts whitespace-separated words.
func Words(s string) int {
	return len(strings.Fields(s))
}
