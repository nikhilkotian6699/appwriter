package guild

import (
	"strings"
	"testing"

	"writersguild/internal/db/sqlcgen"
)

func TestValidateDraft(t *testing.T) {
	cases := []struct{ in, want string }{
		{"```markdown\nThe lamp burned.\n```", "The lamp burned."},
		{"Here is the scene:\n\nThe lamp burned.", "The lamp burned."},
		{"Sure! Here you go:\nThe lamp burned.\n\nIt went out.", "The lamp burned.\n\nIt went out."},
		{"  The lamp burned.  ", "The lamp burned."},
		{"Not a lead-in: this sentence is prose that happens to contain a colon.\nAnd more.", "Not a lead-in: this sentence is prose that happens to contain a colon.\nAnd more."},
	}
	for _, c := range cases {
		got, err := ValidateDraft(c.in)
		if err != nil || got != c.want {
			t.Errorf("ValidateDraft(%q) = %q, %v; want %q", c.in, got, err, c.want)
		}
	}
	for _, in := range []string{"", "   ", "```\n```"} {
		if _, err := ValidateDraft(in); err == nil {
			t.Errorf("ValidateDraft(%q) should fail", in)
		}
	}
}

func TestCowriterPrompts(t *testing.T) {
	in := CowriteInput{
		Project: sqlcgen.Project{Name: "The Long Winter"}, Chapter: sqlcgen.Chapter{Title: "The Gate", ContentMd: chapter},
		Instruction: "Tighten this passage.", Selection: "She did not look back.", ContextBefore: "reached the gate. ", ContextAfter: " “You’re late,”", Notes: "Keep it cold.", SceneTokenLimit: 6000,
	}
	if in.Mode() != ModeSelection {
		t.Fatal("a selection means selection mode")
	}
	p := CowriterUserPrompt(in, "## Characters\n- **Mara**\n")
	for _, want := range []string{`# Story bible of "The Long Winter"`, "**Mara**", ChapterBegin, "She counted the coins", "The author selected this passage:\n<<<\nShe did not look back.\n>>>", "Just before it:", "Scene notes from the author:\nKeep it cold.", "Instruction: Tighten this passage.", "take the place of the selected passage"} {
		if !strings.Contains(p, want) {
			t.Errorf("selection prompt lacks %q", want)
		}
	}
	in.Selection = ""
	in.ContextBefore = strings.Repeat("before ", 400)
	in.ContextAfter = strings.Repeat("after ", 200)
	if in.Mode() != ModeContinue {
		t.Fatal("no selection means continue mode")
	}
	p = CowriterUserPrompt(in, "")
	if !strings.Contains(p, "The author's cursor is in the chapter") || !strings.Contains(p, "insert at the cursor") || !strings.Contains(p, "…before") || !strings.Contains(p, "after…") {
		t.Errorf("continue prompt wrong:\n%s", p[len(p)-400:])
	}
	if !strings.Contains(CowriterSystemPrompt("You are Hemingway."), FixedSuffix) || !strings.Contains(CowriterSystemPrompt("x"), "co-writer") {
		t.Error("system prompt lacks the suffix or the task")
	}
}
