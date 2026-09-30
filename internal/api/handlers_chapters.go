package api

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"writersguild/internal/db/sqlcgen"
)

// ListChapters lists a project's chapters in order.
func (s *Server) ListChapters(w http.ResponseWriter, r *http.Request, projectId ProjectId) {
	u := currentUser(r.Context())
	if _, err := s.q.GetProject(r.Context(), sqlcgen.GetProjectParams{ID: projectId, UserID: u.ID}); err != nil {
		s.fail(w, err)
		return
	}
	rows, err := s.q.ListChapters(r.Context(), sqlcgen.ListChaptersParams{ProjectID: projectId, UserID: u.ID})
	if err != nil {
		s.fail(w, err)
		return
	}
	out := make([]ChapterSummary, 0, len(rows))
	for _, c := range rows {
		out = append(out, toChapterSummary(c))
	}
	writeJSON(w, http.StatusOK, out)
}

// CreateChapter appends a chapter to a project.
func (s *Server) CreateChapter(w http.ResponseWriter, r *http.Request, projectId ProjectId) {
	u := currentUser(r.Context())
	var in ChapterCreateInput
	if err := decodeJSON(r, &in); err != nil {
		s.fail(w, err)
		return
	}
	title := strings.TrimSpace(in.Title)
	if title == "" || len(title) > 300 {
		s.fail(w, errBadRequest("title must be 1 to 300 characters"))
		return
	}
	if _, err := s.q.GetProject(r.Context(), sqlcgen.GetProjectParams{ID: projectId, UserID: u.ID}); err != nil {
		s.fail(w, err)
		return
	}
	pos, err := s.q.NextChapterPosition(r.Context(), projectId)
	if err != nil {
		s.fail(w, err)
		return
	}
	content := stringOr(in.ContentMd, "")
	c, err := s.q.CreateChapter(r.Context(), sqlcgen.CreateChapterParams{
		UserID: u.ID, ProjectID: projectId, Title: title, Position: pos, ContentMd: content, ContentHash: hashContent(content),
	})
	if err != nil {
		s.fail(w, err)
		return
	}
	_ = s.q.TouchProject(r.Context(), projectId)
	writeJSON(w, http.StatusCreated, toChapter(c))
}

// GetChapter returns a chapter with its text.
func (s *Server) GetChapter(w http.ResponseWriter, r *http.Request, chapterId ChapterId) {
	u := currentUser(r.Context())
	c, err := s.q.GetChapter(r.Context(), sqlcgen.GetChapterParams{ID: chapterId, UserID: u.ID})
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toChapter(c))
}

// UpdateChapter renames or moves a chapter.
func (s *Server) UpdateChapter(w http.ResponseWriter, r *http.Request, chapterId ChapterId) {
	u := currentUser(r.Context())
	var in ChapterMetaInput
	if err := decodeJSON(r, &in); err != nil {
		s.fail(w, err)
		return
	}
	title := strings.TrimSpace(in.Title)
	if title == "" || len(title) > 300 {
		s.fail(w, errBadRequest("title must be 1 to 300 characters"))
		return
	}
	cur, err := s.q.GetChapter(r.Context(), sqlcgen.GetChapterParams{ID: chapterId, UserID: u.ID})
	if err != nil {
		s.fail(w, err)
		return
	}
	pos := cur.Position
	if in.Position != nil {
		if *in.Position < 0 {
			s.fail(w, errBadRequest("position must not be negative"))
			return
		}
		pos = int32(*in.Position)
	}
	c, err := s.q.UpdateChapterMeta(r.Context(), sqlcgen.UpdateChapterMetaParams{ID: chapterId, UserID: u.ID, Title: title, Position: pos})
	if err != nil {
		s.fail(w, err)
		return
	}
	_ = s.q.TouchProject(r.Context(), c.ProjectID)
	writeJSON(w, http.StatusOK, toChapter(c))
}

// DeleteChapter removes a chapter and its versions.
func (s *Server) DeleteChapter(w http.ResponseWriter, r *http.Request, chapterId ChapterId) {
	u := currentUser(r.Context())
	n, err := s.q.DeleteChapter(r.Context(), sqlcgen.DeleteChapterParams{ID: chapterId, UserID: u.ID})
	if err != nil {
		s.fail(w, err)
		return
	}
	if n == 0 {
		s.fail(w, errNotFound("chapter"))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// SaveChapterContent stores the text and takes an autosave snapshot when the
// last snapshot is older than the account's autosave interval.
func (s *Server) SaveChapterContent(w http.ResponseWriter, r *http.Request, chapterId ChapterId) {
	u := currentUser(r.Context())
	var in ChapterContentInput
	if err := decodeJSON(r, &in); err != nil {
		s.fail(w, err)
		return
	}
	result, err := s.saveContent(r.Context(), u, chapterId, in.ContentMd, stringOr(in.BaseHash, ""))
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) saveContent(ctx context.Context, u sqlcgen.User, chapterID uuid.UUID, content, baseHash string) (*ChapterSaveResult, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	q := s.q.WithTx(tx)

	cur, err := q.GetChapterForUpdate(ctx, sqlcgen.GetChapterForUpdateParams{ID: chapterID, UserID: u.ID})
	if err != nil {
		return nil, err
	}
	if baseHash != "" && baseHash != cur.ContentHash {
		return nil, errConflict("the chapter changed since it was loaded; reload before saving")
	}
	newHash := hashContent(content)
	if newHash == cur.ContentHash {
		return &ChapterSaveResult{Chapter: toChapter(cur), SnapshotCreated: false}, nil
	}
	if err := q.EnsureSettings(ctx, u.ID); err != nil {
		return nil, err
	}
	st, err := q.GetSettings(ctx, u.ID)
	if err != nil {
		return nil, err
	}
	interval := time.Duration(st.AutosaveSnapshotMinutes) * time.Minute
	snapshot := false
	latest, err := q.LatestChapterVersion(ctx, chapterID)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		snapshot = true
	case err != nil:
		return nil, err
	default:
		snapshot = time.Since(latest.CreatedAt) >= interval
	}
	updated, err := q.UpdateChapterContent(ctx, sqlcgen.UpdateChapterContentParams{ID: chapterID, UserID: u.ID, ContentMd: content, ContentHash: newHash})
	if err != nil {
		return nil, err
	}
	if snapshot {
		if _, err := q.CreateChapterVersion(ctx, sqlcgen.CreateChapterVersionParams{
			UserID: u.ID, ChapterID: chapterID, Kind: "autosave", Label: "", ContentMd: content, ContentHash: newHash,
		}); err != nil {
			return nil, err
		}
	}
	if err := q.TouchProject(ctx, updated.ProjectID); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return &ChapterSaveResult{Chapter: toChapter(updated), SnapshotCreated: snapshot}, nil
}

// ListChapterVersions lists snapshots, newest first.
func (s *Server) ListChapterVersions(w http.ResponseWriter, r *http.Request, chapterId ChapterId) {
	u := currentUser(r.Context())
	if _, err := s.q.GetChapter(r.Context(), sqlcgen.GetChapterParams{ID: chapterId, UserID: u.ID}); err != nil {
		s.fail(w, err)
		return
	}
	rows, err := s.q.ListChapterVersions(r.Context(), sqlcgen.ListChapterVersionsParams{ChapterID: chapterId, UserID: u.ID})
	if err != nil {
		s.fail(w, err)
		return
	}
	out := make([]ChapterVersionSummary, 0, len(rows))
	for _, v := range rows {
		out = append(out, toVersionSummary(v))
	}
	writeJSON(w, http.StatusOK, out)
}

// CreateChapterSnapshot takes a labelled snapshot of the current text.
func (s *Server) CreateChapterSnapshot(w http.ResponseWriter, r *http.Request, chapterId ChapterId) {
	u := currentUser(r.Context())
	var in SnapshotInput
	if err := decodeJSON(r, &in); err != nil {
		s.fail(w, err)
		return
	}
	label := strings.TrimSpace(stringOr(in.Label, ""))
	if len(label) > 200 {
		s.fail(w, errBadRequest("label must be at most 200 characters"))
		return
	}
	c, err := s.q.GetChapter(r.Context(), sqlcgen.GetChapterParams{ID: chapterId, UserID: u.ID})
	if err != nil {
		s.fail(w, err)
		return
	}
	v, err := s.q.CreateChapterVersion(r.Context(), sqlcgen.CreateChapterVersionParams{
		UserID: u.ID, ChapterID: chapterId, Kind: "manual", Label: label, ContentMd: c.ContentMd, ContentHash: c.ContentHash,
	})
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, toVersion(v))
}

// GetChapterVersion returns one snapshot with its text.
func (s *Server) GetChapterVersion(w http.ResponseWriter, r *http.Request, chapterId ChapterId, versionId VersionId) {
	u := currentUser(r.Context())
	v, err := s.q.GetChapterVersion(r.Context(), sqlcgen.GetChapterVersionParams{ID: versionId, UserID: u.ID})
	if err != nil {
		s.fail(w, err)
		return
	}
	if v.ChapterID != chapterId {
		s.fail(w, errNotFound("version"))
		return
	}
	writeJSON(w, http.StatusOK, toVersion(v))
}

// RestoreChapterVersion snapshots the current text as pre_restore, then
// replaces it with the chosen version.
func (s *Server) RestoreChapterVersion(w http.ResponseWriter, r *http.Request, chapterId ChapterId, versionId VersionId) {
	u := currentUser(r.Context())
	ctx := r.Context()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		s.fail(w, err)
		return
	}
	defer tx.Rollback(ctx)
	q := s.q.WithTx(tx)

	cur, err := q.GetChapterForUpdate(ctx, sqlcgen.GetChapterForUpdateParams{ID: chapterId, UserID: u.ID})
	if err != nil {
		s.fail(w, err)
		return
	}
	v, err := q.GetChapterVersion(ctx, sqlcgen.GetChapterVersionParams{ID: versionId, UserID: u.ID})
	if err != nil || v.ChapterID != chapterId {
		s.fail(w, errNotFound("version"))
		return
	}
	label := "Before restoring " + v.CreatedAt.Local().Format("2 Jan 2006 15:04")
	if v.Label != "" {
		label = "Before restoring \"" + v.Label + "\""
	}
	if _, err := q.CreateChapterVersion(ctx, sqlcgen.CreateChapterVersionParams{
		UserID: u.ID, ChapterID: chapterId, Kind: "pre_restore", Label: label, ContentMd: cur.ContentMd, ContentHash: cur.ContentHash,
	}); err != nil {
		s.fail(w, err)
		return
	}
	updated, err := q.UpdateChapterContent(ctx, sqlcgen.UpdateChapterContentParams{ID: chapterId, UserID: u.ID, ContentMd: v.ContentMd, ContentHash: hashContent(v.ContentMd)})
	if err != nil {
		s.fail(w, err)
		return
	}
	if err := q.TouchProject(ctx, updated.ProjectID); err != nil {
		s.fail(w, err)
		return
	}
	if err := tx.Commit(ctx); err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toChapter(updated))
}
