package guild

import (
	"errors"
	"strings"
	"testing"
)

const chapter = "# The Gate\n\nThe rain had stopped before she reached the gate. She did not look back.\n\n“You’re late,” said the porter—without warmth.\n\nShe counted the coins twice and paid him anyway."

func TestParseCritiqueValid(t *testing.T) {
	reply := "Here you go:\n```json\n" + `{
  "writer": "Hemingway",
  "overall": "Clean and quick. The porter arrives too easily.",
  "issues": [
    {"id": "a", "severity": "High", "quote": "She did not look back.", "problem": "Tells what showing already did.", "suggested_fix": "Cut it."},
    {"id": "b", "severity": "low", "quote": "\"You're late,\" said the porter-without warmth.", "problem": "The dash stalls the line.", "suggested_fix": "Use a full stop."}
  ],
  "bible_conflicts": [
    {"quote": "paid him anyway", "conflicts_with": "She has no money after chapter two."},
    {"quote": "the dragon roared", "conflicts_with": "no dragons"}
  ]
}` + "\n```\nHope this helps."
	c, err := ParseCritique(reply, chapter, "Fallback", "")
	if err != nil {
		t.Fatal(err)
	}
	if c.Writer != "Hemingway" || len(c.Issues) != 2 || len(c.BibleConflicts) != 1 {
		t.Fatalf("critique %+v", c)
	}
	if c.Issues[0].Severity != "high" || c.Issues[0].ID != "a" || !c.Issues[0].QuoteExact {
		t.Fatalf("issue 0 %+v", c.Issues[0])
	}
	if got := chapter[c.Issues[0].Start:c.Issues[0].End]; got != "She did not look back." {
		t.Fatalf("anchor 0 %q", got)
	}
	if got := chapter[c.Issues[1].Start:c.Issues[1].End]; got != "“You’re late,” said the porter—without warmth." || c.Issues[1].QuoteExact {
		t.Fatalf("anchor 1 %q exact=%v", got, c.Issues[1].QuoteExact)
	}
	if got := chapter[c.BibleConflicts[0].Start:c.BibleConflicts[0].End]; got != "paid him anyway" {
		t.Fatalf("conflict anchor %q", got)
	}
	joined := strings.Join(c.Warnings, " | ")
	if !strings.Contains(joined, "bible conflict 2") || !strings.Contains(joined, "normalising") {
		t.Fatalf("warnings %v", c.Warnings)
	}
}

func TestParseCritiqueRepairs(t *testing.T) {
	reply := `{"writer": "", "overall": "One. Two. Three.", "issues": [
    {"id": "", "severity": "low", "quote": "counted the coins", "problem": "p", "suggested_fix": ""},
    {"id": "x", "severity": "high", "quote": "reached the gate", "problem": "p", "suggested_fix": ""},
    {"id": "x", "severity": "medium", "quote": "said the porter", "problem": "p", "suggested_fix": ""},
    {"id": "y", "severity": "high", "quote": "paid him", "problem": "p", "suggested_fix": ""}
  ]}`
	c, err := ParseCritique(reply, chapter, "Le Guin", "s2")
	if err != nil {
		t.Fatal(err)
	}
	if c.Writer != "Le Guin" {
		t.Fatalf("writer %q", c.Writer)
	}
	if c.Overall != "One. Two." {
		t.Fatalf("overall %q", c.Overall)
	}
	if len(c.Issues) != 3 {
		t.Fatalf("want 3 issues, got %d", len(c.Issues))
	}
	// Two highs first, then the medium; the low one was dropped.
	if c.Issues[0].Severity != "high" || c.Issues[1].Severity != "high" || c.Issues[2].Severity != "medium" {
		t.Fatalf("order %s %s %s", c.Issues[0].Severity, c.Issues[1].Severity, c.Issues[2].Severity)
	}
	ids := map[string]bool{}
	for _, is := range c.Issues {
		if !strings.HasPrefix(is.ID, "s2-") || ids[is.ID] {
			t.Fatalf("ids not unique and prefixed: %+v", c.Issues)
		}
		ids[is.ID] = true
	}
	if c.BibleConflicts == nil || len(c.BibleConflicts) != 0 {
		t.Fatalf("missing bible_conflicts should become an empty list, got %v", c.BibleConflicts)
	}
}

func TestParseCritiqueRejects(t *testing.T) {
	cases := map[string]string{
		"no json":       "I could not do that.",
		"broken json":   `{"writer": "x", "issues": [}`,
		"bad severity":  `{"issues": [{"id":"1","severity":"urgent","quote":"reached the gate","problem":"p"}]}`,
		"empty problem": `{"issues": [{"id":"1","severity":"high","quote":"reached the gate","problem":""}]}`,
		"empty quote":   `{"issues": [{"id":"1","severity":"high","quote":"","problem":"p"}]}`,
		"made-up quote": `{"issues": [{"id":"1","severity":"high","quote":"The dragon roared at dawn.","problem":"p"}]}`,
	}
	for name, reply := range cases {
		_, err := ParseCritique(reply, chapter, "w", "")
		var verr *ValidationError
		if !errors.As(err, &verr) || len(verr.Problems) == 0 {
			t.Errorf("%s: want ValidationError, got %v", name, err)
			continue
		}
		if name == "made-up quote" && !strings.Contains(verr.Problems[0], "does not appear in the chapter") {
			t.Errorf("made-up quote problem: %q", verr.Problems[0])
		}
		prompt := RetryPrompt(verr)
		if !strings.Contains(prompt, verr.Problems[0]) || !strings.Contains(prompt, "corrected JSON") {
			t.Errorf("%s: retry prompt %q", name, prompt)
		}
	}
}

func TestExtractJSON(t *testing.T) {
	for _, in := range []string{`{"a":1}`, "```json\n{\"a\":1}\n```", "Sure!\n\n{\"a\":1}\n\nDone.", "```\n{\"a\":1}```"} {
		got, err := ExtractJSON(in)
		if err != nil || got != `{"a":1}` {
			t.Errorf("ExtractJSON(%q) = %q, %v", in, got, err)
		}
	}
	if _, err := ExtractJSON("no braces here"); err == nil {
		t.Error("expected an error without braces")
	}
}

func TestLimitSentences(t *testing.T) {
	cases := []struct {
		in, want string
		cut      bool
	}{
		{"One. Two. Three.", "One. Two.", true},
		{"One. Two.", "One. Two.", false},
		{"Only one", "Only one", false},
		{"It cost 3.5 dollars. Fine! Really?", "It cost 3.5 dollars. Fine!", true},
		{"“Go.” She went. He stayed.", "“Go.” She went.", true},
	}
	for _, c := range cases {
		got, cut := limitSentences(c.in, 2)
		if got != c.want || cut != c.cut {
			t.Errorf("limitSentences(%q) = %q, %v; want %q, %v", c.in, got, cut, c.want, c.cut)
		}
	}
}
