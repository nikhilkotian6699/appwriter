package api

import (
	"net/http"
	"strings"

	"github.com/google/uuid"

	"writersguild/internal/db/sqlcgen"
	"writersguild/internal/guild"
	"writersguild/internal/seed"
)

// StartCowrite asks one co-writer for a draft, or two or three for drafts to
// compare, as a background run.
func (s *Server) StartCowrite(w http.ResponseWriter, r *http.Request, chapterId ChapterId) {
	u := currentUser(r.Context())
	var in CowriteStartInput
	if err := decodeJSON(r, &in); err != nil {
		s.fail(w, err)
		return
	}
	instruction := strings.TrimSpace(in.Instruction)
	if instruction == "" || len(instruction) > 2000 {
		s.fail(w, errBadRequest("instruction must be 1 to 2000 characters"))
		return
	}
	if len(in.WriterIds) == 0 {
		s.fail(w, errBadRequest("choose at least one co-writer"))
		return
	}
	if len(in.WriterIds) > guild.MaxCompareWriters {
		s.fail(w, errBadRequest("at most %d writers can be compared", guild.MaxCompareWriters))
		return
	}
	selection := stringOr(in.Selection, "")
	notes := strings.TrimSpace(stringOr(in.Notes, ""))
	before := stringOr(in.ContextBefore, "")
	after := stringOr(in.ContextAfter, "")
	if len(selection) > 60000 || len(notes) > 4000 || len(before) > 20000 || len(after) > 20000 {
		s.fail(w, errBadRequest("the request is too long"))
		return
	}
	ch, err := s.q.GetChapter(r.Context(), sqlcgen.GetChapterParams{ID: chapterId, UserID: u.ID})
	if err != nil {
		s.fail(w, err)
		return
	}
	all, err := s.q.ListWriters(r.Context(), u.ID)
	if err != nil {
		s.fail(w, err)
		return
	}
	byID := make(map[uuid.UUID]sqlcgen.Writer, len(all))
	for _, wr := range all {
		byID[wr.ID] = wr
	}
	var writers []sqlcgen.Writer
	seen := map[uuid.UUID]bool{}
	for _, id := range in.WriterIds {
		if seen[id] {
			continue
		}
		seen[id] = true
		wr, ok := byID[id]
		if !ok {
			s.fail(w, errNotFound("writer "+id.String()))
			return
		}
		if wr.IsSystem || !hasRole(wr, seed.RoleCoWriter) {
			s.fail(w, errBadRequest("%s does not have the co-writer role", wr.Name))
			return
		}
		if !wr.Enabled {
			s.fail(w, errBadRequest("%s is disabled", wr.Name))
			return
		}
		writers = append(writers, wr)
	}
	project, err := s.q.GetProject(r.Context(), sqlcgen.GetProjectParams{ID: ch.ProjectID, UserID: u.ID})
	if err != nil {
		s.fail(w, err)
		return
	}
	bible, err := s.q.ListBibleEntries(r.Context(), sqlcgen.ListBibleEntriesParams{ProjectID: ch.ProjectID, UserID: u.ID})
	if err != nil {
		s.fail(w, err)
		return
	}
	if err := s.q.EnsureSettings(r.Context(), u.ID); err != nil {
		s.fail(w, err)
		return
	}
	settings, err := s.q.GetSettings(r.Context(), u.ID)
	if err != nil {
		s.fail(w, err)
		return
	}
	run, err := s.guild.Cowrite(r.Context(), u, guild.CowriteInput{
		Project: project, Chapter: ch, Bible: bible, Writers: writers, Instruction: instruction, Selection: selection, Notes: notes,
		ContextBefore: before, ContextAfter: after, SceneTokenLimit: int(settings.SceneTokenLimit),
	})
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, toRun(run.Row))
}

func hasRole(w sqlcgen.Writer, role string) bool {
	for _, r := range w.Roles {
		if r == role {
			return true
		}
	}
	return false
}

// ListRunDrafts returns the drafts of a co-write or compare run.
func (s *Server) ListRunDrafts(w http.ResponseWriter, r *http.Request, runId RunId) {
	u := currentUser(r.Context())
	if _, err := s.q.GetRun(r.Context(), sqlcgen.GetRunParams{ID: runId, UserID: u.ID}); err != nil {
		s.fail(w, err)
		return
	}
	rows, err := s.q.ListRunDrafts(r.Context(), sqlcgen.ListRunDraftsParams{RunID: runId, UserID: u.ID})
	if err != nil {
		s.fail(w, err)
		return
	}
	out := make([]Draft, 0, len(rows))
	for _, row := range rows {
		out = append(out, toDraft(row))
	}
	writeJSON(w, http.StatusOK, out)
}

// ListChapterDrafts lists a chapter's recent drafts, newest first.
func (s *Server) ListChapterDrafts(w http.ResponseWriter, r *http.Request, chapterId ChapterId, params ListChapterDraftsParams) {
	u := currentUser(r.Context())
	if _, err := s.q.GetChapter(r.Context(), sqlcgen.GetChapterParams{ID: chapterId, UserID: u.ID}); err != nil {
		s.fail(w, err)
		return
	}
	limit := 20
	if params.Limit != nil {
		limit = *params.Limit
	}
	if limit < 1 || limit > 100 {
		s.fail(w, errBadRequest("limit must be between 1 and 100"))
		return
	}
	rows, err := s.q.ListChapterDrafts(r.Context(), sqlcgen.ListChapterDraftsParams{ChapterID: nullUUID(&chapterId), UserID: u.ID, RowLimit: int32(limit)})
	if err != nil {
		s.fail(w, err)
		return
	}
	out := make([]Draft, 0, len(rows))
	for _, row := range rows {
		out = append(out, toDraft(row))
	}
	writeJSON(w, http.StatusOK, out)
}

var draftDecisions = map[string]bool{guild.DraftPending: true, guild.DraftInserted: true, guild.DraftReplaced: true, guild.DraftDiscarded: true}

// DecideDraft records what the author did with a draft.
func (s *Server) DecideDraft(w http.ResponseWriter, r *http.Request, draftId DraftId) {
	u := currentUser(r.Context())
	var in DraftDecisionInput
	if err := decodeJSON(r, &in); err != nil {
		s.fail(w, err)
		return
	}
	decision := string(in.Decision)
	if !draftDecisions[decision] {
		s.fail(w, errBadRequest("decision must be pending, inserted, replaced or discarded"))
		return
	}
	cur, err := s.q.GetDraft(r.Context(), sqlcgen.GetDraftParams{ID: draftId, UserID: u.ID})
	if err != nil {
		s.fail(w, err)
		return
	}
	if cur.Status != "succeeded" && decision != guild.DraftDiscarded && decision != guild.DraftPending {
		s.fail(w, errBadRequest("only a finished draft can go into the chapter"))
		return
	}
	if decision == guild.DraftReplaced && cur.Mode != guild.ModeSelection {
		s.fail(w, errBadRequest("this draft was not made from a selection; insert it instead"))
		return
	}
	row, err := s.q.SetDraftDecision(r.Context(), sqlcgen.SetDraftDecisionParams{ID: draftId, UserID: u.ID, Decision: decision})
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toDraft(row))
}

func toDraft(d sqlcgen.Draft) Draft {
	out := Draft{
		Id: d.ID, RunId: d.RunID, WriterName: d.WriterName, WriterSlug: d.WriterSlug, ModelAlias: d.ModelAlias, Mode: DraftMode(d.Mode),
		Instruction: d.Instruction, Selection: d.Selection, Notes: d.Notes, Status: DraftStatus(d.Status), Text: d.Text, Error: d.Error,
		Decision: DraftDecision(d.Decision), DecidedAt: d.DecidedAt, Position: int(d.Position), PromptTokens: int(d.PromptTokens), CompletionTokens: int(d.CompletionTokens),
		CostUsd: d.CostUsd, CostEstimated: d.CostEstimated, CreatedAt: d.CreatedAt, FinishedAt: d.FinishedAt,
	}
	if d.ChapterID.Valid {
		out.ChapterId = ptr(d.ChapterID.UUID)
	}
	if d.WriterID.Valid {
		out.WriterId = ptr(d.WriterID.UUID)
	}
	return out
}
