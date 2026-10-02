package guild

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"

	"writersguild/internal/db/sqlcgen"
	"writersguild/internal/llm"
	"writersguild/internal/runs"
	"writersguild/internal/seed"
	"writersguild/internal/text"
)

// Event types of a revision run.
const (
	EventRevisionStarted = "revision.started" // the lead writer began; payload says how many scenes and notes
	EventRevisionDelta   = "revision.delta"   // a chunk of the streamed revised text
	EventRevisionRetry   = "revision.retry"   // the reply failed validation; asking once more
	EventRevisionDone    = "revision.done"    // the proposed revision is stored
)

// Revision statuses, matching the revisions table.
const (
	RevisionProposed  = "proposed"
	RevisionApplied   = "applied"
	RevisionDiscarded = "discarded"
)

// RevisionInput is everything a revision run needs, loaded by the handler.
type RevisionInput struct {
	Project         sqlcgen.Project
	Chapter         sqlcgen.Chapter
	Bible           []sqlcgen.BibleEntry
	CritiqueRunID   uuid.UUID
	Issues          []sqlcgen.Issue // the accepted issues
	SceneTokenLimit int
}

// SkippedIssue is an accepted issue the revision could not apply.
type SkippedIssue struct {
	IssueID uuid.UUID `json:"issue_id"`
	Key     string    `json:"key"`
	Quote   string    `json:"quote"`
	Reason  string    `json:"reason"`
}

// RevisionEvent is the payload of the revision.* events.
type RevisionEvent struct {
	WriterID   uuid.UUID       `json:"writer_id"`
	Slug       string          `json:"slug"`
	Scene      int             `json:"scene"`
	Scenes     int             `json:"scenes,omitempty"`      // revision.started
	Notes      int             `json:"notes,omitempty"`       // revision.started
	Text       string          `json:"text,omitempty"`        // revision.delta
	Reason     string          `json:"reason,omitempty"`      // revision.retry
	RevisionID uuid.UUID       `json:"revision_id,omitempty"` // revision.done
	Stats      *text.DiffStats `json:"stats,omitempty"`       // revision.done
	Skipped    []SkippedIssue  `json:"skipped,omitempty"`     // revision.done
	Warnings   []string        `json:"warnings,omitempty"`    // revision.done
	Usage      *Usage          `json:"usage,omitempty"`       // revision.done
}

// RevisionResult is stored as the run's result.
type RevisionResult struct {
	RevisionID   uuid.UUID      `json:"revision_id"`
	Hunks        int            `json:"hunks"`
	WordsAdded   int            `json:"words_added"`
	WordsRemoved int            `json:"words_removed"`
	Applied      int            `json:"applied_notes"`
	Skipped      []SkippedIssue `json:"skipped"`
}

// Revise asks the lead writer to apply the accepted issues and stores the
// result as a proposed revision with word-level hunks. It returns once the
// run is recorded; the work happens in the background.
func (g *Guild) Revise(ctx context.Context, user sqlcgen.User, in RevisionInput) (*runs.Run, error) {
	if len(in.Issues) == 0 {
		return nil, errors.New("guild: no accepted issues to apply")
	}
	ids := make([]uuid.UUID, 0, len(in.Issues))
	for _, is := range in.Issues {
		ids = append(ids, is.ID)
	}
	start := runs.StartParams{
		User: user, Kind: runs.KindRevision,
		Params:    map[string]any{"critique_run_id": in.CritiqueRunID, "issue_ids": ids, "content_hash": in.Chapter.ContentHash, "chapter_title": in.Chapter.Title},
		ProjectID: uuid.NullUUID{UUID: in.Project.ID, Valid: true}, ProjectName: in.Project.Name,
		ChapterID: uuid.NullUUID{UUID: in.Chapter.ID, Valid: true},
	}
	return g.engine.Launch(ctx, start, func(ctx context.Context, run *runs.Run, em *runs.Emitter) (any, error) {
		return g.runRevise(ctx, run, em, in)
	})
}

// locatedIssue is an accepted issue anchored in the chapter as it stands now.
type locatedIssue struct {
	Issue sqlcgen.Issue
	Start int
	End   int
	Fix   string
}

// FinalFix is the wording the revision applies: the author's edit when
// there is one, else the editor-in-chief's suggestion.
func FinalFix(is sqlcgen.Issue) string {
	if is.EditedFix != nil && strings.TrimSpace(*is.EditedFix) != "" {
		return strings.TrimSpace(*is.EditedFix)
	}
	return is.SuggestedFix
}

// locateIssues anchors the accepted issues in the current text: by stored
// offsets when the chapter has not changed since the critique, else by
// finding the quote again. Unfound quotes are skipped with a reason.
func locateIssues(chapter sqlcgen.Chapter, issues []sqlcgen.Issue) ([]locatedIssue, []SkippedIssue) {
	var located []locatedIssue
	var skipped []SkippedIssue
	for _, is := range issues {
		fix := FinalFix(is)
		if strings.TrimSpace(fix) == "" {
			skipped = append(skipped, SkippedIssue{IssueID: is.ID, Key: is.Key, Quote: is.Quote, Reason: "the issue has no fix to apply"})
			continue
		}
		if is.ContentHash == chapter.ContentHash && int(is.QuoteEnd) <= len(chapter.ContentMd) && chapter.ContentMd[is.QuoteStart:is.QuoteEnd] == is.Quote {
			located = append(located, locatedIssue{Issue: is, Start: int(is.QuoteStart), End: int(is.QuoteEnd), Fix: fix})
			continue
		}
		m, ok := text.FindQuote(chapter.ContentMd, is.Quote)
		if !ok {
			skipped = append(skipped, SkippedIssue{IssueID: is.ID, Key: is.Key, Quote: is.Quote, Reason: "the quoted passage is no longer in the chapter"})
			continue
		}
		located = append(located, locatedIssue{Issue: is, Start: m.Start, End: m.End, Fix: fix})
	}
	return located, skipped
}

func (g *Guild) runRevise(ctx context.Context, run *runs.Run, em *runs.Emitter, in RevisionInput) (any, error) {
	lead, err := g.q.GetWriterBySlug(ctx, sqlcgen.GetWriterBySlugParams{UserID: run.Row.UserID, Slug: seed.SlugLeadWriter})
	if err != nil {
		return nil, errors.New("no lead-writer writer exists in this workspace; it is one of the system agents on the Writers page")
	}
	located, skipped := locateIssues(in.Chapter, in.Issues)
	if len(located) == 0 {
		return nil, errors.New("none of the accepted issues could be located in the chapter as it stands now")
	}
	original := in.Chapter.ContentMd
	scenes := text.SplitScenes(original, in.SceneTokenLimit)
	bible := BibleContext(in.Bible)
	base := RevisionEvent{WriterID: lead.ID, Slug: lead.Slug}
	started := base
	started.Scenes = len(scenes)
	started.Notes = len(located)
	_ = em.Emit(ctx, EventRevisionStarted, started)

	var usage Usage
	var warnings []string
	var revised strings.Builder
	for _, sc := range scenes {
		var mine []locatedIssue
		for _, li := range located {
			if li.Start >= sc.Start && li.Start < sc.End {
				mine = append(mine, li)
			}
		}
		if len(mine) == 0 {
			revised.WriteString(sc.Text)
			continue
		}
		out, u, err := g.leadWriterScene(ctx, run, em, in, bible, lead, base, sc, len(scenes), mine)
		usage.add(u)
		if err != nil {
			return nil, err
		}
		if out == sc.Text {
			warnings = append(warnings, fmt.Sprintf("the lead writer returned scene %d unchanged", sc.Index+1))
		}
		revised.WriteString(out)
	}
	revisedMD := revised.String()
	hunks := text.Diff(original, revisedMD)
	stats := text.Stats(hunks)
	if len(hunks) == 0 {
		warnings = append(warnings, "the lead writer changed nothing")
	}
	hunksJSON, _ := json.Marshal(hunks)
	statsJSON, _ := json.Marshal(stats)
	ids := make([]uuid.UUID, 0, len(located))
	for _, li := range located {
		ids = append(ids, li.Issue.ID)
	}
	idsJSON, _ := json.Marshal(ids)
	if skipped == nil {
		skipped = []SkippedIssue{}
	}
	skippedJSON, _ := json.Marshal(skipped)
	bg := context.WithoutCancel(ctx)
	row, err := g.q.CreateRevision(bg, sqlcgen.CreateRevisionParams{
		UserID: run.Row.UserID, RunID: run.Row.ID, ChapterID: in.Chapter.ID, CritiqueRunID: uuid.NullUUID{UUID: in.CritiqueRunID, Valid: true},
		BaseHash: in.Chapter.ContentHash, BaseContentMd: original, RevisedMd: revisedMD, Hunks: hunksJSON, Stats: statsJSON, IssueIds: idsJSON, Skipped: skippedJSON,
	})
	if err != nil {
		return nil, fmt.Errorf("store revision: %w", err)
	}
	done := base
	done.RevisionID = row.ID
	done.Stats = &stats
	done.Skipped = skipped
	done.Warnings = warnings
	done.Usage = &usage
	_ = em.Emit(bg, EventRevisionDone, done)
	return RevisionResult{RevisionID: row.ID, Hunks: len(hunks), WordsAdded: stats.WordsAdded, WordsRemoved: stats.WordsRemoved, Applied: len(located), Skipped: skipped}, nil
}

// leadWriterScene asks the lead writer to apply the notes of one scene,
// retrying once when the reply fails validation.
func (g *Guild) leadWriterScene(ctx context.Context, run *runs.Run, em *runs.Emitter, in RevisionInput, bible string, lead sqlcgen.Writer, base RevisionEvent, sc text.Scene, sceneCount int, notes []locatedIssue) (string, Usage, error) {
	var usage Usage
	messages := []llm.Message{
		{Role: "system", Content: LeadWriterSystemPrompt(lead.SystemPrompt)},
		{Role: "user", Content: LeadWriterUserPrompt(in.Project.Name, in.Chapter.Title, bible, sc, sceneCount, notes)},
	}
	for attempt := 1; attempt <= 2; attempt++ {
		callCtx, cancel := context.WithTimeout(ctx, g.CallTimeout)
		stream := newDeltaStream(em, func(t string) (string, any) {
			ev := base
			ev.Scene = sc.Index
			ev.Text = t
			return EventRevisionDelta, ev
		})
		req := llm.Request{Model: lead.ModelAlias, Messages: messages, Temperature: llm.Float64(lead.Temperature)}
		resp, err := g.tracker.Call(callCtx, run, runs.CallOpts{
			WriterID: uuid.NullUUID{UUID: lead.ID, Valid: true}, GenerationName: "lead-writer", OnDelta: stream.delta,
		}, req)
		stream.flush(callCtx)
		cancel()
		if resp != nil {
			usage.addResponse(resp)
		}
		if err != nil {
			if ctx.Err() != nil {
				return "", usage, ctx.Err()
			}
			if errors.Is(err, context.DeadlineExceeded) {
				return "", usage, fmt.Errorf("the lead writer did not answer within %s", g.CallTimeout)
			}
			return "", usage, errors.New(FriendlyError(err))
		}
		revised, verr := ValidateRevision(sc.Text, resp.Content)
		if verr == nil {
			return revised, usage, nil
		}
		if attempt == 2 {
			return "", usage, fmt.Errorf("the lead writer returned unusable text twice: %s", strings.Join(verr.Problems, "; "))
		}
		retry := base
		retry.Scene = sc.Index
		retry.Reason = strings.Join(verr.Problems, "; ")
		_ = em.Emit(ctx, EventRevisionRetry, retry)
		messages = append(messages,
			llm.Message{Role: "assistant", Content: resp.Content},
			llm.Message{Role: "user", Content: "Your previous reply could not be used: " + strings.Join(verr.Problems, "; ") + ". Reply again with the complete revised text only, in Markdown, applying only the notes listed."},
		)
	}
	return "", usage, errors.New("unreachable")
}

// ValidateRevision cleans the lead writer's reply (fences, surrounding
// whitespace) and rejects replies that are empty or wildly different in
// length from the text they revise, which signals commentary or a summary
// instead of the chapter.
func ValidateRevision(original, reply string) (string, *ValidationError) {
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
	if s == "" {
		return "", &ValidationError{Problems: []string{"the reply is empty"}}
	}
	ow, nw := text.Words(original), text.Words(s)
	if ow >= 20 {
		if nw*2 < ow {
			return "", &ValidationError{Problems: []string{fmt.Sprintf("the reply has %d words where the text had %d; return the complete revised text, not a summary", nw, ow)}}
		}
		if nw > ow*2 {
			return "", &ValidationError{Problems: []string{fmt.Sprintf("the reply has %d words where the text had %d; apply only the listed notes without adding new material", nw, ow)}}
		}
	}
	// Keep the original's surrounding whitespace exactly, so scenes join as
	// they did and a dropped blank line does not count as a change.
	lead := original[:len(original)-len(strings.TrimLeft(original, " \t\r\n"))]
	trail := original[len(strings.TrimRight(original, " \t\r\n")):]
	return lead + s + trail, nil
}

const leadWriterRole = `

## Your task in this session
You are applying the editorial notes the author accepted on one chapter (or one scene of it). Make exactly the changes the notes ask for, where they ask for them, and nothing else: every other sentence stays as written, in the author's voice, rhythm, vocabulary, punctuation, formatting and paragraph breaks. Return the complete revised text in Markdown, with no commentary, no title that was not there and no code fences.`

// LeadWriterSystemPrompt is the lead writer's own prompt plus the fixed
// suffix and the task.
func LeadWriterSystemPrompt(writerPrompt string) string {
	return SystemPrompt(writerPrompt) + leadWriterRole
}

// LeadWriterUserPrompt lays out the bible, the text and the notes to apply.
func LeadWriterUserPrompt(projectName, chapterTitle, bible string, sc text.Scene, sceneCount int, notes []locatedIssue) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Story bible of \"%s\"\n\n%s\n", projectName, bible)
	fmt.Fprintf(&b, "# Chapter: %s", chapterTitle)
	if sceneCount > 1 {
		fmt.Fprintf(&b, " — scene %d of %d", sc.Index+1, sceneCount)
		if sc.Title != "" {
			fmt.Fprintf(&b, " (%s)", sc.Title)
		}
		b.WriteString("\nOnly this scene is given; return only this scene, revised.")
	}
	b.WriteString("\n\n")
	b.WriteString(ChapterBegin)
	b.WriteString("\n")
	b.WriteString(sc.Text)
	if !strings.HasSuffix(sc.Text, "\n") {
		b.WriteString("\n")
	}
	b.WriteString(ChapterEnd)
	b.WriteString("\n\n# Accepted notes to apply (and nothing else)\n")
	for _, n := range notes {
		fmt.Fprintf(&b, "- [%s] passage: %q — problem: %s — change to make: %s\n", n.Issue.Key, n.Issue.Quote, n.Issue.Problem, n.Fix)
	}
	b.WriteString("\nReturn the complete revised text now, Markdown only.")
	return b.String()
}
