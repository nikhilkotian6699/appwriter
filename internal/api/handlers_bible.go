package api

import (
	"net/http"
	"strings"

	"github.com/google/uuid"

	"writersguild/internal/db/sqlcgen"
)

var bibleSections = map[BibleSection]bool{
	"premise": true, "character": true, "setting": true, "timeline": true, "style": true, "chapter_summary": true,
}

// ListBibleEntries returns every entry of a project's story bible.
func (s *Server) ListBibleEntries(w http.ResponseWriter, r *http.Request, projectId ProjectId) {
	u := currentUser(r.Context())
	if _, err := s.q.GetProject(r.Context(), sqlcgen.GetProjectParams{ID: projectId, UserID: u.ID}); err != nil {
		s.fail(w, err)
		return
	}
	rows, err := s.q.ListBibleEntries(r.Context(), sqlcgen.ListBibleEntriesParams{ProjectID: projectId, UserID: u.ID})
	if err != nil {
		s.fail(w, err)
		return
	}
	out := make([]BibleEntry, 0, len(rows))
	for _, e := range rows {
		out = append(out, toBibleEntry(e))
	}
	writeJSON(w, http.StatusOK, out)
}

// checkChapterInProject verifies an optional chapter reference.
func (s *Server) checkChapterInProject(r *http.Request, u sqlcgen.User, projectID uuid.UUID, chapterID *uuid.UUID) error {
	if chapterID == nil {
		return nil
	}
	c, err := s.q.GetChapter(r.Context(), sqlcgen.GetChapterParams{ID: *chapterID, UserID: u.ID})
	if err != nil || c.ProjectID != projectID {
		return errBadRequest("chapter_id does not belong to this project")
	}
	return nil
}

// CreateBibleEntry adds an entry to a section.
func (s *Server) CreateBibleEntry(w http.ResponseWriter, r *http.Request, projectId ProjectId) {
	u := currentUser(r.Context())
	var in BibleEntryInput
	if err := decodeJSON(r, &in); err != nil {
		s.fail(w, err)
		return
	}
	if !bibleSections[in.Section] {
		s.fail(w, errBadRequest("unknown section %q", in.Section))
		return
	}
	title := strings.TrimSpace(in.Title)
	if len(title) > 300 {
		s.fail(w, errBadRequest("title must be at most 300 characters"))
		return
	}
	if _, err := s.q.GetProject(r.Context(), sqlcgen.GetProjectParams{ID: projectId, UserID: u.ID}); err != nil {
		s.fail(w, err)
		return
	}
	if err := s.checkChapterInProject(r, u, projectId, in.ChapterId); err != nil {
		s.fail(w, err)
		return
	}
	fields, err := encodeFields(in.Fields)
	if err != nil {
		s.fail(w, errBadRequest("fields must be an object of strings"))
		return
	}
	var pos int32
	if in.Position != nil {
		pos = int32(*in.Position)
	} else {
		pos, err = s.q.NextBibleEntryPosition(r.Context(), sqlcgen.NextBibleEntryPositionParams{ProjectID: projectId, Section: string(in.Section)})
		if err != nil {
			s.fail(w, err)
			return
		}
	}
	e, err := s.q.CreateBibleEntry(r.Context(), sqlcgen.CreateBibleEntryParams{
		UserID: u.ID, ProjectID: projectId, Section: string(in.Section), Title: title, Fields: fields, ChapterID: nullUUID(in.ChapterId), Position: pos,
	})
	if err != nil {
		s.fail(w, err)
		return
	}
	_ = s.q.TouchProject(r.Context(), projectId)
	writeJSON(w, http.StatusCreated, toBibleEntry(e))
}

// UpdateBibleEntry edits an entry. The section cannot change.
func (s *Server) UpdateBibleEntry(w http.ResponseWriter, r *http.Request, entryId EntryId) {
	u := currentUser(r.Context())
	var in BibleEntryInput
	if err := decodeJSON(r, &in); err != nil {
		s.fail(w, err)
		return
	}
	cur, err := s.q.GetBibleEntry(r.Context(), sqlcgen.GetBibleEntryParams{ID: entryId, UserID: u.ID})
	if err != nil {
		s.fail(w, err)
		return
	}
	title := strings.TrimSpace(in.Title)
	if len(title) > 300 {
		s.fail(w, errBadRequest("title must be at most 300 characters"))
		return
	}
	if err := s.checkChapterInProject(r, u, cur.ProjectID, in.ChapterId); err != nil {
		s.fail(w, err)
		return
	}
	fields, err := encodeFields(in.Fields)
	if err != nil {
		s.fail(w, errBadRequest("fields must be an object of strings"))
		return
	}
	pos := cur.Position
	if in.Position != nil {
		pos = int32(*in.Position)
	}
	e, err := s.q.UpdateBibleEntry(r.Context(), sqlcgen.UpdateBibleEntryParams{
		ID: entryId, UserID: u.ID, Title: title, Fields: fields, ChapterID: nullUUID(in.ChapterId), Position: pos,
	})
	if err != nil {
		s.fail(w, err)
		return
	}
	_ = s.q.TouchProject(r.Context(), e.ProjectID)
	writeJSON(w, http.StatusOK, toBibleEntry(e))
}

// DeleteBibleEntry removes an entry.
func (s *Server) DeleteBibleEntry(w http.ResponseWriter, r *http.Request, entryId EntryId) {
	u := currentUser(r.Context())
	n, err := s.q.DeleteBibleEntry(r.Context(), sqlcgen.DeleteBibleEntryParams{ID: entryId, UserID: u.ID})
	if err != nil {
		s.fail(w, err)
		return
	}
	if n == 0 {
		s.fail(w, errNotFound("entry"))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
