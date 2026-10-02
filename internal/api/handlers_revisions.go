package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/google/uuid"

	"writersguild/internal/db/sqlcgen"
	"writersguild/internal/guild"
	"writersguild/internal/runs"
	"writersguild/internal/text"
)

// StartRevision asks the lead writer to apply the accepted issues of a
// critique run to the chapter, as a background run.
func (s *Server) StartRevision(w http.ResponseWriter, r *http.Request, chapterId ChapterId) {
	u := currentUser(r.Context())
	var in RevisionStartInput
	if err := decodeJSON(r, &in); err != nil {
		s.fail(w, err)
		return
	}
	ch, err := s.q.GetChapter(r.Context(), sqlcgen.GetChapterParams{ID: chapterId, UserID: u.ID})
	if err != nil {
		s.fail(w, err)
		return
	}
	critique, err := s.q.GetRun(r.Context(), sqlcgen.GetRunParams{ID: in.RunId, UserID: u.ID})
	if err != nil {
		s.fail(w, errNotFound("critique run"))
		return
	}
	if critique.Kind != runs.KindCritique || !critique.ChapterID.Valid || critique.ChapterID.UUID != ch.ID {
		s.fail(w, errBadRequest("run_id must be a critique run of this chapter"))
		return
	}
	if critique.Status != runs.StatusSucceeded {
		s.fail(w, errBadRequest("the critique run has not finished successfully"))
		return
	}
	all, err := s.q.ListRunIssues(r.Context(), sqlcgen.ListRunIssuesParams{RunID: critique.ID, UserID: u.ID})
	if err != nil {
		s.fail(w, err)
		return
	}
	var accepted []sqlcgen.Issue
	for _, is := range all {
		if is.Decision == "accepted" {
			accepted = append(accepted, is)
		}
	}
	if len(accepted) == 0 {
		s.fail(w, errBadRequest("accept at least one issue before asking for a revision"))
		return
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
	run, err := s.guild.Revise(r.Context(), u, guild.RevisionInput{
		Project: project, Chapter: ch, Bible: bible, CritiqueRunID: critique.ID, Issues: accepted, SceneTokenLimit: int(settings.SceneTokenLimit),
	})
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, toRun(run.Row))
}

// ListChapterRevisions lists a chapter's revisions, newest first.
func (s *Server) ListChapterRevisions(w http.ResponseWriter, r *http.Request, chapterId ChapterId, params ListChapterRevisionsParams) {
	u := currentUser(r.Context())
	ch, err := s.q.GetChapter(r.Context(), sqlcgen.GetChapterParams{ID: chapterId, UserID: u.ID})
	if err != nil {
		s.fail(w, err)
		return
	}
	status := ""
	if params.Status != nil {
		status = string(*params.Status)
		if status != guild.RevisionProposed && status != guild.RevisionApplied && status != guild.RevisionDiscarded {
			s.fail(w, errBadRequest("unknown revision status %q", status))
			return
		}
	}
	limit := 20
	if params.Limit != nil {
		limit = *params.Limit
	}
	if limit < 1 || limit > 100 {
		s.fail(w, errBadRequest("limit must be between 1 and 100"))
		return
	}
	rows, err := s.q.ListChapterRevisions(r.Context(), sqlcgen.ListChapterRevisionsParams{ChapterID: ch.ID, UserID: u.ID, Status: status, RowLimit: int32(limit)})
	if err != nil {
		s.fail(w, err)
		return
	}
	out := make([]Revision, 0, len(rows))
	for _, row := range rows {
		rev, err := toRevision(row, ch.ContentHash)
		if err != nil {
			s.fail(w, err)
			return
		}
		out = append(out, rev)
	}
	writeJSON(w, http.StatusOK, out)
}

// GetRevision returns one revision with its hunks and whether it is stale.
func (s *Server) GetRevision(w http.ResponseWriter, r *http.Request, revisionId RevisionId) {
	u := currentUser(r.Context())
	row, err := s.q.GetRevision(r.Context(), sqlcgen.GetRevisionParams{ID: revisionId, UserID: u.ID})
	if err != nil {
		s.fail(w, err)
		return
	}
	ch, err := s.q.GetChapter(r.Context(), sqlcgen.GetChapterParams{ID: row.ChapterID, UserID: u.ID})
	if err != nil {
		s.fail(w, err)
		return
	}
	rev, err := toRevision(row, ch.ContentHash)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, rev)
}

// ApplyRevision replaces the chapter text with the chosen hunks applied,
// after a "before revision" snapshot. The chapter must not have changed
// since the revision was proposed.
func (s *Server) ApplyRevision(w http.ResponseWriter, r *http.Request, revisionId RevisionId) {
	u := currentUser(r.Context())
	var in RevisionApplyInput
	if r.ContentLength != 0 && r.Body != nil {
		if err := decodeJSON(r, &in); err != nil {
			s.fail(w, err)
			return
		}
	}
	ctx := r.Context()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		s.fail(w, err)
		return
	}
	defer tx.Rollback(ctx)
	q := s.q.WithTx(tx)

	rev, err := q.GetRevisionForUpdate(ctx, sqlcgen.GetRevisionForUpdateParams{ID: revisionId, UserID: u.ID})
	if err != nil {
		s.fail(w, err)
		return
	}
	if rev.Status != guild.RevisionProposed {
		s.fail(w, errConflict("this revision was already "+rev.Status))
		return
	}
	ch, err := q.GetChapterForUpdate(ctx, sqlcgen.GetChapterForUpdateParams{ID: rev.ChapterID, UserID: u.ID})
	if err != nil {
		s.fail(w, err)
		return
	}
	if ch.ContentHash != rev.BaseHash {
		s.fail(w, errConflict("the chapter changed since this revision was proposed; it cannot be applied any more. Discard it and ask for a new revision"))
		return
	}
	var hunks []text.Hunk
	if err := json.Unmarshal(rev.Hunks, &hunks); err != nil {
		s.fail(w, err)
		return
	}
	if len(hunks) == 0 {
		s.fail(w, errBadRequest("this revision changes nothing; discard it"))
		return
	}
	chosen := map[int]bool{}
	var applied []int
	if in.HunkIndexes != nil {
		if len(*in.HunkIndexes) == 0 {
			s.fail(w, errBadRequest("choose at least one hunk to apply, or discard the revision"))
			return
		}
		for _, i := range *in.HunkIndexes {
			if i < 0 || i >= len(hunks) {
				s.fail(w, errBadRequest("hunk %d does not exist", i))
				return
			}
			if !chosen[i] {
				chosen[i] = true
				applied = append(applied, i)
			}
		}
	} else {
		for i := range hunks {
			chosen[i] = true
			applied = append(applied, i)
		}
	}
	content, err := text.ApplyHunks(rev.BaseContentMd, hunks, func(h text.Hunk) bool { return chosen[h.Index] })
	if err != nil {
		s.fail(w, errConflict(err.Error()))
		return
	}
	if _, err := q.CreateChapterVersion(ctx, sqlcgen.CreateChapterVersionParams{
		UserID: u.ID, ChapterID: ch.ID, Kind: "pre_revision", Label: "Before revision", ContentMd: ch.ContentMd, ContentHash: ch.ContentHash,
	}); err != nil {
		s.fail(w, err)
		return
	}
	newHash := hashContent(content)
	updated, err := q.UpdateChapterContent(ctx, sqlcgen.UpdateChapterContentParams{ID: ch.ID, UserID: u.ID, ContentMd: content, ContentHash: newHash})
	if err != nil {
		s.fail(w, err)
		return
	}
	if err := q.TouchProject(ctx, updated.ProjectID); err != nil {
		s.fail(w, err)
		return
	}
	appliedJSON, _ := json.Marshal(applied)
	row, err := q.ApplyRevision(ctx, sqlcgen.ApplyRevisionParams{ID: rev.ID, UserID: u.ID, AppliedHunks: appliedJSON, ResultHash: newHash})
	if err != nil {
		s.fail(w, err)
		return
	}
	if err := tx.Commit(ctx); err != nil {
		s.fail(w, err)
		return
	}
	out, err := toRevision(row, newHash)
	if err != nil {
		s.fail(w, err)
		return
	}
	result := RevisionApplyResult{Chapter: toChapter(updated), Revision: out}
	// The bible keeper looks at what changed; a failure to start it never
	// undoes the revision.
	if bibleRun, err := s.startBibleUpdate(ctx, u, updated, &row); err != nil {
		s.log.Error("start bible update after revision", "revision", row.ID, "err", err)
	} else {
		result.BibleRunId = ptr(bibleRun.Row.ID)
	}
	writeJSON(w, http.StatusOK, result)
}

// DiscardRevision drops a proposed revision without touching the chapter.
func (s *Server) DiscardRevision(w http.ResponseWriter, r *http.Request, revisionId RevisionId) {
	u := currentUser(r.Context())
	rev, err := s.q.GetRevision(r.Context(), sqlcgen.GetRevisionParams{ID: revisionId, UserID: u.ID})
	if err != nil {
		s.fail(w, err)
		return
	}
	if rev.Status != guild.RevisionProposed {
		s.fail(w, errConflict("this revision was already "+rev.Status))
		return
	}
	row, err := s.q.DiscardRevision(r.Context(), sqlcgen.DiscardRevisionParams{ID: revisionId, UserID: u.ID})
	if err != nil {
		s.fail(w, err)
		return
	}
	ch, err := s.q.GetChapter(r.Context(), sqlcgen.GetChapterParams{ID: row.ChapterID, UserID: u.ID})
	if err != nil {
		s.fail(w, err)
		return
	}
	out, err := toRevision(row, ch.ContentHash)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

// toRevision converts a row; currentHash is the chapter's hash now, which
// decides staleness.
func toRevision(r sqlcgen.Revision, currentHash string) (Revision, error) {
	out := Revision{
		Id: r.ID, RunId: r.RunID, ChapterId: r.ChapterID, Status: RevisionStatus(r.Status), BaseHash: r.BaseHash, RevisedMd: r.RevisedMd,
		Stale: r.Status == guild.RevisionProposed && r.BaseHash != currentHash, ResultHash: r.ResultHash, CreatedAt: r.CreatedAt, DecidedAt: r.DecidedAt,
		Hunks: []DiffHunk{}, IssueIds: []uuid.UUID{}, Skipped: []SkippedIssue{},
	}
	if r.CritiqueRunID.Valid {
		out.CritiqueRunId = ptr(r.CritiqueRunID.UUID)
	}
	if len(r.Hunks) > 0 {
		if err := json.Unmarshal(r.Hunks, &out.Hunks); err != nil {
			return out, errors.New("decode revision hunks: " + err.Error())
		}
	}
	if len(r.Stats) > 0 {
		_ = json.Unmarshal(r.Stats, &out.Stats)
	}
	if len(r.IssueIds) > 0 {
		_ = json.Unmarshal(r.IssueIds, &out.IssueIds)
	}
	if len(r.Skipped) > 0 {
		_ = json.Unmarshal(r.Skipped, &out.Skipped)
	}
	if len(r.AppliedHunks) > 0 && string(r.AppliedHunks) != "null" {
		var applied []int
		_ = json.Unmarshal(r.AppliedHunks, &applied)
		out.AppliedHunks = &applied
	}
	if out.Hunks == nil {
		out.Hunks = []DiffHunk{}
	}
	return out, nil
}
