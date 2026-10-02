package guild

import (
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func sampleSources() []CritiqueSource {
	hem := uuid.New()
	leg := uuid.New()
	return []CritiqueSource{
		{CritiqueID: uuid.New(), WriterID: hem, WriterName: "Hemingway", WriterSlug: "hemingway", Critique: &Critique{
			Writer: "Hemingway", Overall: "Too much telling.",
			Issues: []Issue{
				{ID: "i1", Severity: "high", Quote: "She did not look back.", Problem: "Tells what showing already did.", SuggestedFix: "Cut it."},
				{ID: "i2", Severity: "low", Quote: "paid him anyway", Problem: "Soft ending.", SuggestedFix: "End on the coins."},
			},
			BibleConflicts: []BibleConflict{{Quote: "paid him anyway", ConflictsWith: "she has no money"}},
		}},
		{CritiqueID: uuid.New(), WriterID: leg, WriterName: "Le Guin", WriterSlug: "le-guin", Critique: &Critique{
			Writer: "Le Guin", Overall: "Quiet and good.",
			Issues: []Issue{
				{ID: "i1", Severity: "medium", Quote: "She did not look back.", Problem: "Redundant beat.", SuggestedFix: "Drop the sentence."},
			},
		}},
	}
}

func TestBuildEditorInput(t *testing.T) {
	in := BuildEditorInput("The Long Winter", "The Gate", chapter, "## Characters\n- **Mara**: role: porter.\n", 6000, sampleSources())
	if len(in.Sources) != 3 || len(in.Order) != 3 {
		t.Fatalf("sources %d order %v", len(in.Sources), in.Order)
	}
	if in.Order[0] != "hemingway/i1" || in.Order[2] != "le-guin/i1" {
		t.Fatalf("order %v", in.Order)
	}
	ref := in.Sources["le-guin/i1"]
	if ref.WriterName != "Le Guin" || ref.IssueID != "i1" || ref.Issue.Quote != "She did not look back." {
		t.Fatalf("ref %+v", ref)
	}
	for _, want := range []string{
		`# Story bible of "The Long Winter"`, "**Mara**", "# Chapter: The Gate", ChapterBegin, "She counted the coins twice", ChapterEnd,
		"## Hemingway (2 issues)", "Overall: Too much telling.", `- [hemingway/i1] high — quote: "She did not look back." — problem: Tells what showing already did. — fix: Cut it.`,
		"Story bible conflicts raised:", `"paid him anyway" conflicts with she has no money`, "## Le Guin (1 issue)", "[le-guin/i1] medium", "Return the JSON object only.",
	} {
		if !strings.Contains(in.Prompt, want) {
			t.Errorf("prompt lacks %q", want)
		}
	}
	// A chapter over the limit is left out and the editor is told so.
	long := BuildEditorInput("P", "C", strings.Repeat("word ", 4000), "", 500, sampleSources())
	if strings.Contains(long.Prompt, ChapterBegin) || !strings.Contains(long.Prompt, "too long to include") {
		t.Fatalf("long chapter handling wrong")
	}
	if !strings.Contains(EditorSystemPrompt("You are the editor."), FixedSuffix) || !strings.Contains(EditorSystemPrompt("x"), EditorFormat) {
		t.Fatal("editor system prompt lacks the suffix or the format")
	}
}

func TestParseSynthesisMergesAndValidates(t *testing.T) {
	in := BuildEditorInput("P", "C", chapter, "", 0, sampleSources())
	reply := "```json\n" + `{"issues": [
	  {"id": "e1", "severity": "high", "quote": "She did not look back.", "problem": "Both critics find the beat redundant.", "suggested_fix": "Cut the sentence.", "sources": ["hemingway/i1", "le-guin/i1", "le-guin/i1"]},
	  {"id": "", "severity": "LOW", "quote": "paid him anyway", "problem": "Soft ending.", "suggested_fix": "", "sources": ["hemingway/i2", "nobody/i9"]}
	]}` + "\n```"
	syn, err := ParseSynthesis(reply, chapter, in)
	if err != nil {
		t.Fatal(err)
	}
	if len(syn.Issues) != 2 || syn.Fallback {
		t.Fatalf("synthesis %+v", syn)
	}
	first := syn.Issues[0]
	if len(first.Sources) != 2 || first.Sources[0].WriterSlug != "hemingway" || first.Sources[1].WriterSlug != "le-guin" || first.Sources[1].IssueID != "i1" {
		t.Fatalf("sources %+v", first.Sources)
	}
	if chapter[first.Start:first.End] != "She did not look back." || !first.QuoteExact {
		t.Fatalf("anchor %q", chapter[first.Start:first.End])
	}
	second := syn.Issues[1]
	if second.ID != "e2" || second.Severity != "low" || len(second.Sources) != 1 {
		t.Fatalf("second %+v", second)
	}
	joined := strings.Join(syn.Warnings, " | ")
	if !strings.Contains(joined, `unknown source "nobody/i9"`) || !strings.Contains(joined, "id was missing") {
		t.Fatalf("warnings %v", syn.Warnings)
	}
	if strings.Contains(joined, "set aside") {
		t.Fatalf("every note was cited, no set-aside warning expected: %v", syn.Warnings)
	}
}

func TestParseSynthesisLooseSourcesAndSetAside(t *testing.T) {
	in := BuildEditorInput("P", "C", chapter, "", 0, sampleSources())
	reply := `{"issues": [{"id": "e1", "severity": "medium", "quote": "reached the gate", "problem": "p", "suggested_fix": "f", "sources": ["le-guin"]}]}`
	syn, err := ParseSynthesis(reply, chapter, in)
	if err != nil {
		t.Fatal(err)
	}
	if len(syn.Issues[0].Sources) != 1 || syn.Issues[0].Sources[0].ID != "le-guin/i1" {
		t.Fatalf("bare slug should resolve to the writer's only issue: %+v", syn.Issues[0].Sources)
	}
	if !strings.Contains(strings.Join(syn.Warnings, " "), "set aside 2 of the critics' 3 notes") {
		t.Fatalf("warnings %v", syn.Warnings)
	}
	// "i1" is used by two critics, so it is ambiguous and dropped; the issue then has no source and is fatal.
	_, err = ParseSynthesis(`{"issues": [{"id": "e1", "severity": "medium", "quote": "reached the gate", "problem": "p", "sources": ["i1"]}]}`, chapter, in)
	var verr *ValidationError
	if !errors.As(err, &verr) || !strings.Contains(verr.Problems[0], "cites no known source") {
		t.Fatalf("ambiguous source: %v", err)
	}
}

func TestParseSynthesisRejects(t *testing.T) {
	in := BuildEditorInput("P", "C", chapter, "", 0, sampleSources())
	cases := map[string]string{
		"no json":      "Nothing to merge.",
		"bad severity": `{"issues": [{"id":"e1","severity":"urgent","quote":"reached the gate","problem":"p","sources":["hemingway/i1"]}]}`,
		"bad quote":    `{"issues": [{"id":"e1","severity":"high","quote":"The dragon roared.","problem":"p","sources":["hemingway/i1"]}]}`,
		"no problem":   `{"issues": [{"id":"e1","severity":"high","quote":"reached the gate","problem":"","sources":["hemingway/i1"]}]}`,
		"no sources":   `{"issues": [{"id":"e1","severity":"high","quote":"reached the gate","problem":"p","sources":[]}]}`,
		"invented":     `{"issues": [{"id":"e1","severity":"high","quote":"reached the gate","problem":"p","sources":["stephen-king/i1"]}]}`,
	}
	for name, reply := range cases {
		_, err := ParseSynthesis(reply, chapter, in)
		var verr *ValidationError
		if !errors.As(err, &verr) {
			t.Errorf("%s: want ValidationError, got %v", name, err)
		}
	}
	// An empty list is valid: the editor may set every note aside.
	syn, err := ParseSynthesis(`{"issues": []}`, chapter, in)
	if err != nil || len(syn.Issues) != 0 || !strings.Contains(syn.Warnings[0], "set aside 3 of the critics' 3 notes") {
		t.Fatalf("empty list: %v %+v", err, syn)
	}
}

func TestFallbackSynthesis(t *testing.T) {
	syn := FallbackSynthesis(sampleSources(), "the gateway failed upstream (500)")
	if !syn.Fallback || len(syn.Issues) != 3 {
		t.Fatalf("fallback %+v", syn)
	}
	if syn.Issues[0].Severity != "high" || syn.Issues[1].Severity != "medium" || syn.Issues[2].Severity != "low" {
		t.Fatalf("order %s %s %s", syn.Issues[0].Severity, syn.Issues[1].Severity, syn.Issues[2].Severity)
	}
	if syn.Issues[0].ID != "e1" || syn.Issues[0].Sources[0].ID != "hemingway/i1" || syn.Issues[1].Sources[0].WriterSlug != "le-guin" {
		t.Fatalf("ids/sources %+v", syn.Issues)
	}
	if !strings.Contains(syn.Warnings[0], "500") {
		t.Fatalf("warning %v", syn.Warnings)
	}
}
