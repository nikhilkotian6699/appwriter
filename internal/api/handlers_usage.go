package api

import (
	"net/http"
	"time"

	"github.com/google/uuid"
)

// GetUsersUsage reports, per account, what it holds and what it used over
// the period; admins only.
func (s *Server) GetUsersUsage(w http.ResponseWriter, r *http.Request, params GetUsersUsageParams) {
	u := currentUser(r.Context())
	if err := requireAdmin(u); err != nil {
		s.fail(w, err)
		return
	}
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
	ctx := r.Context()
	users, err := s.q.ListUsers(ctx)
	if err != nil {
		s.fail(w, err)
		return
	}
	holdings, err := s.q.UsageHoldingsByUser(ctx)
	if err != nil {
		s.fail(w, err)
		return
	}
	runRows, err := s.q.UsageRunsByUser(ctx, since)
	if err != nil {
		s.fail(w, err)
		return
	}
	callRows, err := s.q.UsageCallsByUser(ctx, since)
	if err != nil {
		s.fail(w, err)
		return
	}
	byID := make(map[uuid.UUID]*AccountUsage, len(users))
	page := UsagePage{Period: period, Accounts: make([]AccountUsage, 0, len(users))}
	for _, row := range users {
		byID[row.ID] = &AccountUsage{User: toUser(row)}
	}
	for _, h := range holdings {
		if a, ok := byID[h.UserID]; ok {
			a.Usage.Projects, a.Usage.Chapters = int(h.Projects), int(h.Chapters)
		}
	}
	for _, rr := range runRows {
		if a, ok := byID[rr.UserID]; ok {
			a.Usage.Runs = int(rr.Runs)
			last := rr.LastRunAt
			a.LastRunAt = &last
		}
	}
	for _, cr := range callRows {
		if a, ok := byID[cr.UserID]; ok {
			a.Usage.ModelCalls = int(cr.Calls)
			a.Usage.PromptTokens, a.Usage.CompletionTokens = cr.PromptTokens, cr.CompletionTokens
			a.Usage.CostUsd, a.Usage.CostEstimated = cr.CostUsd, cr.CostEstimated
		}
	}
	for _, row := range users {
		a := byID[row.ID]
		page.Totals.Projects += a.Usage.Projects
		page.Totals.Chapters += a.Usage.Chapters
		page.Totals.Runs += a.Usage.Runs
		page.Totals.ModelCalls += a.Usage.ModelCalls
		page.Totals.PromptTokens += a.Usage.PromptTokens
		page.Totals.CompletionTokens += a.Usage.CompletionTokens
		page.Totals.CostUsd += a.Usage.CostUsd
		page.Totals.CostEstimated = page.Totals.CostEstimated || a.Usage.CostEstimated
		page.Accounts = append(page.Accounts, *a)
	}
	writeJSON(w, http.StatusOK, page)
}
