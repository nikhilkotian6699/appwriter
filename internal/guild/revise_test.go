package guild

import (
	"strings"
	"testing"

	"github.com/google/uuid"

	"writersguild/internal/db/sqlcgen"
	"writersguild/internal/text"
)

func TestValidateRevision(t *testing.T) {
	original := strings.Repeat("The rain had stopped before she reached the gate. ", 5) + "\n"
	ok, err := ValidateRevision(original, "```markdown\n"+strings.TrimSpace(original)+"\n```")
	if err != nil || !strings.HasSuffix(ok, "\n") || strings.Contains(ok, "```") {
		t.Fatalf("fenced reply: %v %q", err, ok)
	}
	if _, err := ValidateRevision(original, "   "); err == nil {
		t.Fatal("empty reply should fail")
	}
	if _, err := ValidateRevision(original, "Here is a summary: she left."); err == nil || !strings.Contains(err.Problems[0], "not a summary") {
		t.Fatalf("short reply: %v", err)
	}
	if _, err := ValidateRevision(original, strings.Repeat("more words ", 200)); err == nil || !strings.Contains(err.Problems[0], "new material") {
		t.Fatalf("long reply: %v", err)
	}
	// Surrounding whitespace follows the original, not the reply.
	if got, _ := ValidateRevision("\n\nText here.\n\n", "  Text there.  "); got != "\n\nText there.\n\n" {
		t.Fatalf("whitespace: %q", got)
	}
	// Short originals are not length-checked.
	if got, err := ValidateRevision("Hi.", "Hello there, friend of mine, how are you today?"); err != nil || got == "" {
		t.Fatalf("short original: %v %q", err, got)
	}
}

func TestLocateIssuesAndFinalFix(t *testing.T) {
	ch := sqlcgen.Chapter{ContentMd: chapter, ContentHash: "h1"}
	edited := "Cut it entirely."
	issues := []sqlcgen.Issue{
		{ID: uuid.New(), Key: "e1", Quote: "She did not look back.", QuoteStart: 60, QuoteEnd: 82, ContentHash: "h1", SuggestedFix: "Cut it.", EditedFix: &edited},
		{ID: uuid.New(), Key: "e2", Quote: "paid him anyway", ContentHash: "old-hash", QuoteStart: 0, QuoteEnd: 15, SuggestedFix: "End on the coins."},
		{ID: uuid.New(), Key: "e3", Quote: "The dragon roared.", ContentHash: "h1", SuggestedFix: "Drop it."},
		{ID: uuid.New(), Key: "e4", Quote: "reached the gate", ContentHash: "h1", SuggestedFix: "   "},
	}
	located, skipped := locateIssues(ch, issues)
	if len(located) != 2 || len(skipped) != 2 {
		t.Fatalf("located %d skipped %d", len(located), len(skipped))
	}
	if located[0].Fix != edited || chapter[located[0].Start:located[0].End] != "She did not look back." {
		t.Fatalf("e1 %+v", located[0])
	}
	if located[1].Issue.Key != "e2" || chapter[located[1].Start:located[1].End] != "paid him anyway" || located[1].Fix != "End on the coins." {
		t.Fatalf("e2 should be re-anchored by quote: %+v", located[1])
	}
	if skipped[0].Key != "e3" || !strings.Contains(skipped[0].Reason, "no longer") || skipped[1].Key != "e4" || !strings.Contains(skipped[1].Reason, "no fix") {
		t.Fatalf("skipped %+v", skipped)
	}
}

func TestLeadWriterPrompts(t *testing.T) {
	sc := text.Scene{Index: 1, Title: "Later", Text: chapter, Start: 0, End: len(chapter)}
	notes := []locatedIssue{{Issue: sqlcgen.Issue{Key: "e1", Quote: "She did not look back.", Problem: "Redundant."}, Fix: "Cut the sentence."}}
	p := LeadWriterUserPrompt("The Long Winter", "The Gate", "## Characters\n- **Mara**\n", sc, 2, notes)
	for _, want := range []string{`# Story bible of "The Long Winter"`, "**Mara**", "# Chapter: The Gate — scene 2 of 2 (Later)", "return only this scene", ChapterBegin, ChapterEnd,
		`- [e1] passage: "She did not look back." — problem: Redundant. — change to make: Cut the sentence.`, "Markdown only"} {
		if !strings.Contains(p, want) {
			t.Errorf("prompt lacks %q", want)
		}
	}
	sys := LeadWriterSystemPrompt("You are the lead writer.")
	if !strings.Contains(sys, FixedSuffix) || !strings.Contains(sys, "nothing else") {
		t.Fatal("system prompt lacks the suffix or the task")
	}
}
