package text

import (
	"fmt"
	"strings"
)

// Scene is one slice of a chapter, addressed by byte offsets into the
// original Markdown so anything found inside it maps back to the chapter.
type Scene struct {
	Index int
	Title string
	Text  string
	Start int // byte offset of Text in the chapter
	End   int // byte offset just past Text
}

// SplitScenes returns the whole chapter as one scene when it fits within
// tokenLimit, otherwise cuts it at scene breaks (horizontal rules) and
// headings, merges neighbours that fit together, and as a last resort splits
// an oversized section at paragraph breaks. Scenes cover the chapter in
// order without gaps; a limit of zero or less means no limit.
func SplitScenes(md string, tokenLimit int) []Scene {
	if tokenLimit <= 0 || EstimateTokens(md) <= tokenLimit {
		return []Scene{{Index: 0, Title: "", Text: md, Start: 0, End: len(md)}}
	}
	segments := cutAtBoundaries(md)
	var pieces []span
	for _, seg := range segments {
		if EstimateTokens(md[seg.start:seg.end]) <= tokenLimit {
			pieces = append(pieces, seg)
			continue
		}
		pieces = append(pieces, cutAtParagraphs(md, seg, tokenLimit)...)
	}
	merged := mergeSpans(md, pieces, tokenLimit)
	scenes := make([]Scene, 0, len(merged))
	for i, sp := range merged {
		text := md[sp.start:sp.end]
		scenes = append(scenes, Scene{Index: i, Title: sceneTitle(text, i), Text: text, Start: sp.start, End: sp.end})
	}
	return scenes
}

type span struct{ start, end int }

// cutAtBoundaries splits at lines that are horizontal rules or ATX headings.
// The boundary line opens the segment that follows it.
func cutAtBoundaries(md string) []span {
	var out []span
	start := 0
	pos := 0
	first := true
	for pos < len(md) {
		nl := strings.IndexByte(md[pos:], '\n')
		lineEnd := len(md)
		if nl >= 0 {
			lineEnd = pos + nl
		}
		line := md[pos:lineEnd]
		if !first && isBoundary(line) && pos > start {
			out = append(out, span{start, pos})
			start = pos
		}
		first = false
		if nl < 0 {
			break
		}
		pos = lineEnd + 1
	}
	if start < len(md) || len(out) == 0 {
		out = append(out, span{start, len(md)})
	}
	return out
}

// isBoundary reports a Markdown horizontal rule ("---", "***", "* * *",
// "___") or a heading line ("# ...").
func isBoundary(line string) bool {
	t := strings.TrimSpace(line)
	if t == "" {
		return false
	}
	if strings.HasPrefix(t, "#") {
		rest := strings.TrimLeft(t, "#")
		return len(t)-len(rest) <= 6 && (rest == "" || rest[0] == ' ')
	}
	compact := strings.ReplaceAll(t, " ", "")
	if len(compact) < 3 {
		return false
	}
	c := compact[0]
	if c != '-' && c != '*' && c != '_' {
		return false
	}
	return strings.Trim(compact, string(c)) == ""
}

// cutAtParagraphs splits an oversized span at blank lines, packing paragraphs
// greedily up to the limit. A single paragraph above the limit stays whole.
func cutAtParagraphs(md string, sp span, limit int) []span {
	var paras []span
	pos := sp.start
	for pos < sp.end {
		idx := strings.Index(md[pos:sp.end], "\n\n")
		if idx < 0 {
			paras = append(paras, span{pos, sp.end})
			break
		}
		end := pos + idx
		// Swallow the run of blank lines into this paragraph so spans stay contiguous.
		for end < sp.end && md[end] == '\n' {
			end++
		}
		paras = append(paras, span{pos, end})
		pos = end
	}
	var out []span
	cur := span{-1, -1}
	for _, p := range paras {
		if cur.start < 0 {
			cur = p
			continue
		}
		if EstimateTokens(md[cur.start:p.end]) <= limit {
			cur.end = p.end
			continue
		}
		out = append(out, cur)
		cur = p
	}
	if cur.start >= 0 {
		out = append(out, cur)
	}
	return out
}

// mergeSpans joins neighbouring spans while the result stays within limit.
func mergeSpans(md string, spans []span, limit int) []span {
	var out []span
	for _, sp := range spans {
		if n := len(out); n > 0 && EstimateTokens(md[out[n-1].start:sp.end]) <= limit {
			out[n-1].end = sp.end
			continue
		}
		out = append(out, sp)
	}
	return out
}

// sceneTitle uses the heading a scene opens with, else "Scene N".
func sceneTitle(text string, index int) string {
	for _, line := range strings.SplitN(text, "\n", 3) {
		t := strings.TrimSpace(line)
		if t == "" {
			continue
		}
		if strings.HasPrefix(t, "#") {
			if h := strings.TrimSpace(strings.TrimLeft(t, "#")); h != "" {
				return h
			}
		}
		break
	}
	return fmt.Sprintf("Scene %d", index+1)
}
