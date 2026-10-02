package guild

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/google/uuid"

	"writersguild/internal/db/sqlcgen"
	"writersguild/internal/llm"
	"writersguild/internal/runs"
	"writersguild/internal/text"
)

// Event types of a co-write or compare run.
const (
	EventDraftStarted = "draft.started"
	EventDraftDelta   = "draft.delta"
	EventDraftRetry   = "draft.retry"
	EventDraftDone    = "draft.done"
	EventDraftFailed  = "draft.failed"
)

// Draft modes and decisions, matching the drafts table.
const (
	ModeSelection = "selection" // rewrite or replace the selected passage
	ModeContinue  = "continue"  // write on from the cursor

	DraftPending   = "pending"
	DraftInserted  = "inserted"
	DraftReplaced  = "replaced"
	DraftDiscarded = "discarded"
)

// MaxCompareWriters bounds a compare run.
const MaxCompareWriters = 3

// CowriteInput is everything a co-write or compare run needs.
type CowriteInput struct {
	Project         sqlcgen.Project
	Chapter         sqlcgen.Chapter
	Bible           []sqlcgen.BibleEntry
	Writers         []sqlcgen.Writer // one for co-write, two or three for compare
	Instruction     string
	Selection       string // the selected passage; empty means continue from the cursor
	Notes           string // scene notes, optional
	ContextBefore   string // text just before the cursor or the selection
	ContextAfter    string // text just after
	SceneTokenLimit int
}

// Mode reports what the request is based on.
func (in CowriteInput) Mode() string {
	if strings.TrimSpace(in.Selection) != "" {
		return ModeSelection
	}
	return ModeContinue
}

// DraftEvent is the payload of the draft.* events.
type DraftEvent struct {
	DraftID  uuid.UUID `json:"draft_id"`
	WriterID uuid.UUID `json:"writer_id"`
	Slug     string    `json:"slug"`
	Name     string    `json:"name"`
	Text     string    `json:"text,omitempty"`   // draft.delta, draft.done (the whole draft)
	Reason   string    `json:"reason,omitempty"` // draft.retry
	Error    string    `json:"error,omitempty"`  // draft.failed
	Usage    *Usage    `json:"usage,omitempty"`  // draft.done / draft.failed
}

// CowriteResult is stored as the run's result.
type CowriteResult struct {
	Drafts    []uuid.UUID `json:"drafts"`
	Succeeded int         `json:"succeeded"`
	Failed    int         `json:"failed"`
	Mode      string      `json:"mode"`
}

// Cowrite asks one co-writer for a draft, or two or three for drafts to
// compare, in parallel. It returns once the run is recorded.
func (g *Guild) Cowrite(ctx context.Context, user sqlcgen.User, in CowriteInput) (*runs.Run, error) {
	if len(in.Writers) == 0 {
		return nil, errors.New("guild: no co-writer chosen")
	}
	if len(in.Writers) > MaxCompareWriters {
		return nil, fmt.Errorf("guild: at most %d writers can be compared", MaxCompareWriters)
	}
	kind := runs.KindCowrite
	if len(in.Writers) > 1 {
		kind = runs.KindCompare
	}
	ids := make([]uuid.UUID, 0, len(in.Writers))
	names := make([]string, 0, len(in.Writers))
	for _, w := range in.Writers {
		ids = append(ids, w.ID)
		names = append(names, w.Name)
	}
	start := runs.StartParams{
		User: user, Kind: kind,
		Params: map[string]any{
			"writer_ids": ids, "writer_names": names, "mode": in.Mode(), "instruction": in.Instruction,
			"selection_words": text.Words(in.Selection), "content_hash": in.Chapter.ContentHash, "chapter_title": in.Chapter.Title,
		},
		ProjectID: uuid.NullUUID{UUID: in.Project.ID, Valid: true}, ProjectName: in.Project.Name,
		ChapterID: uuid.NullUUID{UUID: in.Chapter.ID, Valid: true},
	}
	return g.engine.Launch(ctx, start, func(ctx context.Context, run *runs.Run, em *runs.Emitter) (any, error) {
		return g.runCowrite(ctx, run, em, in)
	})
}

func (g *Guild) runCowrite(ctx context.Context, run *runs.Run, em *runs.Emitter, in CowriteInput) (any, error) {
	bible := BibleContext(in.Bible)
	rows := make([]sqlcgen.Draft, 0, len(in.Writers))
	for i, w := range in.Writers {
		row, err := g.q.CreateDraft(ctx, sqlcgen.CreateDraftParams{
			UserID: run.Row.UserID, RunID: run.Row.ID, ChapterID: run.Row.ChapterID, WriterID: uuid.NullUUID{UUID: w.ID, Valid: true},
			WriterName: w.Name, WriterSlug: w.Slug, ModelAlias: w.ModelAlias, Mode: in.Mode(), Instruction: in.Instruction,
			Selection: in.Selection, Notes: in.Notes, ContextBefore: in.ContextBefore, ContextAfter: in.ContextAfter, ContentHash: in.Chapter.ContentHash, Position: int32(i),
		})
		if err != nil {
			return nil, fmt.Errorf("record draft: %w", err)
		}
		rows = append(rows, row)
	}
	result := CowriteResult{Mode: in.Mode()}
	var mu sync.Mutex
	var wg sync.WaitGroup
	for i := range in.Writers {
		wg.Add(1)
		go func(w sqlcgen.Writer, row sqlcgen.Draft) {
			defer wg.Done()
			ok := g.cowriter(ctx, run, em, in, bible, w, row)
			mu.Lock()
			result.Drafts = append(result.Drafts, row.ID)
			if ok {
				result.Succeeded++
			} else {
				result.Failed++
			}
			mu.Unlock()
		}(in.Writers[i], rows[i])
	}
	wg.Wait()
	if ctx.Err() != nil {
		return result, ctx.Err()
	}
	if result.Succeeded == 0 {
		return result, errors.New("every writer failed; see the drafts for the reasons")
	}
	return result, nil
}

// cowriter asks one writer for its draft, streaming as it goes, and records
// the outcome. It reports whether a draft was produced.
func (g *Guild) cowriter(ctx context.Context, run *runs.Run, em *runs.Emitter, in CowriteInput, bible string, w sqlcgen.Writer, row sqlcgen.Draft) bool {
	base := DraftEvent{DraftID: row.ID, WriterID: w.ID, Slug: w.Slug, Name: w.Name}
	_ = em.Emit(ctx, EventDraftStarted, base)
	messages := []llm.Message{
		{Role: "system", Content: CowriterSystemPrompt(w.SystemPrompt)},
		{Role: "user", Content: CowriterUserPrompt(in, bible)},
	}
	var usage Usage
	var draft string
	var failErr string
	for attempt := 1; attempt <= 2; attempt++ {
		callCtx, cancel := context.WithTimeout(ctx, g.CallTimeout)
		stream := newDeltaStream(em, func(t string) (string, any) {
			ev := base
			ev.Text = t
			return EventDraftDelta, ev
		})
		req := llm.Request{Model: w.ModelAlias, Messages: messages, Temperature: llm.Float64(w.Temperature)}
		resp, err := g.tracker.Call(callCtx, run, runs.CallOpts{
			WriterID: uuid.NullUUID{UUID: w.ID, Valid: true}, GenerationName: "cowrite:" + w.Slug, OnDelta: stream.delta,
		}, req)
		stream.flush(callCtx)
		cancel()
		if resp != nil {
			usage.addResponse(resp)
		}
		if err != nil {
			switch {
			case ctx.Err() != nil:
				failErr = "the request was cancelled"
			case errors.Is(err, context.DeadlineExceeded):
				failErr = fmt.Sprintf("the writer did not answer within %s", g.CallTimeout)
			default:
				failErr = FriendlyError(err)
			}
			break
		}
		cleaned, verr := ValidateDraft(resp.Content)
		if verr == nil {
			draft = cleaned
			break
		}
		if attempt == 2 {
			failErr = "the writer returned nothing usable twice: " + strings.Join(verr.Problems, "; ")
			break
		}
		retry := base
		retry.Reason = strings.Join(verr.Problems, "; ")
		_ = em.Emit(ctx, EventDraftRetry, retry)
		messages = append(messages,
			llm.Message{Role: "assistant", Content: resp.Content},
			llm.Message{Role: "user", Content: "Your previous reply could not be used: " + strings.Join(verr.Problems, "; ") + ". Reply again with the prose only."},
		)
	}
	status := "succeeded"
	if failErr != "" {
		status = "failed"
		if ctx.Err() != nil {
			status = "cancelled"
		}
	}
	bg := context.WithoutCancel(ctx)
	if _, err := g.q.FinishDraft(bg, sqlcgen.FinishDraftParams{
		ID: row.ID, Status: status, Text: draft, Error: failErr,
		PromptTokens: int32(usage.PromptTokens), CompletionTokens: int32(usage.CompletionTokens), CostUsd: usage.CostUSD, CostEstimated: usage.CostEstimated,
	}); err != nil {
		failErr = "the draft could not be recorded: " + err.Error()
		status = "failed"
	}
	ev := base
	ev.Usage = &usage
	if status == "succeeded" {
		ev.Text = draft
		_ = em.Emit(bg, EventDraftDone, ev)
		return true
	}
	ev.Error = failErr
	_ = em.Emit(bg, EventDraftFailed, ev)
	return false
}

// ValidateDraft cleans a co-writer's reply: fences and surrounding
// whitespace go, as does a short lead-in line such as "Here is the scene:".
// An empty reply is rejected.
func ValidateDraft(reply string) (string, *ValidationError) {
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
	if nl := strings.Index(s, "\n"); nl > 0 && nl < 80 {
		first := strings.TrimSpace(s[:nl])
		rest := strings.TrimSpace(s[nl:])
		if strings.HasSuffix(first, ":") && rest != "" {
			s = rest
		}
	}
	if s == "" {
		return "", &ValidationError{Problems: []string{"the reply is empty"}}
	}
	return s, nil
}

const cowriterRole = `

## Your task in this session
You are drafting as a co-writer of this novel: in your own voice and craft, but in service of the author's story, characters and world as the story bible and the chapter describe them. Return only the prose that was asked for, in Markdown, with no commentary, no title, no quotation marks around it and no code fences.`

// CowriterSystemPrompt is the writer's own prompt plus the fixed suffix and
// the co-writer's task.
func CowriterSystemPrompt(writerPrompt string) string {
	return SystemPrompt(writerPrompt) + cowriterRole
}

// CowriterUserPrompt lays out the bible, the chapter and the request.
func CowriterUserPrompt(in CowriteInput, bible string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Story bible of \"%s\"\n\n%s\n", in.Project.Name, bible)
	fmt.Fprintf(&b, "# Chapter: %s, as it stands\n\n", in.Chapter.Title)
	if in.SceneTokenLimit <= 0 || text.EstimateTokens(in.Chapter.ContentMd) <= in.SceneTokenLimit {
		b.WriteString(ChapterBegin)
		b.WriteString("\n")
		b.WriteString(in.Chapter.ContentMd)
		if !strings.HasSuffix(in.Chapter.ContentMd, "\n") {
			b.WriteString("\n")
		}
		b.WriteString(ChapterEnd)
		b.WriteString("\n\n")
	} else {
		b.WriteString("(The chapter is too long to include whole; the passage around the author's position follows.)\n\n")
	}
	b.WriteString("# The request\n\n")
	if in.Mode() == ModeSelection {
		b.WriteString("The author selected this passage:\n<<<\n")
		b.WriteString(strings.TrimSpace(in.Selection))
		b.WriteString("\n>>>\n")
		if strings.TrimSpace(in.ContextBefore) != "" {
			fmt.Fprintf(&b, "Just before it: %q\n", trimContext(in.ContextBefore, 400, true))
		}
		if strings.TrimSpace(in.ContextAfter) != "" {
			fmt.Fprintf(&b, "Just after it: %q\n", trimContext(in.ContextAfter, 400, false))
		}
	} else {
		b.WriteString("The author's cursor is in the chapter. The text just before the cursor:\n<<<\n")
		b.WriteString(trimContext(in.ContextBefore, 1500, true))
		b.WriteString("\n>>>\nThe text just after the cursor:\n<<<\n")
		b.WriteString(trimContext(in.ContextAfter, 600, false))
		b.WriteString("\n>>>\n")
	}
	if strings.TrimSpace(in.Notes) != "" {
		fmt.Fprintf(&b, "\nScene notes from the author:\n%s\n", strings.TrimSpace(in.Notes))
	}
	fmt.Fprintf(&b, "\nInstruction: %s\n\n", strings.TrimSpace(in.Instruction))
	if in.Mode() == ModeSelection {
		b.WriteString("Return only the text that should take the place of the selected passage.")
	} else {
		b.WriteString("Return only the text to insert at the cursor, so that it reads on from what comes before and into what comes after.")
	}
	return b.String()
}

// trimContext keeps the end (tail) or the start of a context string.
func trimContext(s string, n int, tail bool) string {
	s = strings.TrimSpace(s)
	if len(s) <= n {
		return s
	}
	if tail {
		cut := s[len(s)-n:]
		if i := strings.IndexAny(cut, " \n"); i >= 0 && i < 40 {
			cut = cut[i+1:]
		}
		return "…" + cut
	}
	cut := s[:n]
	if i := strings.LastIndexAny(cut, " \n"); i >= 0 && i > n-40 {
		cut = cut[:i]
	}
	return cut + "…"
}
