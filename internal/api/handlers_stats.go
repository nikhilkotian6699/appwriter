package api

import (
	"net/http"
	"sort"
	"time"

	"github.com/google/uuid"

	"writersguild/internal/db/sqlcgen"
	"writersguild/internal/guild"
)

// statsPeriods maps the period parameter to how far back to look; zero
// means all time.
var statsPeriods = map[string]time.Duration{"7d": 7 * 24 * time.Hour, "30d": 30 * 24 * time.Hour, "90d": 90 * 24 * time.Hour, "all": 0}

// GetWriterStats computes acceptance rates and cost per writer.
func (s *Server) GetWriterStats(w http.ResponseWriter, r *http.Request, params GetWriterStatsParams) {
	u := currentUser(r.Context())
	period := "all"
	if params.Period != nil {
		period = string(*params.Period)
	}
	window, ok := statsPeriods[period]
	if !ok {
		s.fail(w, errBadRequest("period must be 7d, 30d, 90d or all"))
		return
	}
	var since *time.Time
	if window > 0 {
		t := time.Now().Add(-window)
		since = &t
	}
	var project uuid.NullUUID
	if params.ProjectId != nil {
		if _, err := s.q.GetProject(r.Context(), sqlcgen.GetProjectParams{ID: *params.ProjectId, UserID: u.ID}); err != nil {
			s.fail(w, errNotFound("project"))
			return
		}
		project = uuid.NullUUID{UUID: *params.ProjectId, Valid: true}
	}
	ctx := r.Context()
	writers, err := s.q.ListWriters(ctx, u.ID)
	if err != nil {
		s.fail(w, err)
		return
	}
	issueRows, err := s.q.StatsIssuesByWriter(ctx, sqlcgen.StatsIssuesByWriterParams{UserID: u.ID, Since: since, ProjectID: project})
	if err != nil {
		s.fail(w, err)
		return
	}
	critiqueRows, err := s.q.StatsCritiquesByWriter(ctx, sqlcgen.StatsCritiquesByWriterParams{UserID: u.ID, Since: since, ProjectID: project})
	if err != nil {
		s.fail(w, err)
		return
	}
	draftRows, err := s.q.StatsDraftsByWriter(ctx, sqlcgen.StatsDraftsByWriterParams{UserID: u.ID, Since: since, ProjectID: project})
	if err != nil {
		s.fail(w, err)
		return
	}
	costRows, err := s.q.StatsCostByWriterKind(ctx, sqlcgen.StatsCostByWriterKindParams{UserID: u.ID, Since: since, ProjectID: project})
	if err != nil {
		s.fail(w, err)
		return
	}

	byID := make(map[uuid.UUID]*WriterStats, len(writers))
	page := WriterStatsPage{Period: period, Writers: make([]WriterStats, 0, len(writers)), Totals: KindCost{Kind: "all"}}
	if project.Valid {
		page.ProjectId = ptr(project.UUID)
	}
	for _, wr := range writers {
		roles := make([]WriterRole, 0, len(wr.Roles))
		for _, role := range wr.Roles {
			roles = append(roles, WriterRole(role))
		}
		byID[wr.ID] = &WriterStats{WriterId: wr.ID, Name: wr.Name, Slug: wr.Slug, ModelAlias: wr.ModelAlias, Enabled: wr.Enabled, IsSystem: wr.IsSystem, Roles: roles, ByKind: []KindCost{}}
	}
	for _, row := range issueRows {
		ws, ok := byID[row.WriterID]
		if !ok {
			continue
		}
		ws.Issues.Listed += int(row.Count)
		switch row.Decision {
		case "accepted":
			ws.Issues.Accepted += int(row.Count)
		case "rejected":
			ws.Issues.Rejected += int(row.Count)
		default:
			ws.Issues.Pending += int(row.Count)
		}
	}
	for _, row := range critiqueRows {
		if ws, ok := byID[row.WriterID.UUID]; ok && row.WriterID.Valid {
			ws.Critiques = int(row.Count)
		}
	}
	for _, row := range draftRows {
		ws, ok := byID[row.WriterID.UUID]
		if !ok || !row.WriterID.Valid {
			continue
		}
		ws.Drafts.Count += int(row.Count)
		switch row.Decision {
		case guild.DraftInserted, guild.DraftReplaced:
			ws.Drafts.Used += int(row.Count)
		case guild.DraftDiscarded:
			ws.Drafts.Discarded += int(row.Count)
		default:
			ws.Drafts.Pending += int(row.Count)
		}
	}
	for _, row := range costRows {
		kc := KindCost{Kind: row.Kind, Calls: int(row.Calls), CostUsd: row.CostUsd, CostEstimated: row.CostEstimated, PromptTokens: row.PromptTokens, CompletionTokens: row.CompletionTokens}
		page.Totals.Calls += kc.Calls
		page.Totals.CostUsd += kc.CostUsd
		page.Totals.CostEstimated = page.Totals.CostEstimated || kc.CostEstimated
		page.Totals.PromptTokens += kc.PromptTokens
		page.Totals.CompletionTokens += kc.CompletionTokens
		if !row.WriterID.Valid {
			continue // a writer since deleted: counted in the totals only
		}
		ws, ok := byID[row.WriterID.UUID]
		if !ok {
			continue
		}
		ws.ByKind = append(ws.ByKind, kc)
		ws.Calls += kc.Calls
		ws.CostUsd += kc.CostUsd
		ws.CostEstimated = ws.CostEstimated || kc.CostEstimated
		ws.PromptTokens += kc.PromptTokens
		ws.CompletionTokens += kc.CompletionTokens
	}
	for _, wr := range writers {
		ws := byID[wr.ID]
		ws.Issues.AcceptanceRate = rate(ws.Issues.Accepted, ws.Issues.Rejected)
		ws.Drafts.AcceptanceRate = rate(ws.Drafts.Used, ws.Drafts.Discarded)
		sort.Slice(ws.ByKind, func(a, b int) bool { return ws.ByKind[a].CostUsd > ws.ByKind[b].CostUsd })
		page.Writers = append(page.Writers, *ws)
	}
	// Busiest writers first; system agents and idle writers after.
	sort.SliceStable(page.Writers, func(a, b int) bool {
		if page.Writers[a].CostUsd != page.Writers[b].CostUsd {
			return page.Writers[a].CostUsd > page.Writers[b].CostUsd
		}
		return page.Writers[a].Name < page.Writers[b].Name
	})
	writeJSON(w, http.StatusOK, page)
}

// rate is yes / (yes + no), or nil when nothing was decided.
func rate(yes, no int) *float64 {
	if yes+no == 0 {
		return nil
	}
	return ptr(float64(yes) / float64(yes+no))
}
