package text

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"unicode"
)

// OpKind says what a diff operation does to the text.
type OpKind string

const (
	OpEqual  OpKind = "equal"
	OpInsert OpKind = "insert"
	OpDelete OpKind = "delete"
)

// Op is one word-level step of a diff: text that is unchanged, added or
// removed. Whitespace and punctuation are tokens too, so the ops of a hunk
// concatenate back to its exact old and new text.
type Op struct {
	Kind OpKind `json:"kind"`
	Text string `json:"text"`
}

// Hunk is one contiguous region of change, addressed by byte offsets into
// the old text so a subset of hunks can be applied later. Ops hold the
// word-level marks inside the region; the context strings are the unchanged
// words just before and after it, for display.
type Hunk struct {
	Index         int    `json:"index"`
	OldStart      int    `json:"old_start"`
	OldEnd        int    `json:"old_end"`
	OldText       string `json:"old_text"`
	NewText       string `json:"new_text"`
	Ops           []Op   `json:"ops"`
	ContextBefore string `json:"context_before"`
	ContextAfter  string `json:"context_after"`
}

// DiffStats counts words added and removed across hunks.
type DiffStats struct {
	WordsAdded   int `json:"words_added"`
	WordsRemoved int `json:"words_removed"`
	Hunks        int `json:"hunks"`
}

// joinGap is the largest run of unchanged words that still joins two
// changes into one hunk; smaller gaps would scatter one edit over several.
const joinGap = 3

// contextChars bounds the context shown around a hunk.
const contextChars = 60

// maxEditDistance bounds the Myers search; beyond it the remaining text is
// treated as one replacement, which keeps memory bounded for a rewrite.
const maxEditDistance = 2500

// Diff computes word-level hunks that turn old into new. Applying every
// hunk reproduces new exactly; applying none reproduces old.
func Diff(old, new string) []Hunk {
	if old == new {
		return []Hunk{}
	}
	ops := coalesce(myers(tokenize(old), tokenize(new)))
	var hunks []Hunk
	oldPos := 0
	i := 0
	for i < len(ops) {
		if ops[i].Kind == OpEqual {
			oldPos += len(ops[i].Text)
			i++
			continue
		}
		// A change starts here. Extend over short equal gaps that separate changes.
		start := oldPos
		j := i
		var region []Op
		var newText strings.Builder
		pos := oldPos
		for j < len(ops) {
			op := ops[j]
			if op.Kind == OpEqual {
				// Absorb only if a change follows within the gap.
				if j+1 < len(ops) && Words(op.Text) <= joinGap {
					region = append(region, op)
					newText.WriteString(op.Text)
					pos += len(op.Text)
					j++
					continue
				}
				break
			}
			region = append(region, op)
			if op.Kind == OpInsert {
				newText.WriteString(op.Text)
			} else {
				pos += len(op.Text)
			}
			j++
		}
		// Do not end a hunk on absorbed equal text.
		for len(region) > 0 && region[len(region)-1].Kind == OpEqual {
			last := region[len(region)-1]
			region = region[:len(region)-1]
			pos -= len(last.Text)
			nt := newText.String()
			newText.Reset()
			newText.WriteString(nt[:len(nt)-len(last.Text)])
			j--
		}
		h := Hunk{Index: len(hunks), OldStart: start, OldEnd: pos, OldText: old[start:pos], NewText: newText.String(), Ops: region}
		h.ContextBefore = contextBefore(old, start)
		h.ContextAfter = contextAfter(old, pos)
		hunks = append(hunks, h)
		oldPos = pos
		i = j
	}
	if hunks == nil {
		hunks = []Hunk{}
	}
	return hunks
}

// ApplyHunks rebuilds the text with the accepted hunks applied and the rest
// left as they were. It fails when a hunk no longer matches old, which means
// the text changed after the diff was computed.
func ApplyHunks(old string, hunks []Hunk, accept func(h Hunk) bool) (string, error) {
	sorted := make([]Hunk, len(hunks))
	copy(sorted, hunks)
	sort.Slice(sorted, func(a, b int) bool { return sorted[a].OldStart < sorted[b].OldStart })
	var b strings.Builder
	pos := 0
	for _, h := range sorted {
		if h.OldStart < pos || h.OldEnd < h.OldStart || h.OldEnd > len(old) {
			return "", fmt.Errorf("text: hunk %d is out of range or overlaps another", h.Index)
		}
		if old[h.OldStart:h.OldEnd] != h.OldText {
			return "", errors.New("text: the text changed since the diff was computed")
		}
		b.WriteString(old[pos:h.OldStart])
		if accept == nil || accept(h) {
			b.WriteString(h.NewText)
		} else {
			b.WriteString(h.OldText)
		}
		pos = h.OldEnd
	}
	b.WriteString(old[pos:])
	return b.String(), nil
}

// Stats sums the words added and removed over hunks.
func Stats(hunks []Hunk) DiffStats {
	st := DiffStats{Hunks: len(hunks)}
	for _, h := range hunks {
		for _, op := range h.Ops {
			switch op.Kind {
			case OpInsert:
				st.WordsAdded += Words(op.Text)
			case OpDelete:
				st.WordsRemoved += Words(op.Text)
			}
		}
	}
	return st
}

func contextBefore(s string, at int) string {
	start := at - contextChars
	if start <= 0 {
		return s[:at]
	}
	// Start on a word boundary.
	for start < at && !unicode.IsSpace(rune(s[start-1])) {
		start++
	}
	return s[start:at]
}

func contextAfter(s string, at int) string {
	end := at + contextChars
	if end >= len(s) {
		return s[at:]
	}
	for end > at && !unicode.IsSpace(rune(s[end])) {
		end--
	}
	return s[at:end]
}

// tokenize splits text into words, whitespace runs and single punctuation
// marks. Apostrophes inside a word stay in the word ("don't", "l’heure").
func tokenize(s string) []string {
	var out []string
	runes := []rune(s)
	i := 0
	kindOf := func(r rune) int {
		switch {
		case unicode.IsSpace(r):
			return 0
		case unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.Is(unicode.Mn, r):
			return 1
		default:
			return 2
		}
	}
	for i < len(runes) {
		r := runes[i]
		k := kindOf(r)
		j := i + 1
		switch k {
		case 0:
			for j < len(runes) && kindOf(runes[j]) == 0 {
				j++
			}
		case 1:
			for j < len(runes) {
				if kindOf(runes[j]) == 1 {
					j++
					continue
				}
				if (runes[j] == '\'' || runes[j] == '’') && j+1 < len(runes) && kindOf(runes[j+1]) == 1 {
					j += 2
					continue
				}
				break
			}
		default:
			// one punctuation mark per token
		}
		out = append(out, string(runes[i:j]))
		i = j
	}
	return out
}

// coalesce joins consecutive ops of the same kind.
func coalesce(ops []Op) []Op {
	var out []Op
	for _, op := range ops {
		if op.Text == "" {
			continue
		}
		if n := len(out); n > 0 && out[n-1].Kind == op.Kind {
			out[n-1].Text += op.Text
			continue
		}
		out = append(out, op)
	}
	return out
}

// myers computes a shortest edit script between token slices (Myers 1986),
// after trimming the common prefix and suffix. Past maxEditDistance the
// middle is reported as one deletion and one insertion.
func myers(a, b []string) []Op {
	prefix := 0
	for prefix < len(a) && prefix < len(b) && a[prefix] == b[prefix] {
		prefix++
	}
	suffix := 0
	for suffix < len(a)-prefix && suffix < len(b)-prefix && a[len(a)-1-suffix] == b[len(b)-1-suffix] {
		suffix++
	}
	var out []Op
	if prefix > 0 {
		out = append(out, Op{Kind: OpEqual, Text: strings.Join(a[:prefix], "")})
	}
	mid := myersCore(a[prefix:len(a)-suffix], b[prefix:len(b)-suffix])
	out = append(out, mid...)
	if suffix > 0 {
		out = append(out, Op{Kind: OpEqual, Text: strings.Join(a[len(a)-suffix:], "")})
	}
	return out
}

func myersCore(a, b []string) []Op {
	n, m := len(a), len(b)
	if n == 0 && m == 0 {
		return nil
	}
	if n == 0 {
		return []Op{{Kind: OpInsert, Text: strings.Join(b, "")}}
	}
	if m == 0 {
		return []Op{{Kind: OpDelete, Text: strings.Join(a, "")}}
	}
	max := n + m
	off := max
	v := make([]int, 2*max+2)
	var trace [][]int
	for d := 0; d <= max; d++ {
		if d > maxEditDistance {
			return []Op{{Kind: OpDelete, Text: strings.Join(a, "")}, {Kind: OpInsert, Text: strings.Join(b, "")}}
		}
		snapshot := make([]int, len(v))
		copy(snapshot, v)
		trace = append(trace, snapshot)
		for k := -d; k <= d; k += 2 {
			var x int
			if k == -d || (k != d && v[k-1+off] < v[k+1+off]) {
				x = v[k+1+off]
			} else {
				x = v[k-1+off] + 1
			}
			y := x - k
			for x < n && y < m && a[x] == b[y] {
				x++
				y++
			}
			v[k+off] = x
			if x >= n && y >= m {
				return backtrack(a, b, trace, off)
			}
		}
	}
	return []Op{{Kind: OpDelete, Text: strings.Join(a, "")}, {Kind: OpInsert, Text: strings.Join(b, "")}}
}

func backtrack(a, b []string, trace [][]int, off int) []Op {
	x, y := len(a), len(b)
	var rev []Op
	for d := len(trace) - 1; d >= 0; d-- {
		v := trace[d]
		k := x - y
		var prevK int
		if k == -d || (k != d && v[k-1+off] < v[k+1+off]) {
			prevK = k + 1
		} else {
			prevK = k - 1
		}
		prevX := v[prevK+off]
		prevY := prevX - prevK
		for x > prevX && y > prevY {
			rev = append(rev, Op{Kind: OpEqual, Text: a[x-1]})
			x--
			y--
		}
		if d > 0 {
			if x == prevX {
				rev = append(rev, Op{Kind: OpInsert, Text: b[prevY]})
			} else {
				rev = append(rev, Op{Kind: OpDelete, Text: a[prevX]})
			}
		}
		x, y = prevX, prevY
	}
	for i, j := 0, len(rev)-1; i < j; i, j = i+1, j-1 {
		rev[i], rev[j] = rev[j], rev[i]
	}
	return rev
}
