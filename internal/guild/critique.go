package guild

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"writersguild/internal/db/sqlcgen"
	"writersguild/internal/llm"
	"writersguild/internal/runs"
	"writersguild/internal/seed"
	"writersguild/internal/text"
)

// Event types of a critique run, on top of run.started and run.finished.
const (
	EventCritiquePlan  = "critique.plan"  // writers and scenes the run will cover
	EventWriterStarted = "writer.started" // a critic began reading
	EventWriterDelta   = "writer.delta"   // a chunk of a critic's streamed reply
	EventWriterRetry   = "writer.retry"   // the reply was invalid; asking once more
	EventWriterDone    = "writer.done"    // the critic's validated critique
	EventWriterFailed  = "writer.failed"  // the critic gave up (gateway error, invalid output twice, timeout)
	EventEditorStarted = "editor.started" // the editor-in-chief began merging the notes
	EventEditorDelta   = "editor.delta"   // a chunk of the editor-in-chief's streamed reply
	EventEditorRetry   = "editor.retry"   // the editor's reply was invalid; asking once more
	EventEditorDone    = "editor.done"    // the prioritized list (fallback=true when assembled without the editor)
)

// Critique statuses, matching the critiques table.
const (
	CritiqueRunning   = "running"
	CritiqueSucceeded = "succeeded"
	CritiqueFailed    = "failed"
	CritiqueCancelled = "cancelled"
)

// CritiqueInput is everything a critique run needs, loaded by the handler.
type CritiqueInput struct {
	Project         sqlcgen.Project
	Chapter         sqlcgen.Chapter
	Writers         []sqlcgen.Writer
	Bible           []sqlcgen.BibleEntry
	SceneTokenLimit int
}

// PlanPayload is the payload of critique.plan.
type PlanPayload struct {
	Writers     []PlanWriter `json:"writers"`
	Scenes      []PlanScene  `json:"scenes"`
	ContentHash string       `json:"content_hash"`
}

// PlanWriter names one attending critic.
type PlanWriter struct {
	CritiqueID uuid.UUID `json:"critique_id"`
	WriterID   uuid.UUID `json:"writer_id"`
	Name       string    `json:"name"`
	Slug       string    `json:"slug"`
	ModelAlias string    `json:"model_alias"`
}

// PlanScene describes one scene of the split chapter.
type PlanScene struct {
	Index int    `json:"index"`
	Title string `json:"title"`
	Start int    `json:"start"`
	End   int    `json:"end"`
}

// WriterEvent is the payload shared by the writer.* events.
type WriterEvent struct {
	CritiqueID uuid.UUID `json:"critique_id"`
	WriterID   uuid.UUID `json:"writer_id"`
	Slug       string    `json:"slug"`
	Scene      int       `json:"scene"`
	Text       string    `json:"text,omitempty"`     // writer.delta
	Reason     string    `json:"reason,omitempty"`   // writer.retry
	Error      string    `json:"error,omitempty"`    // writer.failed
	Critique   *Critique `json:"critique,omitempty"` // writer.done, and writer.failed when partial
	Usage      *Usage    `json:"usage,omitempty"`    // writer.done / writer.failed
}

// Usage sums a critic's calls.
type Usage struct {
	PromptTokens     int     `json:"prompt_tokens"`
	CompletionTokens int     `json:"completion_tokens"`
	CostUSD          float64 `json:"cost_usd"`
	CostEstimated    bool    `json:"cost_estimated"`
}

// CritiqueResult is stored as the run's result.
type CritiqueResult struct {
	Critiques []uuid.UUID `json:"critiques"`
	Succeeded int         `json:"succeeded"`
	Failed    int         `json:"failed"`
	Scenes    int         `json:"scenes"`
	// Issues counts the editor-in-chief's list; Synthesis is "ok", "fallback"
	// (assembled from the critics' notes after the editor failed) or
	// "skipped" (no critic succeeded).
	Issues    int    `json:"issues"`
	Synthesis string `json:"synthesis"`
}

// EditorEvent is the payload of the editor.* events.
type EditorEvent struct {
	WriterID uuid.UUID     `json:"writer_id"`
	Slug     string        `json:"slug"`
	Text     string        `json:"text,omitempty"`     // editor.delta
	Reason   string        `json:"reason,omitempty"`   // editor.retry
	Error    string        `json:"error,omitempty"`    // editor.done with fallback
	Fallback bool          `json:"fallback"`           // editor.done
	Issues   []StoredIssue `json:"issues,omitempty"`   // editor.done
	Warnings []string      `json:"warnings,omitempty"` // editor.done
	Usage    *Usage        `json:"usage,omitempty"`    // editor.done
}

// StoredIssue is an issue row as the events and the API present it.
type StoredIssue struct {
	ID           uuid.UUID     `json:"id"`
	Key          string        `json:"key"`
	Position     int           `json:"position"`
	Severity     string        `json:"severity"`
	Quote        string        `json:"quote"`
	Problem      string        `json:"problem"`
	SuggestedFix string        `json:"suggested_fix"`
	Start        int           `json:"start"`
	End          int           `json:"end"`
	QuoteExact   bool          `json:"quote_exact"`
	Sources      []IssueSource `json:"sources"`
	Decision     string        `json:"decision"`
	EditedFix    *string       `json:"edited_fix,omitempty"`
}

// ToStoredIssue converts a row.
func ToStoredIssue(r sqlcgen.Issue) StoredIssue {
	out := StoredIssue{ID: r.ID, Key: r.Key, Position: int(r.Position), Severity: r.Severity, Quote: r.Quote, Problem: r.Problem, SuggestedFix: r.SuggestedFix,
		Start: int(r.QuoteStart), End: int(r.QuoteEnd), QuoteExact: r.QuoteExact, Sources: []IssueSource{}, Decision: r.Decision, EditedFix: r.EditedFix}
	_ = json.Unmarshal(r.Sources, &out.Sources)
	if out.Sources == nil {
		out.Sources = []IssueSource{}
	}
	return out
}

// Critique convenes the chosen critics on a chapter. It returns as soon as
// the run is recorded; the writers work in the background and the browser
// follows them through the run's events.
func (g *Guild) Critique(ctx context.Context, user sqlcgen.User, in CritiqueInput) (*runs.Run, error) {
	if len(in.Writers) == 0 {
		return nil, errors.New("guild: no critics chosen")
	}
	names := make([]string, 0, len(in.Writers))
	ids := make([]uuid.UUID, 0, len(in.Writers))
	for _, w := range in.Writers {
		names = append(names, w.Name)
		ids = append(ids, w.ID)
	}
	params := map[string]any{
		"writer_ids": ids, "writer_names": names, "content_hash": in.Chapter.ContentHash, "chapter_title": in.Chapter.Title,
	}
	start := runs.StartParams{
		User: user, Kind: runs.KindCritique, Params: params,
		ProjectID: uuid.NullUUID{UUID: in.Project.ID, Valid: true}, ProjectName: in.Project.Name,
		ChapterID: uuid.NullUUID{UUID: in.Chapter.ID, Valid: true},
	}
	return g.engine.Launch(ctx, start, func(ctx context.Context, run *runs.Run, em *runs.Emitter) (any, error) {
		return g.runCritique(ctx, run, em, in)
	})
}

func (g *Guild) runCritique(ctx context.Context, run *runs.Run, em *runs.Emitter, in CritiqueInput) (any, error) {
	scenes := text.SplitScenes(in.Chapter.ContentMd, in.SceneTokenLimit)
	bible := BibleContext(in.Bible)

	plan := PlanPayload{ContentHash: in.Chapter.ContentHash}
	for _, sc := range scenes {
		plan.Scenes = append(plan.Scenes, PlanScene{Index: sc.Index, Title: sc.Title, Start: sc.Start, End: sc.End})
	}
	rows := make([]sqlcgen.Critique, 0, len(in.Writers))
	for _, w := range in.Writers {
		row, err := g.q.CreateCritique(ctx, sqlcgen.CreateCritiqueParams{
			UserID: run.Row.UserID, RunID: run.Row.ID, ChapterID: run.Row.ChapterID,
			WriterID: uuid.NullUUID{UUID: w.ID, Valid: true}, WriterName: w.Name, WriterSlug: w.Slug, ModelAlias: w.ModelAlias,
			SceneCount: int32(len(scenes)),
		})
		if err != nil {
			return nil, fmt.Errorf("record critique: %w", err)
		}
		rows = append(rows, row)
		plan.Writers = append(plan.Writers, PlanWriter{CritiqueID: row.ID, WriterID: w.ID, Name: w.Name, Slug: w.Slug, ModelAlias: w.ModelAlias})
	}
	if err := em.Emit(ctx, EventCritiquePlan, plan); err != nil {
		return nil, err
	}

	result := CritiqueResult{Scenes: len(scenes), Synthesis: "skipped"}
	sources := make([]CritiqueSource, len(in.Writers))
	var mu sync.Mutex
	var wg sync.WaitGroup
	for i := range in.Writers {
		wg.Add(1)
		go func(i int, w sqlcgen.Writer, row sqlcgen.Critique) {
			defer wg.Done()
			c, ok := g.critic(ctx, run, em, in, scenes, bible, w, row)
			mu.Lock()
			result.Critiques = append(result.Critiques, row.ID)
			if ok {
				result.Succeeded++
				sources[i] = CritiqueSource{CritiqueID: row.ID, WriterID: w.ID, WriterName: w.Name, WriterSlug: w.Slug, Critique: c}
			} else {
				result.Failed++
			}
			mu.Unlock()
		}(i, in.Writers[i], rows[i])
	}
	wg.Wait()
	if ctx.Err() != nil {
		return result, ctx.Err()
	}
	if result.Succeeded == 0 {
		return result, errors.New("every writer failed; see the writers' panels for the reasons")
	}
	var ok []CritiqueSource
	for _, s := range sources {
		if s.Critique != nil {
			ok = append(ok, s)
		}
	}
	syn, err := g.synthesize(ctx, run, em, in, bible, ok)
	if err != nil {
		return result, err
	}
	result.Issues = len(syn.Issues)
	result.Synthesis = "ok"
	if syn.Fallback {
		result.Synthesis = "fallback"
	}
	return result, nil
}

// synthesize runs the editor-in-chief over the critics' notes, stores the
// prioritized list and emits the editor.* events. When the editor fails for
// any reason the list is assembled from the notes instead, so the author
// always has something to decide on.
func (g *Guild) synthesize(ctx context.Context, run *runs.Run, em *runs.Emitter, in CritiqueInput, bible string, critiques []CritiqueSource) (*Synthesis, error) {
	editor, err := g.q.GetWriterBySlug(ctx, sqlcgen.GetWriterBySlugParams{UserID: run.Row.UserID, Slug: seed.SlugEditorInChief})
	base := EditorEvent{Slug: seed.SlugEditorInChief}
	var syn *Synthesis
	var usage Usage
	if err != nil {
		syn = FallbackSynthesis(critiques, "no editor-in-chief writer exists in this workspace")
	} else {
		base.WriterID = editor.ID
		_ = em.Emit(ctx, EventEditorStarted, base)
		var ferr error
		syn, usage, ferr = g.editorCall(ctx, run, em, in, bible, editor, base, critiques)
		if ferr != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			syn = FallbackSynthesis(critiques, ferr.Error())
			base.Error = ferr.Error()
		}
	}
	bg := context.WithoutCancel(ctx)
	rows, err := g.storeIssues(bg, run, in.Chapter, syn)
	if err != nil {
		return nil, fmt.Errorf("store issues: %w", err)
	}
	ev := base
	ev.Fallback = syn.Fallback
	ev.Warnings = syn.Warnings
	ev.Usage = &usage
	ev.Issues = make([]StoredIssue, 0, len(rows))
	for _, r := range rows {
		ev.Issues = append(ev.Issues, ToStoredIssue(r))
	}
	_ = em.Emit(bg, EventEditorDone, ev)
	return syn, nil
}

// editorCall asks the editor-in-chief once, retrying once on invalid output.
func (g *Guild) editorCall(ctx context.Context, run *runs.Run, em *runs.Emitter, in CritiqueInput, bible string, editor sqlcgen.Writer, base EditorEvent, critiques []CritiqueSource) (*Synthesis, Usage, error) {
	var usage Usage
	input := BuildEditorInput(in.Project.Name, in.Chapter.Title, in.Chapter.ContentMd, bible, in.SceneTokenLimit, critiques)
	messages := []llm.Message{
		{Role: "system", Content: EditorSystemPrompt(editor.SystemPrompt)},
		{Role: "user", Content: input.Prompt},
	}
	for attempt := 1; attempt <= 2; attempt++ {
		callCtx, cancel := context.WithTimeout(ctx, g.CallTimeout)
		stream := newDeltaStream(em, func(text string) (string, any) {
			ev := base
			ev.Text = text
			return EventEditorDelta, ev
		})
		req := llm.Request{Model: editor.ModelAlias, Messages: messages, Temperature: llm.Float64(editor.Temperature), JSONMode: true}
		resp, err := g.tracker.Call(callCtx, run, runs.CallOpts{
			WriterID: uuid.NullUUID{UUID: editor.ID, Valid: true}, GenerationName: "editor-in-chief", OnDelta: stream.delta,
		}, req)
		stream.flush(callCtx)
		cancel()
		if resp != nil {
			usage.addResponse(resp)
		}
		if err != nil {
			if ctx.Err() != nil {
				return nil, usage, ctx.Err()
			}
			if errors.Is(err, context.DeadlineExceeded) {
				return nil, usage, fmt.Errorf("the editor-in-chief did not answer within %s", g.CallTimeout)
			}
			return nil, usage, errors.New(FriendlyError(err))
		}
		syn, perr := ParseSynthesis(resp.Content, in.Chapter.ContentMd, input)
		if perr == nil {
			return syn, usage, nil
		}
		var verr *ValidationError
		if !errors.As(perr, &verr) {
			return nil, usage, perr
		}
		if attempt == 2 {
			return nil, usage, fmt.Errorf("the editor-in-chief returned invalid output twice: %s", strings.Join(verr.Problems, "; "))
		}
		retry := base
		retry.Reason = strings.Join(verr.Problems, "; ")
		_ = em.Emit(ctx, EventEditorRetry, retry)
		messages = append(messages,
			llm.Message{Role: "assistant", Content: resp.Content},
			llm.Message{Role: "user", Content: RetryPrompt(verr)},
		)
	}
	return nil, usage, errors.New("unreachable")
}

// critic runs one writer over every scene, streaming as it goes. It returns
// the merged critique and whether the writer produced a usable one.
func (g *Guild) critic(ctx context.Context, run *runs.Run, em *runs.Emitter, in CritiqueInput, scenes []text.Scene, bible string, w sqlcgen.Writer, row sqlcgen.Critique) (*Critique, bool) {
	base := WriterEvent{CritiqueID: row.ID, WriterID: w.ID, Slug: w.Slug}
	_ = em.Emit(ctx, EventWriterStarted, base)

	merged := &Critique{Writer: w.Name, Issues: []Issue{}, BibleConflicts: []BibleConflict{}}
	var raw strings.Builder
	var usage Usage
	var overalls []string
	var failErr string
	for _, sc := range scenes {
		prefix := ""
		if len(scenes) > 1 {
			prefix = fmt.Sprintf("s%d", sc.Index+1)
		}
		c, reply, u, err := g.criticScene(ctx, run, em, in, bible, w, base, sc, len(scenes), prefix)
		usage.add(u)
		if raw.Len() > 0 {
			raw.WriteString("\n\n")
		}
		if len(scenes) > 1 {
			fmt.Fprintf(&raw, "### Scene %d\n", sc.Index+1)
		}
		raw.WriteString(reply)
		if err != nil {
			failErr = err.Error()
			break
		}
		for _, is := range c.Issues {
			is.Start += sc.Start
			is.End += sc.Start
			merged.Issues = append(merged.Issues, is)
		}
		for _, bc := range c.BibleConflicts {
			bc.Start += sc.Start
			bc.End += sc.Start
			merged.BibleConflicts = append(merged.BibleConflicts, bc)
		}
		if c.Overall != "" {
			if len(scenes) > 1 {
				overalls = append(overalls, fmt.Sprintf("Scene %d: %s", sc.Index+1, c.Overall))
			} else {
				overalls = append(overalls, c.Overall)
			}
		}
		merged.Warnings = append(merged.Warnings, c.Warnings...)
	}
	merged.Overall = strings.Join(overalls, " ")

	status := CritiqueSucceeded
	if failErr != "" {
		status = CritiqueFailed
		if ctx.Err() != nil {
			status = CritiqueCancelled
		}
	}
	var critiqueJSON []byte
	if failErr == "" || len(merged.Issues) > 0 || merged.Overall != "" {
		critiqueJSON, _ = json.Marshal(merged)
	}
	bg := context.WithoutCancel(ctx)
	if _, err := g.q.FinishCritique(bg, sqlcgen.FinishCritiqueParams{
		ID: row.ID, Status: status, RawText: raw.String(), Critique: critiqueJSON, Error: failErr,
		PromptTokens: int32(usage.PromptTokens), CompletionTokens: int32(usage.CompletionTokens), CostUsd: usage.CostUSD, CostEstimated: usage.CostEstimated,
	}); err != nil {
		failErr = "the critique could not be recorded: " + err.Error()
		status = CritiqueFailed
	}
	ev := base
	ev.Usage = &usage
	if status == CritiqueSucceeded {
		ev.Critique = merged
		_ = em.Emit(bg, EventWriterDone, ev)
		return merged, true
	}
	ev.Error = failErr
	if critiqueJSON != nil {
		ev.Critique = merged
	}
	_ = em.Emit(bg, EventWriterFailed, ev)
	return nil, false
}

// criticScene asks one writer about one scene, validating the reply and
// retrying once on invalid output. It returns the critique with offsets
// relative to the scene, the raw reply text, and the usage of all calls.
func (g *Guild) criticScene(ctx context.Context, run *runs.Run, em *runs.Emitter, in CritiqueInput, bible string, w sqlcgen.Writer, base WriterEvent, sc text.Scene, sceneCount int, prefix string) (*Critique, string, Usage, error) {
	var usage Usage
	messages := []llm.Message{
		{Role: "system", Content: CriticSystemPrompt(w.SystemPrompt)},
		{Role: "user", Content: CriticUserPrompt(in.Project.Name, in.Chapter.Title, bible, sc.Index, sceneCount, sc.Title, sc.Text)},
	}
	var transcript strings.Builder
	for attempt := 1; attempt <= 2; attempt++ {
		callCtx, cancel := context.WithTimeout(ctx, g.CallTimeout)
		stream := newDeltaStream(em, func(text string) (string, any) {
			ev := base
			ev.Scene = sc.Index
			ev.Text = text
			return EventWriterDelta, ev
		})
		req := llm.Request{Model: w.ModelAlias, Messages: messages, Temperature: llm.Float64(w.Temperature), JSONMode: true}
		resp, err := g.tracker.Call(callCtx, run, runs.CallOpts{
			WriterID: uuid.NullUUID{UUID: w.ID, Valid: true}, GenerationName: "critic:" + w.Slug, OnDelta: stream.delta,
		}, req)
		stream.flush(callCtx)
		cancel()
		if resp != nil {
			usage.addResponse(resp)
		}
		if err != nil {
			if ctx.Err() != nil {
				return nil, transcript.String(), usage, ctx.Err()
			}
			if errors.Is(err, context.DeadlineExceeded) {
				return nil, transcript.String(), usage, fmt.Errorf("the writer did not answer within %s", g.CallTimeout)
			}
			return nil, transcript.String(), usage, errors.New(FriendlyError(err))
		}
		if transcript.Len() > 0 {
			transcript.WriteString("\n\n")
		}
		transcript.WriteString(resp.Content)
		c, perr := ParseCritique(resp.Content, sc.Text, w.Name, prefix)
		if perr == nil {
			return c, transcript.String(), usage, nil
		}
		var verr *ValidationError
		if !errors.As(perr, &verr) {
			return nil, transcript.String(), usage, perr
		}
		if attempt == 2 {
			return nil, transcript.String(), usage, fmt.Errorf("the writer returned invalid output twice: %s", strings.Join(verr.Problems, "; "))
		}
		retry := base
		retry.Scene = sc.Index
		retry.Reason = strings.Join(verr.Problems, "; ")
		_ = em.Emit(ctx, EventWriterRetry, retry)
		messages = append(messages,
			llm.Message{Role: "assistant", Content: resp.Content},
			llm.Message{Role: "user", Content: RetryPrompt(verr)},
		)
	}
	return nil, transcript.String(), usage, errors.New("unreachable")
}

func (u *Usage) add(o Usage) {
	u.PromptTokens += o.PromptTokens
	u.CompletionTokens += o.CompletionTokens
	u.CostUSD += o.CostUSD
	u.CostEstimated = u.CostEstimated || o.CostEstimated
}

func (u *Usage) addResponse(r *llm.Response) {
	u.PromptTokens += r.Usage.PromptTokens
	u.CompletionTokens += r.Usage.CompletionTokens
	if r.CostKnown {
		u.CostUSD += r.CostUSD
		u.CostEstimated = u.CostEstimated || r.CostEstimated
	} else if r.Usage.PromptTokens+r.Usage.CompletionTokens > 0 {
		u.CostEstimated = true
	}
}

// deltaStream coalesces streamed fragments into delta events so a long reply
// does not become hundreds of stored rows. build turns the buffered text into
// the event type and payload to emit.
type deltaStream struct {
	em    *runs.Emitter
	build func(text string) (string, any)
	buf   strings.Builder
	last  time.Time
}

const (
	deltaFlushBytes = 160
	deltaFlushEvery = 200 * time.Millisecond
)

func newDeltaStream(em *runs.Emitter, build func(text string) (string, any)) *deltaStream {
	return &deltaStream{em: em, build: build, last: time.Now()}
}

func (d *deltaStream) delta(s string) {
	d.buf.WriteString(s)
	if d.buf.Len() >= deltaFlushBytes || time.Since(d.last) >= deltaFlushEvery {
		d.flush(context.Background())
	}
}

func (d *deltaStream) flush(ctx context.Context) {
	if d.buf.Len() == 0 {
		return
	}
	typ, ev := d.build(d.buf.String())
	d.buf.Reset()
	d.last = time.Now()
	_ = d.em.Emit(ctx, typ, ev)
}
