package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"writersguild/internal/db/sqlcgen"
	"writersguild/internal/runs"
)

// isolationFixtures is everything account A owns that account B must never
// reach.
type isolationFixtures struct {
	project, chapter, version, entry, writer, run, critique, issue, revision, draft, proposal uuid.UUID
}

// makeFixtures gives account A one of everything, through the API where it
// exists and through the queries for rows that only workflows create.
func makeFixtures(t *testing.T, a *env) isolationFixtures {
	t.Helper()
	ctx := context.Background()
	var f isolationFixtures
	var p Project
	a.want(a.do("POST", "/api/projects", ProjectInput{Name: "A's novel"}, &p), 201, "POST", "/api/projects")
	f.project = p.Id
	var ch Chapter
	a.want(a.do("POST", "/api/projects/"+p.Id.String()+"/chapters", ChapterCreateInput{Title: "Secret chapter", ContentMd: ptr("The lantern had been lit. Nobody asked where.")}, &ch), 201, "POST", "chapter")
	f.chapter = ch.Id
	var v ChapterVersion
	a.want(a.do("POST", "/api/chapters/"+ch.Id.String()+"/versions", SnapshotInput{Label: ptr("mine")}, &v), 201, "POST", "snapshot")
	f.version = v.Id
	var be BibleEntry
	a.want(a.do("POST", "/api/projects/"+p.Id.String()+"/bible", BibleEntryInput{Section: "character", Title: "Mara", Fields: map[string]string{"role": "lead"}}, &be), 201, "POST", "bible")
	f.entry = be.Id
	wr := a.createWriter("A's critic", "writersguild-a", []WriterRole{"critic", "co-writer"}, true)
	f.writer = wr.Id

	run, err := a.engine.Tracker().Start(ctx, runs.StartParams{User: a.user, Kind: runs.KindCritique, ProjectID: uuid.NullUUID{UUID: p.Id, Valid: true}, ChapterID: uuid.NullUUID{UUID: ch.Id, Valid: true}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.engine.Tracker().Finish(ctx, run, map[string]int{"issues": 1}, nil); err != nil {
		t.Fatal(err)
	}
	f.run = run.Row.ID
	crit, err := a.q.CreateCritique(ctx, sqlcgen.CreateCritiqueParams{UserID: a.user.ID, RunID: run.Row.ID, ChapterID: run.Row.ChapterID, WriterID: uuid.NullUUID{UUID: wr.Id, Valid: true}, WriterName: wr.Name, WriterSlug: wr.Slug, ModelAlias: wr.ModelAlias, SceneCount: 1})
	if err != nil {
		t.Fatal(err)
	}
	f.critique = crit.ID
	issue, err := a.q.CreateIssue(ctx, sqlcgen.CreateIssueParams{UserID: a.user.ID, RunID: run.Row.ID, ChapterID: run.Row.ChapterID, Position: 0, Key: "e1", Severity: "high", Quote: "Nobody asked where.", Problem: "p", SuggestedFix: "f", QuoteStart: 26, QuoteEnd: 45, QuoteExact: true, Sources: []byte(`[]`), ContentHash: ch.ContentHash})
	if err != nil {
		t.Fatal(err)
	}
	f.issue = issue.ID
	rev, err := a.q.CreateRevision(ctx, sqlcgen.CreateRevisionParams{UserID: a.user.ID, RunID: run.Row.ID, ChapterID: ch.Id, CritiqueRunID: uuid.NullUUID{UUID: run.Row.ID, Valid: true}, BaseHash: ch.ContentHash, BaseContentMd: ch.ContentMd, RevisedMd: ch.ContentMd + " More.", Hunks: []byte(`[]`), Stats: []byte(`{}`), IssueIds: []byte(`[]`), Skipped: []byte(`[]`)})
	if err != nil {
		t.Fatal(err)
	}
	f.revision = rev.ID
	draft, err := a.q.CreateDraft(ctx, sqlcgen.CreateDraftParams{UserID: a.user.ID, RunID: run.Row.ID, ChapterID: run.Row.ChapterID, WriterID: uuid.NullUUID{UUID: wr.Id, Valid: true}, WriterName: wr.Name, WriterSlug: wr.Slug, ModelAlias: wr.ModelAlias, Mode: "continue", Instruction: "go", ContentHash: ch.ContentHash})
	if err != nil {
		t.Fatal(err)
	}
	f.draft = draft.ID
	prop, err := a.q.CreateBibleProposal(ctx, sqlcgen.CreateBibleProposalParams{UserID: a.user.ID, RunID: run.Row.ID, ProjectID: p.Id, ChapterID: run.Row.ChapterID, Action: "add", Section: "character", Title: "New", Fields: []byte(`{}`), Rationale: "r"})
	if err != nil {
		t.Fatal(err)
	}
	f.proposal = prop.ID
	return f
}

// probe says how account B calls one route with A's identifiers and what
// must come back.
type probe struct {
	path string
	body any
	// expect is "404" (A's thing is invisible), "403" (not B's role),
	// "public" (no account involved), or "own" (B sees only B's data: the
	// reply must not mention any of A's identifiers).
	expect string
}

// isolationProbes must name every API route. A route without a probe fails
// the test, so a new endpoint cannot ship without saying how it keeps
// accounts apart.
func isolationProbes(f isolationFixtures) map[string]probe {
	id := func(u uuid.UUID) string { return u.String() }
	return map[string]probe{
		"GET /api/healthz":          {path: "/api/healthz", expect: "public"},
		"POST /api/auth/login":      {path: "/api/auth/login", body: map[string]string{"username": "nobody", "password": "nothing"}, expect: "public"},
		"POST /api/auth/logout":     {path: "/api/auth/logout", expect: "public"},
		"GET /api/me":               {path: "/api/me", expect: "own"},
		"GET /api/settings":         {path: "/api/settings", expect: "own"},
		"PUT /api/settings":         {path: "/api/settings", body: SettingsInput{SceneTokenLimit: 6000, AutosaveSnapshotMinutes: 10}, expect: "own"},
		"GET /api/gateway/models":   {path: "/api/gateway/models", expect: "own"},
		"GET /api/stats/writers":    {path: "/api/stats/writers", expect: "own"},
		"GET /api/users":            {path: "/api/users", expect: "403"},
		"POST /api/users":           {path: "/api/users", body: UserCreateInput{Username: "intruder", Password: "first password 1", Role: "author"}, expect: "403"},
		"GET /api/users/usage":      {path: "/api/users/usage", expect: "403"},
		"PUT /api/account":          {path: "/api/account", body: AccountInput{DisplayName: "B"}, expect: "own"},
		"PUT /api/account/password": {path: "/api/account/password", body: PasswordChangeInput{CurrentPassword: "x", NewPassword: "long enough 1"}, expect: "own"},

		"GET /api/projects":                             {path: "/api/projects", expect: "own"},
		"POST /api/projects":                            {path: "/api/projects", body: ProjectInput{Name: "B's novel"}, expect: "own"},
		"GET /api/projects/{projectId}":                 {path: "/api/projects/" + id(f.project), expect: "404"},
		"PUT /api/projects/{projectId}":                 {path: "/api/projects/" + id(f.project), body: ProjectInput{Name: "Taken over"}, expect: "404"},
		"DELETE /api/projects/{projectId}":              {path: "/api/projects/" + id(f.project), expect: "404"},
		"GET /api/projects/{projectId}/chapters":        {path: "/api/projects/" + id(f.project) + "/chapters", expect: "404"},
		"POST /api/projects/{projectId}/chapters":       {path: "/api/projects/" + id(f.project) + "/chapters", body: ChapterCreateInput{Title: "Planted"}, expect: "404"},
		"GET /api/projects/{projectId}/bible":           {path: "/api/projects/" + id(f.project) + "/bible", expect: "404"},
		"POST /api/projects/{projectId}/bible":          {path: "/api/projects/" + id(f.project) + "/bible", body: BibleEntryInput{Section: "premise", Title: "x"}, expect: "404"},
		"GET /api/projects/{projectId}/bible/proposals": {path: "/api/projects/" + id(f.project) + "/bible/proposals", expect: "404"},
		"PUT /api/bible-entries/{entryId}":              {path: "/api/bible-entries/" + id(f.entry), body: BibleEntryInput{Section: "character", Title: "Renamed"}, expect: "404"},
		"DELETE /api/bible-entries/{entryId}":           {path: "/api/bible-entries/" + id(f.entry), expect: "404"},
		"PUT /api/bible-proposals/{proposalId}":         {path: "/api/bible-proposals/" + id(f.proposal), body: BibleProposalDecisionInput{Decision: "approved"}, expect: "404"},

		"GET /api/chapters/{chapterId}":                               {path: "/api/chapters/" + id(f.chapter), expect: "404"},
		"PUT /api/chapters/{chapterId}":                               {path: "/api/chapters/" + id(f.chapter), body: ChapterMetaInput{Title: "Taken"}, expect: "404"},
		"DELETE /api/chapters/{chapterId}":                            {path: "/api/chapters/" + id(f.chapter), expect: "404"},
		"PUT /api/chapters/{chapterId}/content":                       {path: "/api/chapters/" + id(f.chapter) + "/content", body: ChapterContentInput{ContentMd: "overwritten"}, expect: "404"},
		"GET /api/chapters/{chapterId}/versions":                      {path: "/api/chapters/" + id(f.chapter) + "/versions", expect: "404"},
		"POST /api/chapters/{chapterId}/versions":                     {path: "/api/chapters/" + id(f.chapter) + "/versions", body: SnapshotInput{Label: ptr("x")}, expect: "404"},
		"GET /api/chapters/{chapterId}/versions/{versionId}":          {path: "/api/chapters/" + id(f.chapter) + "/versions/" + id(f.version), expect: "404"},
		"POST /api/chapters/{chapterId}/versions/{versionId}/restore": {path: "/api/chapters/" + id(f.chapter) + "/versions/" + id(f.version) + "/restore", expect: "404"},
		"GET /api/chapters/{chapterId}/runs":                          {path: "/api/chapters/" + id(f.chapter) + "/runs", expect: "404"},
		"POST /api/chapters/{chapterId}/critiques":                    {path: "/api/chapters/" + id(f.chapter) + "/critiques", body: CritiqueStartInput{}, expect: "404"},
		"GET /api/chapters/{chapterId}/revisions":                     {path: "/api/chapters/" + id(f.chapter) + "/revisions", expect: "404"},
		"POST /api/chapters/{chapterId}/revisions":                    {path: "/api/chapters/" + id(f.chapter) + "/revisions", body: RevisionStartInput{RunId: f.run}, expect: "404"},
		"POST /api/chapters/{chapterId}/bible-updates":                {path: "/api/chapters/" + id(f.chapter) + "/bible-updates", body: BibleUpdateStartInput{}, expect: "404"},
		"GET /api/chapters/{chapterId}/drafts":                        {path: "/api/chapters/" + id(f.chapter) + "/drafts", expect: "404"},
		"POST /api/chapters/{chapterId}/drafts":                       {path: "/api/chapters/" + id(f.chapter) + "/drafts", body: CowriteStartInput{WriterIds: []uuid.UUID{f.writer}, Instruction: "go"}, expect: "404"},
		"GET /api/chapters/{chapterId}/history":                       {path: "/api/chapters/" + id(f.chapter) + "/history", expect: "404"},

		"GET /api/runs/{runId}":                    {path: "/api/runs/" + id(f.run), expect: "404"},
		"POST /api/runs/{runId}/cancel":            {path: "/api/runs/" + id(f.run) + "/cancel", expect: "404"},
		"GET /api/runs/{runId}/events":             {path: "/api/runs/" + id(f.run) + "/events", expect: "404"},
		"GET /api/runs/{runId}/critiques":          {path: "/api/runs/" + id(f.run) + "/critiques", expect: "404"},
		"GET /api/runs/{runId}/issues":             {path: "/api/runs/" + id(f.run) + "/issues", expect: "404"},
		"GET /api/runs/{runId}/drafts":             {path: "/api/runs/" + id(f.run) + "/drafts", expect: "404"},
		"GET /api/runs/{runId}/bible-proposals":    {path: "/api/runs/" + id(f.run) + "/bible-proposals", expect: "404"},
		"GET /api/runs/{runId}/calls":              {path: "/api/runs/" + id(f.run) + "/calls", expect: "404"},
		"PUT /api/issues/{issueId}/decision":       {path: "/api/issues/" + id(f.issue) + "/decision", body: IssueDecisionInput{Decision: "accepted"}, expect: "404"},
		"GET /api/revisions/{revisionId}":          {path: "/api/revisions/" + id(f.revision), expect: "404"},
		"POST /api/revisions/{revisionId}/apply":   {path: "/api/revisions/" + id(f.revision) + "/apply", expect: "404"},
		"POST /api/revisions/{revisionId}/discard": {path: "/api/revisions/" + id(f.revision) + "/discard", expect: "404"},
		"PUT /api/drafts/{draftId}/decision":       {path: "/api/drafts/" + id(f.draft) + "/decision", body: DraftDecisionInput{Decision: "discarded"}, expect: "404"},

		"GET /api/writers":                       {path: "/api/writers", expect: "own"},
		"POST /api/writers":                      {path: "/api/writers", body: WriterInput{Name: "B's writer", Roles: []WriterRole{"critic"}, Enabled: true, Temperature: 0.5}, expect: "own"},
		"POST /api/writers/test":                 {path: "/api/writers/test", body: WriterTestInput{WriterId: &f.writer, Name: "x", ModelAlias: "lumos-chat", SystemPrompt: "x", Temperature: 0.5}, expect: "404"},
		"GET /api/writers/{writerId}":            {path: "/api/writers/" + id(f.writer), expect: "404"},
		"PUT /api/writers/{writerId}":            {path: "/api/writers/" + id(f.writer), body: WriterInput{Name: "Hijacked", ModelAlias: ptr("x"), Roles: []WriterRole{}, Enabled: true, Temperature: 0.5}, expect: "404"},
		"DELETE /api/writers/{writerId}":         {path: "/api/writers/" + id(f.writer), expect: "404"},
		"POST /api/writers/{writerId}/duplicate": {path: "/api/writers/" + id(f.writer) + "/duplicate", expect: "404"},
	}
}

// TestIntegrationIsolation walks every route of the router as account B
// against account A's work. A route without a probe fails the test.
func TestIntegrationIsolation(t *testing.T) {
	a := newEnv(t)
	b := newEnv(t)
	f := makeFixtures(t, a)
	probes := isolationProbes(f)
	ids := []string{f.project.String(), f.chapter.String(), f.version.String(), f.entry.String(), f.writer.String(), f.run.String(), f.critique.String(), f.issue.String(), f.revision.String(), f.draft.String(), f.proposal.String(), "A's novel", "Secret chapter", "A's critic"}

	mux, ok := a.handler.(chi.Routes)
	if !ok {
		t.Fatal("router is not a chi.Routes")
	}
	var routes []string
	seen := map[string]bool{}
	collect := func(method, route string) {
		if !strings.HasPrefix(route, "/api/") {
			return // the app shell and the guide are static
		}
		key := method + " " + route
		if !seen[key] {
			seen[key] = true
			routes = append(routes, key)
		}
	}
	walkRoutes(mux, collect)
	sort.Strings(routes)
	if len(routes) < 50 {
		t.Fatalf("only %d API routes found; the walk looks broken", len(routes))
	}
	var missing []string
	for _, key := range routes {
		p, ok := probes[key]
		if !ok {
			missing = append(missing, key)
			continue
		}
		method := strings.SplitN(key, " ", 2)[0]
		rec := b.do(method, p.path, p.body, nil)
		if err := checkProbe(p, rec, ids); err != nil {
			t.Errorf("%s: %v (body %s)", key, err, truncateRunes(rec.Body.String(), 200))
		}
	}
	if len(missing) > 0 {
		t.Fatalf("routes without an isolation probe (add them to isolationProbes):\n  %s", strings.Join(missing, "\n  "))
	}
	for key := range probes {
		if !seen[key] {
			t.Errorf("probe %s names a route that no longer exists", key)
		}
	}
	// A's things are still exactly as they were.
	var ch Chapter
	a.want(a.do("GET", "/api/chapters/"+f.chapter.String(), nil, &ch), 200, "GET", "A's chapter")
	if ch.Title != "Secret chapter" || !strings.Contains(ch.ContentMd, "Nobody asked where") {
		t.Fatalf("A's chapter changed: %+v", ch)
	}
	var writers []Writer
	a.want(a.do("GET", "/api/writers", nil, &writers), 200, "GET", "A's writers")
	found := false
	for _, w := range writers {
		if w.Id == f.writer && w.Name == "A's critic" {
			found = true
		}
	}
	if !found {
		t.Fatal("A's writer was touched")
	}
}

// checkProbe judges B's reply.
func checkProbe(p probe, rec *httptest.ResponseRecorder, aIDs []string) error {
	body := rec.Body.String()
	switch p.expect {
	case "404":
		if rec.Code != 404 {
			return fmt.Errorf("status %d, want 404", rec.Code)
		}
	case "403":
		if rec.Code != 403 {
			return fmt.Errorf("status %d, want 403", rec.Code)
		}
	case "public":
		if rec.Code >= 500 {
			return fmt.Errorf("status %d", rec.Code)
		}
	case "own":
		if rec.Code >= 500 {
			return fmt.Errorf("status %d", rec.Code)
		}
	default:
		return fmt.Errorf("unknown expectation %q", p.expect)
	}
	for _, id := range aIDs {
		if strings.Contains(body, id) {
			return fmt.Errorf("reply mentions A's %q", id)
		}
	}
	// Sanity: a JSON reply must stay JSON.
	if ct := rec.Header().Get("Content-Type"); strings.HasPrefix(ct, "application/json") && len(body) > 0 && !json.Valid([]byte(body)) {
		return fmt.Errorf("invalid JSON reply")
	}
	return nil
}
