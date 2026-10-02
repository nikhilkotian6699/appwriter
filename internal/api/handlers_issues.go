package api

import (
	"net/http"
	"strings"

	"writersguild/internal/db/sqlcgen"
	"writersguild/internal/guild"
)

// ListRunIssues returns the editor-in-chief's prioritized list for a run.
func (s *Server) ListRunIssues(w http.ResponseWriter, r *http.Request, runId RunId) {
	u := currentUser(r.Context())
	if _, err := s.q.GetRun(r.Context(), sqlcgen.GetRunParams{ID: runId, UserID: u.ID}); err != nil {
		s.fail(w, err)
		return
	}
	rows, err := s.q.ListRunIssues(r.Context(), sqlcgen.ListRunIssuesParams{RunID: runId, UserID: u.ID})
	if err != nil {
		s.fail(w, err)
		return
	}
	out := make([]Issue, 0, len(rows))
	for _, row := range rows {
		out = append(out, toIssue(row))
	}
	writeJSON(w, http.StatusOK, out)
}

func toIssue(r sqlcgen.Issue) Issue {
	st := guild.ToStoredIssue(r)
	out := Issue{
		Id: r.ID, RunId: r.RunID, Position: st.Position, Key: st.Key, Severity: IssueSeverity(st.Severity), Quote: st.Quote, Problem: st.Problem,
		SuggestedFix: st.SuggestedFix, Start: st.Start, End: st.End, QuoteExact: st.QuoteExact, Decision: IssueDecision(st.Decision),
		EditedFix: r.EditedFix, DecidedAt: r.DecidedAt, ContentHash: r.ContentHash, CreatedAt: r.CreatedAt, Sources: make([]IssueSource, 0, len(st.Sources)),
	}
	if r.ChapterID.Valid {
		out.ChapterId = ptr(r.ChapterID.UUID)
	}
	for _, src := range st.Sources {
		out.Sources = append(out.Sources, IssueSource{Id: src.ID, CritiqueId: src.CritiqueID, WriterId: src.WriterID, WriterName: src.WriterName, WriterSlug: src.WriterSlug, IssueId: src.IssueID})
	}
	return out
}

var issueDecisions = map[string]bool{"pending": true, "accepted": true, "rejected": true}

// DecideIssue records the author's decision on one issue of the
// editor-in-chief's list: accept, reject, accept with an edited fix, or
// pending to undo.
func (s *Server) DecideIssue(w http.ResponseWriter, r *http.Request, issueId IssueId) {
	u := currentUser(r.Context())
	var in IssueDecisionInput
	if err := decodeJSON(r, &in); err != nil {
		s.fail(w, err)
		return
	}
	decision := string(in.Decision)
	if !issueDecisions[decision] {
		s.fail(w, errBadRequest("decision must be pending, accepted or rejected"))
		return
	}
	cur, err := s.q.GetIssue(r.Context(), sqlcgen.GetIssueParams{ID: issueId, UserID: u.ID})
	if err != nil {
		s.fail(w, err)
		return
	}
	edited := cur.EditedFix
	if in.EditedFix != nil {
		t := strings.TrimSpace(*in.EditedFix)
		if len(t) > 5000 {
			s.fail(w, errBadRequest("edited_fix must be at most 5000 characters"))
			return
		}
		if t == "" {
			edited = nil
		} else {
			edited = &t
		}
	}
	row, err := s.q.SetIssueDecision(r.Context(), sqlcgen.SetIssueDecisionParams{ID: issueId, UserID: u.ID, Decision: decision, EditedFix: edited})
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toIssue(row))
}
