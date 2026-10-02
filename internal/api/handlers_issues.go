package api

import (
	"net/http"

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
