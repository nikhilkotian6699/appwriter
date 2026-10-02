package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"writersguild/internal/db/sqlcgen"
	"writersguild/internal/guild"
	"writersguild/internal/runs"
)

// StartBibleUpdate asks the bible keeper what the chapter, as it stands,
// changes in the story bible.
func (s *Server) StartBibleUpdate(w http.ResponseWriter, r *http.Request, chapterId ChapterId) {
	u := currentUser(r.Context())
	var in BibleUpdateStartInput
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
	var revision *sqlcgen.Revision
	if in.RevisionId != nil {
		rev, err := s.q.GetRevision(r.Context(), sqlcgen.GetRevisionParams{ID: *in.RevisionId, UserID: u.ID})
		if err != nil {
			s.fail(w, errNotFound("revision"))
			return
		}
		if rev.ChapterID != ch.ID || rev.Status != guild.RevisionApplied {
			s.fail(w, errBadRequest("revision_id must be an applied revision of this chapter"))
			return
		}
		revision = &rev
	}
	run, err := s.startBibleUpdate(r.Context(), u, ch, revision)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, toRun(run.Row))
}

// startBibleUpdate loads what the keeper needs and launches the run.
func (s *Server) startBibleUpdate(ctx context.Context, u sqlcgen.User, ch sqlcgen.Chapter, revision *sqlcgen.Revision) (*runs.Run, error) {
	project, err := s.q.GetProject(ctx, sqlcgen.GetProjectParams{ID: ch.ProjectID, UserID: u.ID})
	if err != nil {
		return nil, err
	}
	bible, err := s.q.ListBibleEntries(ctx, sqlcgen.ListBibleEntriesParams{ProjectID: ch.ProjectID, UserID: u.ID})
	if err != nil {
		return nil, err
	}
	if err := s.q.EnsureSettings(ctx, u.ID); err != nil {
		return nil, err
	}
	settings, err := s.q.GetSettings(ctx, u.ID)
	if err != nil {
		return nil, err
	}
	return s.guild.BibleUpdate(ctx, u, guild.BibleUpdateInput{Project: project, Chapter: ch, Bible: bible, Revision: revision, SceneTokenLimit: int(settings.SceneTokenLimit)})
}

// ListProjectBibleProposals lists a project's proposals, newest first.
func (s *Server) ListProjectBibleProposals(w http.ResponseWriter, r *http.Request, projectId ProjectId, params ListProjectBibleProposalsParams) {
	u := currentUser(r.Context())
	if _, err := s.q.GetProject(r.Context(), sqlcgen.GetProjectParams{ID: projectId, UserID: u.ID}); err != nil {
		s.fail(w, err)
		return
	}
	status := ""
	if params.Status != nil {
		status = string(*params.Status)
		if status != guild.ProposalPending && status != guild.ProposalApproved && status != guild.ProposalRejected {
			s.fail(w, errBadRequest("unknown proposal status %q", status))
			return
		}
	}
	limit := 50
	if params.Limit != nil {
		limit = *params.Limit
	}
	if limit < 1 || limit > 200 {
		s.fail(w, errBadRequest("limit must be between 1 and 200"))
		return
	}
	rows, err := s.q.ListProjectBibleProposals(r.Context(), sqlcgen.ListProjectBibleProposalsParams{ProjectID: projectId, UserID: u.ID, Status: status, RowLimit: int32(limit)})
	if err != nil {
		s.fail(w, err)
		return
	}
	s.writeProposals(w, r, u, rows)
}

// ListRunBibleProposals lists what one bible update run proposed.
func (s *Server) ListRunBibleProposals(w http.ResponseWriter, r *http.Request, runId RunId) {
	u := currentUser(r.Context())
	if _, err := s.q.GetRun(r.Context(), sqlcgen.GetRunParams{ID: runId, UserID: u.ID}); err != nil {
		s.fail(w, err)
		return
	}
	rows, err := s.q.ListRunBibleProposals(r.Context(), sqlcgen.ListRunBibleProposalsParams{RunID: runId, UserID: u.ID})
	if err != nil {
		s.fail(w, err)
		return
	}
	s.writeProposals(w, r, u, rows)
}

func (s *Server) writeProposals(w http.ResponseWriter, r *http.Request, u sqlcgen.User, rows []sqlcgen.BibleProposal) {
	out := make([]BibleProposal, 0, len(rows))
	for _, row := range rows {
		out = append(out, s.toBibleProposal(r.Context(), u, row))
	}
	writeJSON(w, http.StatusOK, out)
}

// DecideBibleProposal approves (optionally with edits) or rejects a proposal.
// Approval applies it to the story bible in the same transaction.
func (s *Server) DecideBibleProposal(w http.ResponseWriter, r *http.Request, proposalId ProposalId) {
	u := currentUser(r.Context())
	var in BibleProposalDecisionInput
	if err := decodeJSON(r, &in); err != nil {
		s.fail(w, err)
		return
	}
	decision := string(in.Decision)
	if decision != guild.ProposalApproved && decision != guild.ProposalRejected {
		s.fail(w, errBadRequest("decision must be approved or rejected"))
		return
	}
	ctx := r.Context()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		s.fail(w, err)
		return
	}
	defer tx.Rollback(ctx)
	q := s.q.WithTx(tx)

	p, err := q.GetBibleProposalForUpdate(ctx, sqlcgen.GetBibleProposalForUpdateParams{ID: proposalId, UserID: u.ID})
	if err != nil {
		s.fail(w, err)
		return
	}
	if p.Status != guild.ProposalPending {
		s.fail(w, errConflict("this proposal was already "+p.Status))
		return
	}
	title := p.Title
	if in.Title != nil {
		title = strings.TrimSpace(*in.Title)
		if len(title) > 300 {
			s.fail(w, errBadRequest("title must be at most 300 characters"))
			return
		}
	}
	fields := map[string]string{}
	_ = json.Unmarshal(p.Fields, &fields)
	if in.Fields != nil {
		fields = map[string]string{}
		for k, v := range *in.Fields {
			k = strings.TrimSpace(k)
			if k != "" {
				fields[k] = strings.TrimSpace(v)
			}
		}
	}
	fieldsJSON, _ := json.Marshal(fields)
	applied := uuid.NullUUID{}
	if decision == guild.ProposalApproved {
		switch p.Action {
		case guild.ProposalAdd:
			if title == "" && len(fields) == 0 {
				s.fail(w, errBadRequest("an addition needs a title or fields"))
				return
			}
			pos, err := q.NextBibleEntryPosition(ctx, sqlcgen.NextBibleEntryPositionParams{ProjectID: p.ProjectID, Section: p.Section})
			if err != nil {
				s.fail(w, err)
				return
			}
			chapterID := uuid.NullUUID{}
			if p.Section == "chapter_summary" {
				chapterID = p.ChapterID
			}
			entry, err := q.CreateBibleEntry(ctx, sqlcgen.CreateBibleEntryParams{UserID: u.ID, ProjectID: p.ProjectID, Section: p.Section, Title: title, Fields: fieldsJSON, ChapterID: chapterID, Position: pos})
			if err != nil {
				s.fail(w, err)
				return
			}
			applied = uuid.NullUUID{UUID: entry.ID, Valid: true}
		case guild.ProposalUpdate:
			if !p.EntryID.Valid {
				s.fail(w, errConflict("the entry this proposal updates no longer exists"))
				return
			}
			cur, err := q.GetBibleEntry(ctx, sqlcgen.GetBibleEntryParams{ID: p.EntryID.UUID, UserID: u.ID})
			if errors.Is(err, pgx.ErrNoRows) {
				s.fail(w, errConflict("the entry this proposal updates no longer exists"))
				return
			}
			if err != nil {
				s.fail(w, err)
				return
			}
			if _, err := q.UpdateBibleEntry(ctx, sqlcgen.UpdateBibleEntryParams{ID: cur.ID, UserID: u.ID, Title: title, Fields: fieldsJSON, ChapterID: cur.ChapterID, Position: cur.Position}); err != nil {
				s.fail(w, err)
				return
			}
			applied = uuid.NullUUID{UUID: cur.ID, Valid: true}
		case guild.ProposalDelete:
			if !p.EntryID.Valid {
				s.fail(w, errConflict("the entry this proposal deletes no longer exists"))
				return
			}
			n, err := q.DeleteBibleEntry(ctx, sqlcgen.DeleteBibleEntryParams{ID: p.EntryID.UUID, UserID: u.ID})
			if err != nil {
				s.fail(w, err)
				return
			}
			if n == 0 {
				s.fail(w, errConflict("the entry this proposal deletes no longer exists"))
				return
			}
			applied = p.EntryID
		}
		if err := q.TouchProject(ctx, p.ProjectID); err != nil {
			s.fail(w, err)
			return
		}
	}
	row, err := q.DecideBibleProposal(ctx, sqlcgen.DecideBibleProposalParams{ID: p.ID, UserID: u.ID, Status: decision, Title: title, Fields: fieldsJSON, AppliedEntryID: applied})
	if err != nil {
		s.fail(w, err)
		return
	}
	if err := tx.Commit(ctx); err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, s.toBibleProposal(ctx, u, row))
}

// toBibleProposal converts a row and attaches the entry it concerns, when
// it still exists.
func (s *Server) toBibleProposal(ctx context.Context, u sqlcgen.User, r sqlcgen.BibleProposal) BibleProposal {
	st := guild.ToStoredProposal(r)
	out := BibleProposal{
		Id: r.ID, RunId: r.RunID, ProjectId: r.ProjectID, Action: ProposalAction(st.Action), Section: BibleSection(st.Section), Title: st.Title, Fields: st.Fields,
		Rationale: st.Rationale, Status: ProposalStatus(st.Status), Position: st.Position, CreatedAt: r.CreatedAt, DecidedAt: r.DecidedAt,
	}
	out.ChapterId = st.ChapterID
	out.RevisionId = st.RevisionID
	out.EntryId = st.EntryID
	if r.AppliedEntryID.Valid {
		out.AppliedEntryId = ptr(r.AppliedEntryID.UUID)
	}
	if r.EntryID.Valid {
		if cur, err := s.q.GetBibleEntry(ctx, sqlcgen.GetBibleEntryParams{ID: r.EntryID.UUID, UserID: u.ID}); err == nil {
			e := toBibleEntry(cur)
			out.Current = &e
		}
	}
	return out
}
