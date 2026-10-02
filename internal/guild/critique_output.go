package guild

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"writersguild/internal/text"
)

// Severity levels a critic may assign, most urgent first.
const (
	SeverityHigh   = "high"
	SeverityMedium = "medium"
	SeverityLow    = "low"
)

var severityRank = map[string]int{SeverityHigh: 0, SeverityMedium: 1, SeverityLow: 2}

// MaxIssues is how many issues one critic may raise per chapter or scene.
const MaxIssues = 3

// Issue is one problem a critic found, anchored in the chapter text.
type Issue struct {
	ID           string `json:"id"`
	Severity     string `json:"severity"`
	Quote        string `json:"quote"`
	Problem      string `json:"problem"`
	SuggestedFix string `json:"suggested_fix"`
	// Start and End are byte offsets of the quote in the chapter Markdown;
	// QuoteExact is false when the quote matched only after normalisation.
	Start      int  `json:"start"`
	End        int  `json:"end"`
	QuoteExact bool `json:"quote_exact"`
}

// BibleConflict is a passage that contradicts the story bible.
type BibleConflict struct {
	Quote         string `json:"quote"`
	ConflictsWith string `json:"conflicts_with"`
	Start         int    `json:"start"`
	End           int    `json:"end"`
}

// Critique is one critic's validated reply.
type Critique struct {
	Writer         string          `json:"writer"`
	Overall        string          `json:"overall"`
	Issues         []Issue         `json:"issues"`
	BibleConflicts []BibleConflict `json:"bible_conflicts"`
	// Warnings lists what validation repaired without rejecting the reply.
	Warnings []string `json:"warnings,omitempty"`
}

// ValidationError means the reply cannot be used and the writer should be
// asked again. Problems are written for the model to act on.
type ValidationError struct {
	Problems []string
}

func (e *ValidationError) Error() string {
	return "invalid critique: " + strings.Join(e.Problems, "; ")
}

// CritiqueFormat is the output contract given to every critic.
const CritiqueFormat = `Return ONLY a JSON object with exactly this shape and nothing else:
{
  "writer": "<your name>",
  "overall": "<at most two sentences on the chapter as a whole>",
  "issues": [
    {
      "id": "<short unique id such as i1>",
      "severity": "high" | "medium" | "low",
      "quote": "<an exact, verbatim passage copied from the chapter text, one sentence or less, long enough to be unique>",
      "problem": "<what is wrong and why it matters>",
      "suggested_fix": "<a concrete rewrite or change, in the author's voice>"
    }
  ],
  "bible_conflicts": [
    { "quote": "<exact passage from the chapter>", "conflicts_with": "<the story bible fact it contradicts>" }
  ]
}
Rules: at most 3 issues, most important first; every "quote" must be copied character for character from the chapter, never paraphrased or invented; use [] for no issues or no conflicts; no Markdown fences, no commentary.`

// ExtractJSON pulls the JSON object out of a reply that may wrap it in code
// fences or stray prose.
func ExtractJSON(reply string) (string, error) {
	s := strings.TrimSpace(reply)
	if strings.HasPrefix(s, "```") {
		if nl := strings.IndexByte(s, '\n'); nl >= 0 {
			s = s[nl+1:]
		}
		if end := strings.LastIndex(s, "```"); end >= 0 {
			s = s[:end]
		}
		s = strings.TrimSpace(s)
	}
	start := strings.IndexByte(s, '{')
	end := strings.LastIndexByte(s, '}')
	if start < 0 || end < start {
		return "", errors.New("the reply contains no JSON object")
	}
	return s[start : end+1], nil
}

type rawCritique struct {
	Writer  string `json:"writer"`
	Overall string `json:"overall"`
	Issues  []struct {
		ID           string `json:"id"`
		Severity     string `json:"severity"`
		Quote        string `json:"quote"`
		Problem      string `json:"problem"`
		SuggestedFix string `json:"suggested_fix"`
	} `json:"issues"`
	BibleConflicts []struct {
		Quote         string `json:"quote"`
		ConflictsWith string `json:"conflicts_with"`
	} `json:"bible_conflicts"`
}

// ParseCritique decodes a critic's reply and validates it against the
// chapter text. writerName fills a missing "writer"; idPrefix makes issue ids
// unique across scenes ("s2" gives ids like s2-i1). Quotes that are not in the
// chapter, bad severities and empty problems return a ValidationError so the
// writer can be asked once more; smaller faults are repaired and listed in
// Warnings.
func ParseCritique(reply, chapterText, writerName, idPrefix string) (*Critique, error) {
	body, err := ExtractJSON(reply)
	if err != nil {
		return nil, &ValidationError{Problems: []string{err.Error()}}
	}
	var raw rawCritique
	dec := json.NewDecoder(strings.NewReader(body))
	if err := dec.Decode(&raw); err != nil {
		return nil, &ValidationError{Problems: []string{"the JSON does not parse: " + err.Error()}}
	}
	out := &Critique{Writer: strings.TrimSpace(raw.Writer), Issues: []Issue{}, BibleConflicts: []BibleConflict{}}
	var problems []string
	if out.Writer == "" {
		out.Writer = writerName
		out.Warnings = append(out.Warnings, `"writer" was empty and was filled in`)
	}
	overall, cut := limitSentences(strings.TrimSpace(raw.Overall), 2)
	if cut {
		out.Warnings = append(out.Warnings, `"overall" ran past two sentences and was shortened`)
	}
	out.Overall = overall

	seen := map[string]bool{}
	for i, ri := range raw.Issues {
		n := i + 1
		is := Issue{ID: strings.TrimSpace(ri.ID), Severity: strings.ToLower(strings.TrimSpace(ri.Severity)),
			Quote: strings.TrimSpace(ri.Quote), Problem: strings.TrimSpace(ri.Problem), SuggestedFix: strings.TrimSpace(ri.SuggestedFix)}
		if _, ok := severityRank[is.Severity]; !ok {
			problems = append(problems, fmt.Sprintf("issue %d: severity %q is not one of high, medium, low", n, ri.Severity))
		}
		if is.Problem == "" {
			problems = append(problems, fmt.Sprintf("issue %d: \"problem\" is empty", n))
		}
		if is.Quote == "" {
			problems = append(problems, fmt.Sprintf("issue %d: \"quote\" is empty", n))
		} else if m, ok := text.FindQuote(chapterText, is.Quote); ok {
			is.Start, is.End, is.QuoteExact = m.Start, m.End, m.Exact
			if !m.Exact {
				out.Warnings = append(out.Warnings, fmt.Sprintf("issue %d: the quote matched the chapter only after normalising punctuation", n))
			}
		} else {
			problems = append(problems, fmt.Sprintf("issue %d: the quote %q does not appear in the chapter; copy an exact passage", n, truncate(is.Quote, 80)))
		}
		if is.ID == "" || seen[is.ID] {
			is.ID = fmt.Sprintf("i%d", n)
			out.Warnings = append(out.Warnings, fmt.Sprintf("issue %d: id was missing or duplicated and was replaced", n))
		}
		seen[is.ID] = true
		out.Issues = append(out.Issues, is)
	}
	if len(problems) > 0 {
		return nil, &ValidationError{Problems: problems}
	}
	if len(out.Issues) > MaxIssues {
		sort.SliceStable(out.Issues, func(a, b int) bool {
			return severityRank[out.Issues[a].Severity] < severityRank[out.Issues[b].Severity]
		})
		out.Issues = out.Issues[:MaxIssues]
		out.Warnings = append(out.Warnings, fmt.Sprintf("more than %d issues were returned; the %d most severe were kept", MaxIssues, MaxIssues))
	}
	if idPrefix != "" {
		for i := range out.Issues {
			out.Issues[i].ID = idPrefix + "-" + out.Issues[i].ID
		}
	}
	for i, rc := range raw.BibleConflicts {
		bc := BibleConflict{Quote: strings.TrimSpace(rc.Quote), ConflictsWith: strings.TrimSpace(rc.ConflictsWith)}
		if bc.Quote == "" || bc.ConflictsWith == "" {
			out.Warnings = append(out.Warnings, fmt.Sprintf("bible conflict %d was incomplete and was dropped", i+1))
			continue
		}
		m, ok := text.FindQuote(chapterText, bc.Quote)
		if !ok {
			out.Warnings = append(out.Warnings, fmt.Sprintf("bible conflict %d quoted text that is not in the chapter and was dropped", i+1))
			continue
		}
		bc.Start, bc.End = m.Start, m.End
		out.BibleConflicts = append(out.BibleConflicts, bc)
	}
	return out, nil
}

// RetryPrompt tells the writer what was wrong with its last reply.
func RetryPrompt(verr *ValidationError) string {
	var b strings.Builder
	b.WriteString("Your previous reply was not valid and was discarded:\n")
	for _, p := range verr.Problems {
		b.WriteString("- ")
		b.WriteString(p)
		b.WriteString("\n")
	}
	b.WriteString("\nReply again with the corrected JSON object only. Every quote must be copied exactly from the chapter text you were given.")
	return b.String()
}

// limitSentences keeps the first n sentences. It reports whether anything
// was cut.
func limitSentences(s string, n int) (string, bool) {
	count := 0
	for i, r := range s {
		if r != '.' && r != '!' && r != '?' {
			continue
		}
		next := i + utf8.RuneLen(r)
		// Closing quotes after the terminator belong to the sentence.
		for next < len(s) {
			q, size := utf8.DecodeRuneInString(s[next:])
			if !strings.ContainsRune(`"'”’)`, q) {
				break
			}
			next += size
		}
		if next < len(s) {
			if following, _ := utf8.DecodeRuneInString(s[next:]); !unicode.IsSpace(following) {
				continue // an abbreviation or a number such as 3.5
			}
		}
		count++
		if count == n && next < len(s) {
			return strings.TrimSpace(s[:next]), true
		}
	}
	return s, false
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
