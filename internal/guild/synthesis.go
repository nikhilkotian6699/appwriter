package guild

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/google/uuid"

	"writersguild/internal/db/sqlcgen"
	"writersguild/internal/runs"
	"writersguild/internal/text"
)

// EditorFormat is the output contract of the editor-in-chief.
const EditorFormat = `Return ONLY a JSON object with exactly this shape and nothing else:
{
  "issues": [
    {
      "id": "<short unique id such as e1>",
      "severity": "high" | "medium" | "low",
      "quote": "<the exact passage concerned, copied character for character from the chapter text or from a critic's quote>",
      "problem": "<the merged problem, one or two sentences>",
      "suggested_fix": "<one concrete fix that reconciles the critics, in the author's voice>",
      "sources": ["<source id>", "<source id>"]
    }
  ]
}
Rules: most important first. Merge notes about the same passage or the same fault into one issue and list EVERY source id it merges. When critics contradict each other, keep the reading closest to the author's evident intent and say so in "problem". Drop notes that would change the story rather than improve the telling. Never add an issue no critic raised: every issue cites at least one of the source ids given, exactly as written. Keep the list short. No Markdown fences, no commentary.`

// IssueSource names a critic's note that a synthesized issue merges.
type IssueSource struct {
	ID         string    `json:"id"`
	CritiqueID uuid.UUID `json:"critique_id"`
	WriterID   uuid.UUID `json:"writer_id"`
	WriterName string    `json:"writer_name"`
	WriterSlug string    `json:"writer_slug"`
	IssueID    string    `json:"issue_id"`
}

// SourceRef is one critic's issue as offered to the editor-in-chief.
type SourceRef struct {
	IssueSource
	Issue Issue
}

// CritiqueSource is one critic's validated output, with who wrote it.
type CritiqueSource struct {
	CritiqueID uuid.UUID
	WriterID   uuid.UUID
	WriterName string
	WriterSlug string
	Critique   *Critique
}

// SynthIssue is one entry of the editor-in-chief's prioritized list.
type SynthIssue struct {
	Issue
	Sources []IssueSource `json:"sources"`
}

// Synthesis is the editor-in-chief's validated reply.
type Synthesis struct {
	Issues   []SynthIssue `json:"issues"`
	Warnings []string     `json:"warnings,omitempty"`
	// Fallback is true when the list was assembled from the critics' notes
	// because the editor-in-chief failed.
	Fallback bool `json:"fallback"`
}

// EditorInput is what BuildEditorInput assembles: the labelled sources and
// the user prompt.
type EditorInput struct {
	Sources map[string]SourceRef
	Order   []string // source ids in prompt order
	Prompt  string
}

const editorRole = `

## Your task in this session
Several critics of the Writers' Guild have read one chapter and returned their notes. You receive the story bible, the chapter text and every note, each note labelled with a source id. Produce the single prioritized list of issues the author should consider.

## Required output
` + EditorFormat

// EditorSystemPrompt is the editor-in-chief's own prompt plus the fixed
// suffix and the task.
func EditorSystemPrompt(editorPrompt string) string {
	return SystemPrompt(editorPrompt) + editorRole
}

// BuildEditorInput lays out the critics' notes for the editor-in-chief. Each
// issue gets a source id "<writer slug>/<issue id>" so the reply can cite it.
// The chapter text is included when it fits within tokenLimit; otherwise the
// editor works from the quotes alone and is told so.
func BuildEditorInput(projectName, chapterTitle, chapterText, bible string, tokenLimit int, critiques []CritiqueSource) EditorInput {
	in := EditorInput{Sources: map[string]SourceRef{}}
	var b strings.Builder
	fmt.Fprintf(&b, "# Story bible of \"%s\"\n\n%s\n", projectName, bible)
	fmt.Fprintf(&b, "# Chapter: %s\n\n", chapterTitle)
	if tokenLimit <= 0 || text.EstimateTokens(chapterText) <= tokenLimit {
		b.WriteString(ChapterBegin)
		b.WriteString("\n")
		b.WriteString(chapterText)
		if !strings.HasSuffix(chapterText, "\n") {
			b.WriteString("\n")
		}
		b.WriteString(ChapterEnd)
		b.WriteString("\n\n")
	} else {
		b.WriteString("(The chapter is too long to include here; work from the critics' quotes, which are copied exactly from it.)\n\n")
	}
	b.WriteString("# The critics' notes\n\n")
	for _, c := range critiques {
		if c.Critique == nil {
			continue
		}
		fmt.Fprintf(&b, "## %s (%d issue%s)\n", c.WriterName, len(c.Critique.Issues), plural(len(c.Critique.Issues)))
		if c.Critique.Overall != "" {
			fmt.Fprintf(&b, "Overall: %s\n", c.Critique.Overall)
		}
		for _, is := range c.Critique.Issues {
			sid := c.WriterSlug + "/" + is.ID
			in.Sources[sid] = SourceRef{
				IssueSource: IssueSource{ID: sid, CritiqueID: c.CritiqueID, WriterID: c.WriterID, WriterName: c.WriterName, WriterSlug: c.WriterSlug, IssueID: is.ID},
				Issue:       is,
			}
			in.Order = append(in.Order, sid)
			fmt.Fprintf(&b, "- [%s] %s — quote: %q — problem: %s", sid, is.Severity, is.Quote, is.Problem)
			if is.SuggestedFix != "" {
				fmt.Fprintf(&b, " — fix: %s", is.SuggestedFix)
			}
			b.WriteString("\n")
		}
		if len(c.Critique.BibleConflicts) > 0 {
			b.WriteString("Story bible conflicts raised:\n")
			for _, bc := range c.Critique.BibleConflicts {
				fmt.Fprintf(&b, "- %q conflicts with %s\n", bc.Quote, bc.ConflictsWith)
			}
		}
		b.WriteString("\n")
	}
	b.WriteString("Merge, reconcile and prioritize these notes now. Return the JSON object only.")
	in.Prompt = b.String()
	return in
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

type rawSynthesis struct {
	Issues []struct {
		ID           string   `json:"id"`
		Severity     string   `json:"severity"`
		Quote        string   `json:"quote"`
		Problem      string   `json:"problem"`
		SuggestedFix string   `json:"suggested_fix"`
		Sources      []string `json:"sources"`
	} `json:"issues"`
}

// ParseSynthesis decodes the editor-in-chief's reply and validates it:
// every issue needs a valid severity, a quote found in the chapter, a problem
// and at least one known source id (fatal, retried once). Unknown source ids
// are dropped with a warning; missing or duplicate ids are replaced.
func ParseSynthesis(reply, chapterText string, in EditorInput) (*Synthesis, error) {
	body, err := ExtractJSON(reply)
	if err != nil {
		return nil, &ValidationError{Problems: []string{err.Error()}}
	}
	var raw rawSynthesis
	if err := json.Unmarshal([]byte(body), &raw); err != nil {
		return nil, &ValidationError{Problems: []string{"the JSON does not parse: " + err.Error()}}
	}
	out := &Synthesis{Issues: []SynthIssue{}}
	var problems []string
	seen := map[string]bool{}
	cited := map[string]bool{}
	for i, ri := range raw.Issues {
		n := i + 1
		is := SynthIssue{Issue: Issue{ID: strings.TrimSpace(ri.ID), Severity: strings.ToLower(strings.TrimSpace(ri.Severity)),
			Quote: strings.TrimSpace(ri.Quote), Problem: strings.TrimSpace(ri.Problem), SuggestedFix: strings.TrimSpace(ri.SuggestedFix)}}
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
		} else {
			problems = append(problems, fmt.Sprintf("issue %d: the quote %q does not appear in the chapter; copy an exact passage", n, truncate(is.Quote, 80)))
		}
		srcSeen := map[string]bool{}
		for _, sid := range ri.Sources {
			sid = strings.TrimSpace(sid)
			ref, ok := in.Sources[sid]
			if !ok {
				// Tolerate a bare writer slug or a bare issue id when it is unambiguous.
				ref, ok = resolveLooseSource(in, sid)
			}
			if !ok {
				out.Warnings = append(out.Warnings, fmt.Sprintf("issue %d cited an unknown source %q, which was dropped", n, sid))
				continue
			}
			if srcSeen[ref.ID] {
				continue
			}
			srcSeen[ref.ID] = true
			cited[ref.ID] = true
			is.Sources = append(is.Sources, ref.IssueSource)
		}
		if len(is.Sources) == 0 {
			problems = append(problems, fmt.Sprintf("issue %d cites no known source id; every issue must cite at least one of the source ids given", n))
		}
		if is.ID == "" || seen[is.ID] {
			is.ID = fmt.Sprintf("e%d", n)
			out.Warnings = append(out.Warnings, fmt.Sprintf("issue %d: id was missing or duplicated and was replaced", n))
		}
		seen[is.ID] = true
		if is.Sources == nil {
			is.Sources = []IssueSource{}
		}
		out.Issues = append(out.Issues, is)
	}
	if len(problems) > 0 {
		return nil, &ValidationError{Problems: problems}
	}
	dropped := 0
	for _, sid := range in.Order {
		if !cited[sid] {
			dropped++
		}
	}
	if dropped > 0 {
		out.Warnings = append(out.Warnings, fmt.Sprintf("the editor-in-chief set aside %d of the critics' %d notes", dropped, len(in.Order)))
	}
	return out, nil
}

// resolveLooseSource accepts "slug" (the writer's only issue) or "i1" (an
// issue id used by exactly one critic).
func resolveLooseSource(in EditorInput, sid string) (SourceRef, bool) {
	var found SourceRef
	matches := 0
	for _, id := range in.Order {
		ref := in.Sources[id]
		if ref.WriterSlug == sid || ref.IssueID == sid || strings.EqualFold(ref.WriterName, sid) {
			found = ref
			matches++
		}
	}
	return found, matches == 1
}

// FallbackSynthesis lists every critic's issues unmerged, most severe first,
// each citing its own critic. It stands in when the editor-in-chief fails.
func FallbackSynthesis(critiques []CritiqueSource, reason string) *Synthesis {
	out := &Synthesis{Issues: []SynthIssue{}, Fallback: true}
	for _, c := range critiques {
		if c.Critique == nil {
			continue
		}
		for _, is := range c.Critique.Issues {
			out.Issues = append(out.Issues, SynthIssue{Issue: is, Sources: []IssueSource{{
				ID: c.WriterSlug + "/" + is.ID, CritiqueID: c.CritiqueID, WriterID: c.WriterID, WriterName: c.WriterName, WriterSlug: c.WriterSlug, IssueID: is.ID,
			}}})
		}
	}
	sort.SliceStable(out.Issues, func(a, b int) bool {
		return severityRank[out.Issues[a].Severity] < severityRank[out.Issues[b].Severity]
	})
	for i := range out.Issues {
		out.Issues[i].ID = fmt.Sprintf("e%d", i+1)
	}
	out.Warnings = append(out.Warnings, "the editor-in-chief could not synthesize the notes ("+reason+"); the critics' issues are listed unmerged, most severe first")
	return out
}

// storeIssues writes the prioritized list and returns the rows.
func (g *Guild) storeIssues(ctx context.Context, run *runs.Run, chapter sqlcgen.Chapter, syn *Synthesis) ([]sqlcgen.Issue, error) {
	rows := make([]sqlcgen.Issue, 0, len(syn.Issues))
	for i, is := range syn.Issues {
		srcJSON, err := json.Marshal(is.Sources)
		if err != nil {
			return nil, err
		}
		row, err := g.q.CreateIssue(ctx, sqlcgen.CreateIssueParams{
			UserID: run.Row.UserID, RunID: run.Row.ID, ChapterID: run.Row.ChapterID, Position: int32(i), Key: is.ID,
			Severity: is.Severity, Quote: is.Quote, Problem: is.Problem, SuggestedFix: is.SuggestedFix,
			QuoteStart: int32(is.Start), QuoteEnd: int32(is.End), QuoteExact: is.QuoteExact, Sources: srcJSON, ContentHash: chapter.ContentHash,
		})
		if err != nil {
			return nil, err
		}
		rows = append(rows, row)
	}
	return rows, nil
}
