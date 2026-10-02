package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"writersguild/internal/db/sqlcgen"
	"writersguild/internal/guild"
	"writersguild/internal/seed"
)

// StartCritique convenes the chosen critics on a chapter as a background run.
func (s *Server) StartCritique(w http.ResponseWriter, r *http.Request, chapterId ChapterId) {
	u := currentUser(r.Context())
	var in CritiqueStartInput
	if r.ContentLength != 0 && r.Body != nil {
		if err := decodeJSON(r, &in); err != nil {
			s.fail(w, err)
			return
		}
	}
	ch, err := s.q.GetChapter(r.Context(), sqlcgen.GetChapterParams{ID: chapterId, UserID: u.ID})
	if err != nil {
		s.fail(w, err)
		return
	}
	if strings.TrimSpace(ch.ContentMd) == "" {
		s.fail(w, errBadRequest("the chapter is empty; write something before convening the Guild"))
		return
	}
	project, err := s.q.GetProject(r.Context(), sqlcgen.GetProjectParams{ID: ch.ProjectID, UserID: u.ID})
	if err != nil {
		s.fail(w, err)
		return
	}
	all, err := s.q.ListWriters(r.Context(), u.ID)
	if err != nil {
		s.fail(w, err)
		return
	}
	critics, err := chooseCritics(all, in.WriterIds)
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
	run, err := s.guild.Critique(r.Context(), u, guild.CritiqueInput{
		Project: project, Chapter: ch, Writers: critics, Bible: bible, SceneTokenLimit: int(settings.SceneTokenLimit),
	})
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, toRun(run.Row))
}

// chooseCritics picks the requested writers, or every enabled critic.
func chooseCritics(all []sqlcgen.Writer, ids *[]uuid.UUID) ([]sqlcgen.Writer, error) {
	isCritic := func(w sqlcgen.Writer) bool {
		for _, r := range w.Roles {
			if r == seed.RoleCritic {
				return true
			}
		}
		return false
	}
	var out []sqlcgen.Writer
	if ids == nil || len(*ids) == 0 {
		for _, w := range all {
			if w.Enabled && !w.IsSystem && isCritic(w) {
				out = append(out, w)
			}
		}
		if len(out) == 0 {
			return nil, errBadRequest("no enabled writer has the critic role; enable one on the Writers page")
		}
		return out, nil
	}
	byID := make(map[uuid.UUID]sqlcgen.Writer, len(all))
	for _, w := range all {
		byID[w.ID] = w
	}
	seen := map[uuid.UUID]bool{}
	for _, id := range *ids {
		if seen[id] {
			continue
		}
		seen[id] = true
		w, ok := byID[id]
		if !ok {
			return nil, errNotFound("writer " + id.String())
		}
		if w.IsSystem || !isCritic(w) {
			return nil, errBadRequest("%s does not have the critic role", w.Name)
		}
		if !w.Enabled {
			return nil, errBadRequest("%s is disabled", w.Name)
		}
		out = append(out, w)
	}
	if len(out) > 12 {
		return nil, errBadRequest("at most 12 critics can attend at once")
	}
	return out, nil
}

// ListRunCritiques returns the critics' records for a run.
func (s *Server) ListRunCritiques(w http.ResponseWriter, r *http.Request, runId RunId) {
	u := currentUser(r.Context())
	if _, err := s.q.GetRun(r.Context(), sqlcgen.GetRunParams{ID: runId, UserID: u.ID}); err != nil {
		s.fail(w, err)
		return
	}
	rows, err := s.q.ListRunCritiques(r.Context(), sqlcgen.ListRunCritiquesParams{RunID: runId, UserID: u.ID})
	if err != nil {
		s.fail(w, err)
		return
	}
	out := make([]CritiqueRecord, 0, len(rows))
	for _, row := range rows {
		rec, err := toCritiqueRecord(row)
		if err != nil {
			s.fail(w, fmt.Errorf("decode critique %s: %w", row.ID, err))
			return
		}
		out = append(out, rec)
	}
	writeJSON(w, http.StatusOK, out)
}

func toCritiqueRecord(c sqlcgen.Critique) (CritiqueRecord, error) {
	out := CritiqueRecord{
		Id: c.ID, RunId: c.RunID, WriterName: c.WriterName, WriterSlug: c.WriterSlug, ModelAlias: c.ModelAlias,
		Status: CritiqueStatus(c.Status), RawText: c.RawText, Error: c.Error, SceneCount: int(c.SceneCount),
		PromptTokens: int(c.PromptTokens), CompletionTokens: int(c.CompletionTokens), CostUsd: c.CostUsd, CostEstimated: c.CostEstimated,
		CreatedAt: c.CreatedAt, FinishedAt: c.FinishedAt,
	}
	if c.ChapterID.Valid {
		out.ChapterId = ptr(c.ChapterID.UUID)
	}
	if c.WriterID.Valid {
		out.WriterId = ptr(c.WriterID.UUID)
	}
	if len(c.Critique) > 0 && string(c.Critique) != "null" {
		var parsed Critique
		if err := json.Unmarshal(c.Critique, &parsed); err != nil {
			return out, err
		}
		out.Critique = &parsed
	}
	return out, nil
}
