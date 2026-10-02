package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/google/uuid"

	"writersguild/internal/db/sqlcgen"
	"writersguild/internal/runs"
	"writersguild/internal/text"
)

// GetChapterHistory lists a chapter's runs, newest first, with a summary and
// the counts behind it, plus totals over every matching run.
func (s *Server) GetChapterHistory(w http.ResponseWriter, r *http.Request, chapterId ChapterId, params GetChapterHistoryParams) {
	u := currentUser(r.Context())
	ch, err := s.q.GetChapter(r.Context(), sqlcgen.GetChapterParams{ID: chapterId, UserID: u.ID})
	if err != nil {
		s.fail(w, err)
		return
	}
	kind := ""
	if params.Kind != nil {
		kind = string(*params.Kind)
		if !runKinds[kind] {
			s.fail(w, errBadRequest("unknown run kind %q", kind))
			return
		}
	}
	limit := 30
	if params.Limit != nil {
		limit = *params.Limit
	}
	if limit < 1 || limit > 100 {
		s.fail(w, errBadRequest("limit must be between 1 and 100"))
		return
	}
	ctx := r.Context()
	chapterID := nullUUID(&ch.ID)
	rows, err := s.q.ListChapterHistory(ctx, sqlcgen.ListChapterHistoryParams{ChapterID: chapterID, UserID: u.ID, Kind: kind, Before: params.Before, RowLimit: int32(limit)})
	if err != nil {
		s.fail(w, err)
		return
	}
	totals, err := s.q.ChapterRunTotals(ctx, sqlcgen.ChapterRunTotalsParams{ChapterID: chapterID, UserID: u.ID, Kind: kind})
	if err != nil {
		s.fail(w, err)
		return
	}
	kindRows, err := s.q.ChapterRunKindCounts(ctx, sqlcgen.ChapterRunKindCountsParams{ChapterID: chapterID, UserID: u.ID})
	if err != nil {
		s.fail(w, err)
		return
	}
	ids := make([]uuid.UUID, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.ID)
	}
	agg, err := s.loadRunAggregates(ctx, ids)
	if err != nil {
		s.fail(w, err)
		return
	}
	page := HistoryPage{
		Items:  make([]RunHistoryItem, 0, len(rows)),
		Totals: HistoryTotals{Runs: int(totals.Runs), CostUsd: totals.CostUsd, CostEstimated: totals.CostEstimated, PromptTokens: totals.PromptTokens, CompletionTokens: totals.CompletionTokens},
		Kinds:  make([]HistoryKindCount, 0, len(kindRows)),
	}
	for _, k := range kindRows {
		page.Kinds = append(page.Kinds, HistoryKindCount{Kind: RunKind(k.Kind), Count: int(k.Count)})
	}
	for _, row := range rows {
		page.Items = append(page.Items, agg.item(row))
	}
	if len(rows) == limit {
		last := rows[len(rows)-1].CreatedAt
		page.NextBefore = &last
	}
	writeJSON(w, http.StatusOK, page)
}

// runAggregates holds the per-run counts for one page of runs.
type runAggregates struct {
	critiques map[uuid.UUID]sqlcgen.CritiqueCountsByRunRow
	issues    map[uuid.UUID]sqlcgen.IssueCountsByRunRow
	revisions map[uuid.UUID]sqlcgen.RevisionsByRunRow
	drafts    map[uuid.UUID]sqlcgen.DraftCountsByRunRow
	proposals map[uuid.UUID]sqlcgen.ProposalCountsByRunRow
	writers   map[uuid.UUID]sqlcgen.WritersByRunRow
}

func (s *Server) loadRunAggregates(c context.Context, ids []uuid.UUID) (*runAggregates, error) {
	a := &runAggregates{
		critiques: map[uuid.UUID]sqlcgen.CritiqueCountsByRunRow{}, issues: map[uuid.UUID]sqlcgen.IssueCountsByRunRow{},
		revisions: map[uuid.UUID]sqlcgen.RevisionsByRunRow{}, drafts: map[uuid.UUID]sqlcgen.DraftCountsByRunRow{},
		proposals: map[uuid.UUID]sqlcgen.ProposalCountsByRunRow{}, writers: map[uuid.UUID]sqlcgen.WritersByRunRow{},
	}
	if len(ids) == 0 {
		return a, nil
	}
	if rows, err := s.q.CritiqueCountsByRun(c, ids); err != nil {
		return nil, err
	} else {
		for _, r := range rows {
			a.critiques[r.RunID] = r
		}
	}
	if rows, err := s.q.IssueCountsByRun(c, ids); err != nil {
		return nil, err
	} else {
		for _, r := range rows {
			a.issues[r.RunID] = r
		}
	}
	if rows, err := s.q.RevisionsByRun(c, ids); err != nil {
		return nil, err
	} else {
		for _, r := range rows {
			a.revisions[r.RunID] = r
		}
	}
	if rows, err := s.q.DraftCountsByRun(c, ids); err != nil {
		return nil, err
	} else {
		for _, r := range rows {
			a.drafts[r.RunID] = r
		}
	}
	if rows, err := s.q.ProposalCountsByRun(c, ids); err != nil {
		return nil, err
	} else {
		for _, r := range rows {
			a.proposals[r.RunID] = r
		}
	}
	if rows, err := s.q.WritersByRun(c, ids); err != nil {
		return nil, err
	} else {
		for _, r := range rows {
			if r.RunID.Valid {
				a.writers[r.RunID.UUID] = r
			}
		}
	}
	return a, nil
}

// item builds the history entry for one run.
func (a *runAggregates) item(row sqlcgen.Run) RunHistoryItem {
	it := RunHistoryItem{Run: toRun(row), Writers: []string{}}
	if wr, ok := a.writers[row.ID]; ok {
		it.ModelCalls = int(wr.Calls)
		names := append([]string(nil), wr.WriterNames...)
		sort.Strings(names)
		it.Writers = names
	}
	var parts []string
	switch row.Kind {
	case runs.KindCritique:
		cs := CritiqueSummary{Synthesis: "skipped"}
		if c, ok := a.critiques[row.ID]; ok {
			cs.Critics, cs.Failed = int(c.Critics), int(c.Failed)
		}
		if i, ok := a.issues[row.ID]; ok {
			cs.Issues, cs.Accepted, cs.Rejected, cs.Pending = int(i.Issues), int(i.Accepted), int(i.Rejected), int(i.Pending)
		}
		if v := it.Run.Result; v != nil {
			if syn, ok := (*v)["synthesis"].(string); ok && syn != "" {
				cs.Synthesis = syn
			}
		}
		it.Critique = &cs
		parts = append(parts, fmt.Sprintf("%d critic%s", cs.Critics, plural(cs.Critics)))
		if cs.Failed > 0 {
			parts = append(parts, fmt.Sprintf("%d failed", cs.Failed))
		}
		parts = append(parts, fmt.Sprintf("%d issue%s", cs.Issues, plural(cs.Issues)))
		if cs.Issues > 0 {
			parts = append(parts, fmt.Sprintf("%d accepted", cs.Accepted), fmt.Sprintf("%d rejected", cs.Rejected))
		}
		if cs.Synthesis == "fallback" {
			parts = append(parts, "list assembled without the editor-in-chief")
		}
	case runs.KindRevision:
		if rv, ok := a.revisions[row.ID]; ok {
			rs := RevisionSummary{Status: RevisionStatus(rv.Status), Hunks: int(rv.Hunks), AppliedHunks: int(rv.AppliedHunks)}
			var st text.DiffStats
			_ = json.Unmarshal(rv.Stats, &st)
			rs.WordsAdded, rs.WordsRemoved = st.WordsAdded, st.WordsRemoved
			it.Revision = &rs
			parts = append(parts, fmt.Sprintf("%d change%s proposed", rs.Hunks, plural(rs.Hunks)))
			switch rs.Status {
			case "applied":
				parts = append(parts, fmt.Sprintf("%d applied", rs.AppliedHunks))
			case "discarded":
				parts = append(parts, "discarded")
			default:
				parts = append(parts, "awaiting review")
			}
		}
	case runs.KindBibleUpdate:
		if p, ok := a.proposals[row.ID]; ok {
			bs := BibleSummary{Proposals: int(p.Proposals), Approved: int(p.Approved), Rejected: int(p.Rejected), Pending: int(p.Pending)}
			it.Bible = &bs
			parts = append(parts, fmt.Sprintf("%d proposal%s", bs.Proposals, plural(bs.Proposals)))
			if bs.Proposals > 0 {
				parts = append(parts, fmt.Sprintf("%d approved", bs.Approved), fmt.Sprintf("%d rejected", bs.Rejected))
				if bs.Pending > 0 {
					parts = append(parts, fmt.Sprintf("%d pending", bs.Pending))
				}
			}
		} else if row.Status == runs.StatusSucceeded {
			parts = append(parts, "nothing to change in the bible")
		}
	case runs.KindCowrite, runs.KindCompare:
		if d, ok := a.drafts[row.ID]; ok {
			ds := DraftsSummary{Count: int(d.Count), Inserted: int(d.Inserted), Replaced: int(d.Replaced), Discarded: int(d.Discarded), Pending: int(d.Pending)}
			it.Drafts = &ds
			if row.Kind == runs.KindCompare {
				parts = append(parts, fmt.Sprintf("%d drafts compared", ds.Count))
			} else {
				parts = append(parts, "1 draft")
			}
			used := ds.Inserted + ds.Replaced
			switch {
			case used > 0 && ds.Count == 1:
				if ds.Replaced > 0 {
					parts = append(parts, "replaced the selection")
				} else {
					parts = append(parts, "inserted")
				}
			case used > 0:
				parts = append(parts, fmt.Sprintf("%d used", used))
			}
			if ds.Discarded > 0 {
				parts = append(parts, fmt.Sprintf("%d discarded", ds.Discarded))
			}
			if ds.Pending > 0 {
				parts = append(parts, fmt.Sprintf("%d undecided", ds.Pending))
			}
		}
		if instr, ok := it.Run.Params["instruction"].(string); ok && instr != "" {
			parts = append(parts, fmt.Sprintf("“%s”", truncateRunes(instr, 60)))
		}
	case runs.KindWriterTest:
		alias, _ := it.Run.Params["model_alias"].(string)
		if alias != "" {
			parts = append(parts, "test of "+alias)
		} else {
			parts = append(parts, "writer test")
		}
	}
	if row.Status == runs.StatusFailed {
		msg := row.Error
		if msg == "" {
			msg = "failed"
		}
		parts = append([]string{"Failed: " + msg}, parts...)
	} else if row.Status == runs.StatusCancelled {
		parts = append([]string{"Cancelled"}, parts...)
	} else if row.Status == runs.StatusRunning {
		parts = append([]string{"In progress"}, parts...)
	}
	it.Summary = strings.Join(parts, " · ")
	return it
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

// ListRunCalls returns the gateway calls a run made.
func (s *Server) ListRunCalls(w http.ResponseWriter, r *http.Request, runId RunId) {
	u := currentUser(r.Context())
	if _, err := s.q.GetRun(r.Context(), sqlcgen.GetRunParams{ID: runId, UserID: u.ID}); err != nil {
		s.fail(w, err)
		return
	}
	rows, err := s.q.ListRunModelCalls(r.Context(), sqlcgen.ListRunModelCallsParams{RunID: nullUUID(&runId), UserID: u.ID})
	if err != nil {
		s.fail(w, err)
		return
	}
	out := make([]ModelCall, 0, len(rows))
	for _, row := range rows {
		mc := ModelCall{
			Id: row.ID, GenerationName: row.GenerationName, ModelAlias: row.ModelAlias, PromptTokens: int(row.PromptTokens), CompletionTokens: int(row.CompletionTokens),
			CostUsd: row.CostUsd, CostEstimated: row.CostEstimated, LatencyMs: int(row.LatencyMs), Status: ModelCallStatus(row.Status), Error: row.Error, CreatedAt: row.CreatedAt,
		}
		if row.RunID.Valid {
			mc.RunId = ptr(row.RunID.UUID)
		}
		if row.WriterID.Valid {
			mc.WriterId = ptr(row.WriterID.UUID)
		}
		if row.WriterName != nil {
			mc.WriterName = row.WriterName
		}
		out = append(out, mc)
	}
	writeJSON(w, http.StatusOK, out)
}
