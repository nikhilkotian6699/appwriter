package guild

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/google/uuid"

	"writersguild/internal/db/sqlcgen"
	"writersguild/internal/llm"
	"writersguild/internal/runs"
	"writersguild/internal/seed"
	"writersguild/internal/text"
)

// Event types of a bible update run.
const (
	EventBibleStarted = "bible.started"
	EventBibleDelta   = "bible.delta"
	EventBibleRetry   = "bible.retry"
	EventBibleDone    = "bible.done" // the stored proposals
)

// Proposal actions and statuses, matching the bible_proposals table.
const (
	ProposalAdd    = "add"
	ProposalUpdate = "update"
	ProposalDelete = "delete"

	ProposalPending  = "pending"
	ProposalApproved = "approved"
	ProposalRejected = "rejected"
)

// BibleSections lists the valid sections.
var BibleSections = map[string]bool{"premise": true, "character": true, "setting": true, "timeline": true, "style": true, "chapter_summary": true}

// BibleUpdateInput is everything a bible update run needs.
type BibleUpdateInput struct {
	Project         sqlcgen.Project
	Chapter         sqlcgen.Chapter // as it stands after the revision
	Bible           []sqlcgen.BibleEntry
	Revision        *sqlcgen.Revision // the applied revision, when the run follows one
	SceneTokenLimit int
}

// Proposal is one validated change to the story bible.
type Proposal struct {
	Action    string            `json:"action"`
	EntryID   *uuid.UUID        `json:"entry_id,omitempty"`
	Section   string            `json:"section"`
	Title     string            `json:"title"`
	Fields    map[string]string `json:"fields"`
	Rationale string            `json:"rationale"`
}

// ProposalSet is the bible keeper's validated reply.
type ProposalSet struct {
	Proposals []Proposal `json:"proposals"`
	Warnings  []string   `json:"warnings,omitempty"`
}

// BibleEvent is the payload of the bible.* events.
type BibleEvent struct {
	WriterID  uuid.UUID        `json:"writer_id"`
	Slug      string           `json:"slug"`
	Text      string           `json:"text,omitempty"`
	Reason    string           `json:"reason,omitempty"`
	Proposals []StoredProposal `json:"proposals,omitempty"`
	Warnings  []string         `json:"warnings,omitempty"`
	Usage     *Usage           `json:"usage,omitempty"`
}

// StoredProposal is a proposal row as events and the API present it.
type StoredProposal struct {
	ID         uuid.UUID         `json:"id"`
	RunID      uuid.UUID         `json:"run_id"`
	ProjectID  uuid.UUID         `json:"project_id"`
	ChapterID  *uuid.UUID        `json:"chapter_id,omitempty"`
	RevisionID *uuid.UUID        `json:"revision_id,omitempty"`
	Action     string            `json:"action"`
	EntryID    *uuid.UUID        `json:"entry_id,omitempty"`
	Section    string            `json:"section"`
	Title      string            `json:"title"`
	Fields     map[string]string `json:"fields"`
	Rationale  string            `json:"rationale"`
	Status     string            `json:"status"`
	Position   int               `json:"position"`
}

// ToStoredProposal converts a row.
func ToStoredProposal(r sqlcgen.BibleProposal) StoredProposal {
	out := StoredProposal{ID: r.ID, RunID: r.RunID, ProjectID: r.ProjectID, Action: r.Action, Section: r.Section, Title: r.Title, Fields: map[string]string{}, Rationale: r.Rationale, Status: r.Status, Position: int(r.Position)}
	if r.ChapterID.Valid {
		id := r.ChapterID.UUID
		out.ChapterID = &id
	}
	if r.RevisionID.Valid {
		id := r.RevisionID.UUID
		out.RevisionID = &id
	}
	if r.EntryID.Valid {
		id := r.EntryID.UUID
		out.EntryID = &id
	}
	_ = json.Unmarshal(r.Fields, &out.Fields)
	if out.Fields == nil {
		out.Fields = map[string]string{}
	}
	return out
}

// BibleUpdateResult is stored as the run's result.
type BibleUpdateResult struct {
	Proposals int `json:"proposals"`
	Adds      int `json:"adds"`
	Updates   int `json:"updates"`
	Deletes   int `json:"deletes"`
}

// BibleUpdate asks the bible keeper what the chapter changes in the story
// bible and stores its proposals for the author to decide on.
func (g *Guild) BibleUpdate(ctx context.Context, user sqlcgen.User, in BibleUpdateInput) (*runs.Run, error) {
	params := map[string]any{"chapter_title": in.Chapter.Title, "content_hash": in.Chapter.ContentHash}
	if in.Revision != nil {
		params["revision_id"] = in.Revision.ID
	}
	start := runs.StartParams{
		User: user, Kind: runs.KindBibleUpdate, Params: params,
		ProjectID: uuid.NullUUID{UUID: in.Project.ID, Valid: true}, ProjectName: in.Project.Name,
		ChapterID: uuid.NullUUID{UUID: in.Chapter.ID, Valid: true},
	}
	return g.engine.Launch(ctx, start, func(ctx context.Context, run *runs.Run, em *runs.Emitter) (any, error) {
		return g.runBibleUpdate(ctx, run, em, in)
	})
}

func (g *Guild) runBibleUpdate(ctx context.Context, run *runs.Run, em *runs.Emitter, in BibleUpdateInput) (any, error) {
	keeper, err := g.q.GetWriterBySlug(ctx, sqlcgen.GetWriterBySlugParams{UserID: run.Row.UserID, Slug: seed.SlugBibleKeeper})
	if err != nil {
		return nil, errors.New("no bible-keeper writer exists in this workspace; it is one of the system agents on the Writers page")
	}
	base := BibleEvent{WriterID: keeper.ID, Slug: keeper.Slug}
	_ = em.Emit(ctx, EventBibleStarted, base)

	byID := make(map[uuid.UUID]sqlcgen.BibleEntry, len(in.Bible))
	for _, e := range in.Bible {
		byID[e.ID] = e
	}
	messages := []llm.Message{
		{Role: "system", Content: BibleKeeperSystemPrompt(keeper.SystemPrompt)},
		{Role: "user", Content: BibleKeeperUserPrompt(in)},
	}
	var usage Usage
	var set *ProposalSet
	for attempt := 1; attempt <= 2; attempt++ {
		callCtx, cancel := context.WithTimeout(ctx, g.CallTimeout)
		stream := newDeltaStream(em, func(t string) (string, any) {
			ev := base
			ev.Text = t
			return EventBibleDelta, ev
		})
		req := llm.Request{Model: keeper.ModelAlias, Messages: messages, Temperature: llm.Float64(keeper.Temperature), JSONMode: true}
		resp, err := g.tracker.Call(callCtx, run, runs.CallOpts{
			WriterID: uuid.NullUUID{UUID: keeper.ID, Valid: true}, GenerationName: "bible-keeper", OnDelta: stream.delta,
		}, req)
		stream.flush(callCtx)
		cancel()
		if resp != nil {
			usage.addResponse(resp)
		}
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			if errors.Is(err, context.DeadlineExceeded) {
				return nil, fmt.Errorf("the bible keeper did not answer within %s", g.CallTimeout)
			}
			return nil, errors.New(FriendlyError(err))
		}
		parsed, perr := ParseProposals(resp.Content, byID)
		if perr == nil {
			set = parsed
			break
		}
		var verr *ValidationError
		if !errors.As(perr, &verr) {
			return nil, perr
		}
		if attempt == 2 {
			return nil, fmt.Errorf("the bible keeper returned invalid output twice: %s", strings.Join(verr.Problems, "; "))
		}
		retry := base
		retry.Reason = strings.Join(verr.Problems, "; ")
		_ = em.Emit(ctx, EventBibleRetry, retry)
		messages = append(messages,
			llm.Message{Role: "assistant", Content: resp.Content},
			llm.Message{Role: "user", Content: RetryPrompt(verr)},
		)
	}

	bg := context.WithoutCancel(ctx)
	result := BibleUpdateResult{}
	var stored []StoredProposal
	for i, p := range set.Proposals {
		fieldsJSON, _ := json.Marshal(p.Fields)
		entryID := uuid.NullUUID{}
		if p.EntryID != nil {
			entryID = uuid.NullUUID{UUID: *p.EntryID, Valid: true}
		}
		revisionID := uuid.NullUUID{}
		if in.Revision != nil {
			revisionID = uuid.NullUUID{UUID: in.Revision.ID, Valid: true}
		}
		row, err := g.q.CreateBibleProposal(bg, sqlcgen.CreateBibleProposalParams{
			UserID: run.Row.UserID, RunID: run.Row.ID, ProjectID: in.Project.ID, ChapterID: uuid.NullUUID{UUID: in.Chapter.ID, Valid: true}, RevisionID: revisionID,
			Action: p.Action, EntryID: entryID, Section: p.Section, Title: p.Title, Fields: fieldsJSON, Rationale: p.Rationale, Position: int32(i),
		})
		if err != nil {
			return nil, fmt.Errorf("store proposal: %w", err)
		}
		stored = append(stored, ToStoredProposal(row))
		switch p.Action {
		case ProposalAdd:
			result.Adds++
		case ProposalUpdate:
			result.Updates++
		case ProposalDelete:
			result.Deletes++
		}
	}
	result.Proposals = len(stored)
	done := base
	done.Proposals = stored
	if done.Proposals == nil {
		done.Proposals = []StoredProposal{}
	}
	done.Warnings = set.Warnings
	done.Usage = &usage
	_ = em.Emit(bg, EventBibleDone, done)
	return result, nil
}

// BibleFormat is the output contract of the bible keeper.
const BibleFormat = `Return ONLY a JSON object with exactly this shape and nothing else:
{
  "proposals": [
    {
      "action": "add" | "update" | "delete",
      "entry_id": "<the id of the existing entry, for update and delete; omit for add>",
      "section": "premise" | "character" | "setting" | "timeline" | "style" | "chapter_summary",
      "title": "<the entry's name: a character's name, a place, a rule, a chapter>",
      "fields": { "<field>": "<value>" },
      "rationale": "<one sentence saying what in the chapter requires this>"
    }
  ]
}
Rules: propose only what the chapter states or clearly implies; for an update give the fields as they should read afterwards (only the fields that change); never invent facts; use [] when nothing in the bible needs to change; no Markdown fences, no commentary.`

const bibleKeeperRole = `

## Your task in this session
A chapter of the novel has just been revised. You receive the story bible with each entry's id, the chapter as it now stands and the changes that were made. Propose the additions, updates and deletions that keep the bible true to the text, each with a one-sentence rationale.

## Required output
` + BibleFormat

// BibleKeeperSystemPrompt is the keeper's own prompt plus the fixed suffix
// and the task.
func BibleKeeperSystemPrompt(writerPrompt string) string {
	return SystemPrompt(writerPrompt) + bibleKeeperRole
}

// BibleKeeperUserPrompt lays out the bible with ids, the chapter and the
// changes of the revision.
func BibleKeeperUserPrompt(in BibleUpdateInput) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Story bible of \"%s\" (entry ids in brackets)\n\n%s\n", in.Project.Name, BibleContextWithIDs(in.Bible))
	fmt.Fprintf(&b, "# Chapter: %s, as it now stands\n\n", in.Chapter.Title)
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
		b.WriteString("(The chapter is too long to include here; work from the changes below.)\n\n")
	}
	if in.Revision != nil {
		var hunks []text.Hunk
		_ = json.Unmarshal(in.Revision.Hunks, &hunks)
		var applied map[int]bool
		if len(in.Revision.AppliedHunks) > 0 && string(in.Revision.AppliedHunks) != "null" {
			var idx []int
			_ = json.Unmarshal(in.Revision.AppliedHunks, &idx)
			applied = map[int]bool{}
			for _, i := range idx {
				applied[i] = true
			}
		}
		b.WriteString("# What the revision changed\n\n")
		n := 0
		for _, h := range hunks {
			if applied != nil && !applied[h.Index] {
				continue
			}
			n++
			fmt.Fprintf(&b, "%d. Before: %q\n   After: %q\n", n, strings.TrimSpace(h.ContextBefore+h.OldText+h.ContextAfter), strings.TrimSpace(h.ContextBefore+h.NewText+h.ContextAfter))
		}
		if n == 0 {
			b.WriteString("(No textual change was recorded.)\n")
		}
		b.WriteString("\n")
	}
	b.WriteString("Propose the story bible changes now. Return the JSON object only.")
	return b.String()
}

type rawProposals struct {
	Proposals []struct {
		Action    string         `json:"action"`
		EntryID   string         `json:"entry_id"`
		Section   string         `json:"section"`
		Title     string         `json:"title"`
		Fields    map[string]any `json:"fields"`
		Rationale string         `json:"rationale"`
	} `json:"proposals"`
}

// ParseProposals decodes the keeper's reply and validates it. Bad actions
// or sections and missing rationales are fatal (one retry); an update or a
// delete naming an unknown entry is dropped with a warning; an update's
// fields are merged over the entry so the proposal shows the whole entry as
// it would read.
func ParseProposals(reply string, entries map[uuid.UUID]sqlcgen.BibleEntry) (*ProposalSet, error) {
	body, err := ExtractJSON(reply)
	if err != nil {
		return nil, &ValidationError{Problems: []string{err.Error()}}
	}
	var raw rawProposals
	if err := json.Unmarshal([]byte(body), &raw); err != nil {
		return nil, &ValidationError{Problems: []string{"the JSON does not parse: " + err.Error()}}
	}
	out := &ProposalSet{Proposals: []Proposal{}}
	var problems []string
	for i, rp := range raw.Proposals {
		n := i + 1
		p := Proposal{Action: strings.ToLower(strings.TrimSpace(rp.Action)), Section: strings.ToLower(strings.TrimSpace(rp.Section)), Title: strings.TrimSpace(rp.Title), Rationale: strings.TrimSpace(rp.Rationale), Fields: stringFields(rp.Fields)}
		if p.Action != ProposalAdd && p.Action != ProposalUpdate && p.Action != ProposalDelete {
			problems = append(problems, fmt.Sprintf("proposal %d: action %q is not one of add, update, delete", n, rp.Action))
			continue
		}
		if p.Rationale == "" {
			problems = append(problems, fmt.Sprintf("proposal %d: \"rationale\" is empty", n))
		}
		if p.Action == ProposalAdd {
			if !BibleSections[p.Section] {
				problems = append(problems, fmt.Sprintf("proposal %d: section %q is not valid", n, rp.Section))
				continue
			}
			if p.Title == "" && len(p.Fields) == 0 {
				problems = append(problems, fmt.Sprintf("proposal %d: an addition needs a title or fields", n))
			}
			out.Proposals = append(out.Proposals, p)
			continue
		}
		id, perr := uuid.Parse(strings.TrimSpace(rp.EntryID))
		entry, ok := entries[id]
		if perr != nil || !ok {
			// Tolerate a title in place of the id when it is unambiguous.
			entry, ok = entryByTitle(entries, rp.EntryID, p.Title)
			if !ok {
				out.Warnings = append(out.Warnings, fmt.Sprintf("proposal %d (%s) named an entry that does not exist (%q) and was dropped", n, p.Action, rp.EntryID))
				continue
			}
		}
		eid := entry.ID
		p.EntryID = &eid
		p.Section = entry.Section
		if p.Action == ProposalDelete {
			p.Title = entry.Title
			p.Fields = decodeStringFields(entry.Fields)
			out.Proposals = append(out.Proposals, p)
			continue
		}
		merged := decodeStringFields(entry.Fields)
		for k, v := range p.Fields {
			merged[k] = v
		}
		p.Fields = merged
		if p.Title == "" {
			p.Title = entry.Title
		}
		out.Proposals = append(out.Proposals, p)
	}
	if len(problems) > 0 {
		return nil, &ValidationError{Problems: problems}
	}
	return out, nil
}

func entryByTitle(entries map[uuid.UUID]sqlcgen.BibleEntry, candidates ...string) (sqlcgen.BibleEntry, bool) {
	for _, c := range candidates {
		c = strings.TrimSpace(c)
		if c == "" {
			continue
		}
		var found sqlcgen.BibleEntry
		matches := 0
		for _, e := range entries {
			if strings.EqualFold(e.Title, c) {
				found = e
				matches++
			}
		}
		if matches == 1 {
			return found, true
		}
	}
	return sqlcgen.BibleEntry{}, false
}

func stringFields(m map[string]any) map[string]string {
	out := map[string]string{}
	for k, v := range m {
		switch t := v.(type) {
		case string:
			out[k] = strings.TrimSpace(t)
		case nil:
			out[k] = ""
		default:
			b, _ := json.Marshal(t)
			out[k] = string(b)
		}
	}
	return out
}

func decodeStringFields(raw []byte) map[string]string {
	var generic map[string]any
	if len(raw) == 0 || json.Unmarshal(raw, &generic) != nil {
		return map[string]string{}
	}
	return stringFields(generic)
}

// SortedFieldKeys returns a map's keys in a stable order for display.
func SortedFieldKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
