package text

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// Match locates a quote inside a chapter. Exact is false when the quote was
// found only after normalising quotation marks, dashes, whitespace and
// Markdown emphasis on both sides.
type Match struct {
	Start int
	End   int
	Exact bool
}

// FindQuote looks for quote in text: first verbatim, then normalised. The
// offsets always address the original text, so a highlight lands on the
// author's own words even when the writer straightened a curly quote or
// dropped an italic marker. Quotes shorter than three characters never match.
func FindQuote(text, quote string) (Match, bool) {
	q := strings.TrimSpace(quote)
	if len(q) < 3 {
		return Match{}, false
	}
	if i := strings.Index(text, q); i >= 0 {
		return Match{Start: i, End: i + len(q), Exact: true}, true
	}
	nt, offsets := normalise(text)
	nq, _ := normalise(q)
	if nq == "" {
		return Match{}, false
	}
	i := strings.Index(nt, nq)
	if i < 0 {
		li, lq := strings.ToLower(nt), strings.ToLower(nq)
		if len(li) != len(nt) || len(lq) != len(nq) {
			return Match{}, false
		}
		i = strings.Index(li, lq)
		if i < 0 {
			return Match{}, false
		}
	}
	last := offsets[i+len(nq)-1]
	_, size := utf8.DecodeRuneInString(text[last:])
	return Match{Start: offsets[i], End: last + size, Exact: false}, true
}

// Normalise returns the comparison form of a text: straight quotes, plain
// hyphens, single spaces, no Markdown emphasis or heading markers.
func Normalise(s string) string {
	n, _ := normalise(s)
	return n
}

// normalise builds the comparison form and, for each byte of it, the offset
// of the original byte it came from. Every emitted character is ASCII, so
// byte and character positions coincide in the result.
func normalise(s string) (string, []int) {
	var b strings.Builder
	offsets := make([]int, 0, len(s))
	emit := func(c byte, at int) {
		b.WriteByte(c)
		offsets = append(offsets, at)
	}
	lastSpace := true
	lineStart := true
	i := 0
	for i < len(s) {
		r, size := rune(s[i]), 1
		if r >= 0x80 {
			r, size = utf8.DecodeRuneInString(s[i:])
		}
		at := i
		i += size
		switch {
		case r == '‘' || r == '’' || r == '‚' || r == '′':
			emit('\'', at)
			lastSpace, lineStart = false, false
		case r == '“' || r == '”' || r == '„' || r == '″' || r == '«' || r == '»':
			emit('"', at)
			lastSpace, lineStart = false, false
		case r == '–' || r == '—' || r == '‒' || r == '―' || r == '−':
			emit('-', at)
			lastSpace, lineStart = false, false
		case r == '…':
			emit('.', at)
			emit('.', at)
			emit('.', at)
			lastSpace, lineStart = false, false
		case r == ' ' || unicode.IsSpace(r):
			if r == '\n' {
				lineStart = true
			}
			if !lastSpace {
				emit(' ', at)
				lastSpace = true
			}
		case lineStart && (r == '#' || r == '>'):
			// heading and block-quote markers at the start of a line
		case r == '*' || r == '_' || r == '`':
			// emphasis and code markers
		case r >= 0x80:
			// keep non-ASCII letters as their UTF-8 bytes, one offset each
			for k := 0; k < size; k++ {
				emit(s[at+k], at)
			}
			lastSpace, lineStart = false, false
		default:
			emit(byte(r), at)
			lastSpace, lineStart = false, false
		}
	}
	out := strings.TrimRight(b.String(), " ")
	return out, offsets[:len(out)]
}
