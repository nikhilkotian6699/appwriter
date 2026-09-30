package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/google/uuid"

	"writersguild/internal/config"
	"writersguild/internal/db/sqlcgen"
	"writersguild/internal/guild"
	"writersguild/internal/llm"
	"writersguild/internal/runs"
	"writersguild/internal/seed"
	"writersguild/internal/testutil"
)

type env struct {
	t       *testing.T
	q       *sqlcgen.Queries
	mock    *llm.Mock
	server  *Server
	handler http.Handler
	user    sqlcgen.User
}

func newEnv(t *testing.T) *env {
	t.Helper()
	pool := testutil.DB(t)
	q := sqlcgen.New(pool)
	user := testutil.NewUser(t, q, "author")
	mock := llm.NewMock()
	cfg := config.Config{AppName: "writersguild", DefaultModelAlias: "lumos-chat", LLMTimeout: 5 * time.Second}
	tracker := runs.NewTracker(q, mock, cfg.AppName)
	g := guild.New(tracker, cfg.AppName)
	srv := NewServer(cfg, pool, q, mock, g, slog.New(slog.NewTextHandler(io.Discard, nil)))
	srv.SetUserResolver(func(r *http.Request) (sqlcgen.User, error) { return q.GetUserByID(r.Context(), user.ID) })
	static := fstest.MapFS{"index.html": {Data: []byte("app")}}
	return &env{t: t, q: q, mock: mock, server: srv, handler: NewRouter(srv, static, static), user: user}
}

// do performs a JSON request and decodes the reply into out when given.
func (e *env) do(method, path string, body any, out any) *httptest.ResponseRecorder {
	e.t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req := httptest.NewRequest(method, path, rd)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	e.handler.ServeHTTP(rec, req)
	if out != nil && rec.Code < 300 {
		// json.Unmarshal reuses slice elements, so a field absent from the
		// reply would keep a stale value; start from a zero value instead.
		v := reflect.ValueOf(out).Elem()
		v.Set(reflect.Zero(v.Type()))
		if err := json.Unmarshal(rec.Body.Bytes(), out); err != nil {
			e.t.Fatalf("%s %s: decode %q: %v", method, path, rec.Body.String(), err)
		}
	}
	return rec
}

func (e *env) want(rec *httptest.ResponseRecorder, code int, method, path string) {
	e.t.Helper()
	if rec.Code != code {
		e.t.Fatalf("%s %s: status %d, want %d: %s", method, path, rec.Code, code, rec.Body.String())
	}
}

func TestIntegrationProjectsChaptersVersions(t *testing.T) {
	e := newEnv(t)

	var me Me
	e.want(e.do("GET", "/api/me", nil, &me), 200, "GET", "/api/me")
	if me.User.Username != e.user.Username || me.AliasPrefix != "writersguild-" || me.DefaultModelAlias != "lumos-chat" {
		t.Fatalf("me = %+v", me)
	}

	var p Project
	e.want(e.do("POST", "/api/projects", ProjectInput{Name: "  The Long Winter  ", Description: ptr("a novel")}, &p), 201, "POST", "/api/projects")
	if p.Name != "The Long Winter" || p.Description != "a novel" {
		t.Fatalf("project = %+v", p)
	}
	e.want(e.do("POST", "/api/projects", ProjectInput{Name: "   "}, nil), 400, "POST", "/api/projects")

	var list []ProjectSummary
	e.want(e.do("GET", "/api/projects", nil, &list), 200, "GET", "/api/projects")
	if len(list) != 1 || list[0].ChapterCount != 0 {
		t.Fatalf("list = %+v", list)
	}

	var ch Chapter
	pp := "/api/projects/" + p.Id.String() + "/chapters"
	e.want(e.do("POST", pp, ChapterCreateInput{Title: "One", ContentMd: ptr("It was cold.")}, &ch), 201, "POST", pp)
	if ch.Position != 0 || ch.ContentHash == "" || ch.ContentMd != "It was cold." {
		t.Fatalf("chapter = %+v", ch)
	}
	var ch2 Chapter
	e.want(e.do("POST", pp, ChapterCreateInput{Title: "Two"}, &ch2), 201, "POST", pp)
	if ch2.Position != 1 {
		t.Fatalf("second chapter position = %d", ch2.Position)
	}
	var chapters []ChapterSummary
	e.want(e.do("GET", pp, nil, &chapters), 200, "GET", pp)
	if len(chapters) != 2 || chapters[0].Title != "One" || chapters[0].ContentLength != len("It was cold.") {
		t.Fatalf("chapters = %+v", chapters)
	}

	// first save takes an autosave snapshot; an immediate second save does not
	cp := "/api/chapters/" + ch.Id.String() + "/content"
	var saved ChapterSaveResult
	e.want(e.do("PUT", cp, ChapterContentInput{ContentMd: "It was cold. It was dark.", BaseHash: ptr(ch.ContentHash)}, &saved), 200, "PUT", cp)
	if !saved.SnapshotCreated || saved.Chapter.ContentMd != "It was cold. It was dark." {
		t.Fatalf("first save = %+v", saved)
	}
	e.want(e.do("PUT", cp, ChapterContentInput{ContentMd: "It was cold. It was dark. It was late."}, &saved), 200, "PUT", cp)
	if saved.SnapshotCreated {
		t.Fatalf("second save within the interval must not snapshot: %+v", saved)
	}
	// unchanged content is a no-op
	e.want(e.do("PUT", cp, ChapterContentInput{ContentMd: "It was cold. It was dark. It was late."}, &saved), 200, "PUT", cp)
	// stale base hash is refused
	rec := e.do("PUT", cp, ChapterContentInput{ContentMd: "conflict", BaseHash: ptr(ch.ContentHash)}, nil)
	e.want(rec, 409, "PUT", cp)
	if !strings.Contains(rec.Body.String(), `"conflict"`) {
		t.Fatalf("409 body = %s", rec.Body.String())
	}

	// manual snapshot with a label
	vp := "/api/chapters/" + ch.Id.String() + "/versions"
	var manual ChapterVersion
	e.want(e.do("POST", vp, SnapshotInput{Label: ptr("before the storm")}, &manual), 201, "POST", vp)
	if manual.Kind != "manual" || manual.Label != "before the storm" || manual.ContentMd != saved.Chapter.ContentMd {
		t.Fatalf("manual = %+v", manual)
	}
	var versions []ChapterVersionSummary
	e.want(e.do("GET", vp, nil, &versions), 200, "GET", vp)
	if len(versions) != 2 || versions[0].Kind != "manual" || versions[1].Kind != "autosave" {
		t.Fatalf("versions = %+v", versions)
	}
	autosaveID := versions[1].Id

	// restore the autosave: a pre_restore snapshot appears and the text goes back
	var restored Chapter
	rp := vp + "/" + autosaveID.String() + "/restore"
	e.want(e.do("POST", rp, nil, &restored), 200, "POST", rp)
	if restored.ContentMd != "It was cold. It was dark." {
		t.Fatalf("restored = %+v", restored)
	}
	e.want(e.do("GET", vp, nil, &versions), 200, "GET", vp)
	if len(versions) != 3 || versions[0].Kind != "pre_restore" || !strings.HasPrefix(versions[0].Label, "Before restoring") {
		t.Fatalf("versions after restore = %+v", versions)
	}
	var full ChapterVersion
	e.want(e.do("GET", vp+"/"+versions[0].Id.String(), nil, &full), 200, "GET", vp)
	if full.ContentMd != "It was cold. It was dark. It was late." {
		t.Fatalf("pre_restore content = %q", full.ContentMd)
	}
	// a version id from another chapter is not found under this chapter
	e.want(e.do("GET", "/api/chapters/"+ch2.Id.String()+"/versions/"+versions[0].Id.String(), nil, nil), 404, "GET", "cross-chapter version")

	// rename and move
	var moved Chapter
	e.want(e.do("PUT", "/api/chapters/"+ch2.Id.String(), ChapterMetaInput{Title: "Two, renamed", Position: ptr(5)}, &moved), 200, "PUT", "chapter")
	if moved.Title != "Two, renamed" || moved.Position != 5 {
		t.Fatalf("moved = %+v", moved)
	}

	e.want(e.do("DELETE", "/api/chapters/"+ch2.Id.String(), nil, nil), 204, "DELETE", "chapter")
	e.want(e.do("DELETE", "/api/chapters/"+ch2.Id.String(), nil, nil), 404, "DELETE", "chapter again")
	e.want(e.do("DELETE", "/api/projects/"+p.Id.String(), nil, nil), 204, "DELETE", "project")
	e.want(e.do("GET", "/api/chapters/"+ch.Id.String(), nil, nil), 404, "GET", "chapter of deleted project")
	e.want(e.do("GET", "/api/projects/"+uuid.New().String(), nil, nil), 404, "GET", "unknown project")
	e.want(e.do("GET", "/api/projects/not-a-uuid", nil, nil), 400, "GET", "bad uuid")
	e.want(e.do("GET", "/api/nope", nil, nil), 404, "GET", "unknown route")
}

func TestIntegrationBible(t *testing.T) {
	e := newEnv(t)
	var p Project
	e.want(e.do("POST", "/api/projects", ProjectInput{Name: "Bible test"}, &p), 201, "POST", "/api/projects")
	var ch Chapter
	e.want(e.do("POST", "/api/projects/"+p.Id.String()+"/chapters", ChapterCreateInput{Title: "One"}, &ch), 201, "POST", "chapter")

	bp := "/api/projects/" + p.Id.String() + "/bible"
	var character BibleEntry
	e.want(e.do("POST", bp, BibleEntryInput{Section: "character", Title: "Mara", Fields: map[string]string{"role": "protagonist", "voice": "dry", "arc": "learns to ask", "key_facts": "left-handed"}}, &character), 201, "POST", bp)
	if character.Position != 0 || character.Fields["voice"] != "dry" {
		t.Fatalf("character = %+v", character)
	}
	var second BibleEntry
	e.want(e.do("POST", bp, BibleEntryInput{Section: "character", Title: "Tomas", Fields: map[string]string{}}, &second), 201, "POST", bp)
	if second.Position != 1 {
		t.Fatalf("second position = %d", second.Position)
	}
	var summary BibleEntry
	e.want(e.do("POST", bp, BibleEntryInput{Section: "chapter_summary", Title: "One", Fields: map[string]string{"text": "Mara arrives."}, ChapterId: &ch.Id}, &summary), 201, "POST", bp)
	if summary.ChapterId == nil || *summary.ChapterId != ch.Id {
		t.Fatalf("summary = %+v", summary)
	}
	e.want(e.do("POST", bp, BibleEntryInput{Section: "spells", Title: "x", Fields: map[string]string{}}, nil), 400, "POST", "bad section")
	other := uuid.New()
	e.want(e.do("POST", bp, BibleEntryInput{Section: "chapter_summary", Title: "x", Fields: map[string]string{}, ChapterId: &other}, nil), 400, "POST", "foreign chapter")

	var entries []BibleEntry
	e.want(e.do("GET", bp, nil, &entries), 200, "GET", bp)
	if len(entries) != 3 || entries[0].Section != "chapter_summary" || entries[1].Title != "Mara" {
		t.Fatalf("entries = %+v", entries)
	}

	var updated BibleEntry
	ep := "/api/bible-entries/" + character.Id.String()
	e.want(e.do("PUT", ep, BibleEntryInput{Section: "premise", Title: "Mara Vell", Fields: map[string]string{"role": "protagonist"}, Position: ptr(7)}, &updated), 200, "PUT", ep)
	if updated.Section != "character" || updated.Title != "Mara Vell" || updated.Position != 7 || len(updated.Fields) != 1 {
		t.Fatalf("updated = %+v (section must not change)", updated)
	}
	e.want(e.do("DELETE", ep, nil, nil), 204, "DELETE", ep)
	e.want(e.do("DELETE", ep, nil, nil), 404, "DELETE", ep)

	// deleting the chapter unlinks the summary instead of deleting it
	e.want(e.do("DELETE", "/api/chapters/"+ch.Id.String(), nil, nil), 204, "DELETE", "chapter")
	e.want(e.do("GET", bp, nil, &entries), 200, "GET", bp)
	if len(entries) != 2 || entries[0].ChapterId != nil {
		t.Fatalf("entries after chapter delete = %+v", entries)
	}
}

func TestIntegrationWritersAndTest(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	if err := seed.ForUser(ctx, e.q, e.user.ID, "writersguild", "lumos-chat"); err != nil {
		t.Fatal(err)
	}
	var writers []Writer
	e.want(e.do("GET", "/api/writers", nil, &writers), 200, "GET", "/api/writers")
	if len(writers) != 8 {
		t.Fatalf("seeded %d writers, want 8", len(writers))
	}
	var systemCount int
	byslug := map[string]Writer{}
	for _, w := range writers {
		byslug[w.Slug] = w
		if w.IsSystem {
			systemCount++
			if len(w.Roles) != 0 {
				t.Fatalf("system writer %s has roles %v", w.Slug, w.Roles)
			}
		}
		if w.ModelAlias != "lumos-chat" {
			t.Fatalf("seeded alias = %q, want DEFAULT_MODEL_ALIAS", w.ModelAlias)
		}
	}
	if systemCount != 3 || byslug["garcia-marquez"].Name != "García Márquez" || len(byslug["hemingway"].Roles) != 2 {
		t.Fatalf("seed shape wrong: %+v", byslug)
	}
	// system writers come last
	if !writers[len(writers)-1].IsSystem || writers[0].IsSystem {
		t.Fatalf("ordering: %+v", writers)
	}

	// create: slug generated, uniqueness and alias convention applied
	var w Writer
	e.want(e.do("POST", "/api/writers", WriterInput{Name: " García Márquez ", SystemPrompt: "p", Roles: []WriterRole{"critic", "critic"}, Enabled: true, Temperature: 0.5}, &w), 201, "POST", "/api/writers")
	if w.Slug != "garcia-marquez-2" || w.ModelAlias != "writersguild-garcia-marquez-2" || w.Name != "García Márquez" || len(w.Roles) != 1 {
		t.Fatalf("created = %+v", w)
	}
	e.want(e.do("POST", "/api/writers", WriterInput{Name: "x", Roles: []WriterRole{"editor"}, Temperature: 0.5}, nil), 400, "POST", "bad role")
	e.want(e.do("POST", "/api/writers", WriterInput{Name: "x", Temperature: 3}, nil), 400, "POST", "bad temperature")

	// update: alias required, roles replaced
	var up Writer
	e.want(e.do("PUT", "/api/writers/"+w.Id.String(), WriterInput{Name: "Gabo", ModelAlias: ptr("lumos-chat"), SystemPrompt: "new", Roles: []WriterRole{"co-writer"}, Enabled: false, Temperature: 1.1}, &up), 200, "PUT", "writer")
	if up.Name != "Gabo" || up.Slug != "garcia-marquez-2" || up.ModelAlias != "lumos-chat" || up.Enabled || up.Roles[0] != "co-writer" || up.Temperature != 1.1 {
		t.Fatalf("updated = %+v", up)
	}
	e.want(e.do("PUT", "/api/writers/"+w.Id.String(), WriterInput{Name: "Gabo", ModelAlias: ptr(""), Temperature: 1}, nil), 400, "PUT", "empty alias")

	// system agents: roles are ignored, cannot be deleted or duplicated
	eic := byslug["editor-in-chief"]
	var sys Writer
	e.want(e.do("PUT", "/api/writers/"+eic.Id.String(), WriterInput{Name: "Chief", ModelAlias: ptr("lumos-chat"), SystemPrompt: "merge", Roles: []WriterRole{"critic"}, Enabled: true, Temperature: 0.2}, &sys), 200, "PUT", "system writer")
	if len(sys.Roles) != 0 || sys.Name != "Chief" || sys.Slug != "editor-in-chief" {
		t.Fatalf("system update = %+v", sys)
	}
	e.want(e.do("DELETE", "/api/writers/"+eic.Id.String(), nil, nil), 400, "DELETE", "system writer")
	e.want(e.do("POST", "/api/writers/"+eic.Id.String()+"/duplicate", nil, nil), 400, "POST", "duplicate system")

	// duplicate a normal writer
	var dup Writer
	e.want(e.do("POST", "/api/writers/"+byslug["hemingway"].Id.String()+"/duplicate", nil, &dup), 201, "POST", "duplicate")
	if dup.Name != "Hemingway (copy)" || dup.Slug != "hemingway-copy" || dup.SystemPrompt != byslug["hemingway"].SystemPrompt || dup.IsSystem {
		t.Fatalf("dup = %+v", dup)
	}
	e.want(e.do("DELETE", "/api/writers/"+dup.Id.String(), nil, nil), 204, "DELETE", "dup")
	e.want(e.do("GET", "/api/writers/"+dup.Id.String(), nil, nil), 404, "GET", "deleted dup")

	// test writer through the mock gateway: run + model call + metadata
	var res WriterTestResult
	hem := byslug["hemingway"]
	e.want(e.do("POST", "/api/writers/test", WriterTestInput{WriterId: &hem.Id, Name: hem.Name, ModelAlias: hem.ModelAlias, SystemPrompt: hem.SystemPrompt, Temperature: 0.7}, &res), 200, "POST", "/api/writers/test")
	if !res.Ok || res.Reply == nil || res.PromptTokens != 100 || res.CompletionTokens != 50 || res.CostUsd == nil || *res.CostUsd != 0.0003 || res.CostEstimated {
		t.Fatalf("test result = %+v", res)
	}
	reqs := e.mock.Requests()
	if len(reqs) != 1 {
		t.Fatalf("mock calls = %d", len(reqs))
	}
	md := reqs[0].Metadata
	if md.TraceID != res.RunId.String() || md.GenerationName != "test:hemingway" || md.TraceUserID != e.user.Username || len(md.Tags) != 1 || md.Tags[0] != "writersguild" || md.SessionID != "" {
		t.Fatalf("metadata = %+v", md)
	}
	if !strings.HasSuffix(reqs[0].Messages[0].Content, guild.FixedSuffix) || !strings.HasPrefix(reqs[0].Messages[0].Content, hem.SystemPrompt) {
		t.Fatalf("system prompt must be the writer's prompt plus the fixed suffix")
	}
	run, err := e.q.GetRun(ctx, sqlcgen.GetRunParams{ID: res.RunId, UserID: e.user.ID})
	if err != nil || run.Kind != "writer_test" || run.Status != "succeeded" || run.CostUsd != 0.0003 || run.PromptTokens != 100 || run.TraceID != run.ID.String() {
		t.Fatalf("run = %+v err=%v", run, err)
	}

	// an unknown alias: the gateway's error is shown, the run is failed
	e.mock.CompleteFn = func(ctx context.Context, req llm.Request) (*llm.Response, error) {
		return nil, &llm.GatewayError{Status: 400, Message: "Invalid model name passed in model=" + req.Model}
	}
	e.want(e.do("POST", "/api/writers/test", WriterTestInput{Name: "Nobody", ModelAlias: "writersguild-nobody", SystemPrompt: "", Temperature: 0.7}, &res), 200, "POST", "/api/writers/test")
	if res.Ok || res.Error == nil || !strings.Contains(*res.Error, "does not know this alias") || !strings.Contains(*res.Error, "writersguild-nobody") {
		t.Fatalf("error result = %+v", res)
	}
	run, err = e.q.GetRun(ctx, sqlcgen.GetRunParams{ID: res.RunId, UserID: e.user.ID})
	if err != nil || run.Status != "failed" || run.Error == "" {
		t.Fatalf("failed run = %+v err=%v", run, err)
	}
	e.want(e.do("POST", "/api/writers/test", WriterTestInput{Name: "x", ModelAlias: "  ", Temperature: 0.7}, nil), 400, "POST", "empty alias")

	// gateway models filtered by prefix
	e.mock.Models = []string{"lumos-chat", "writersguild-hemingway", "writersguild-le-guin", "other-model"}
	var gm GatewayModels
	e.want(e.do("GET", "/api/gateway/models", nil, &gm), 200, "GET", "/api/gateway/models")
	if gm.AliasPrefix != "writersguild-" || len(gm.Aliases) != 2 || gm.Aliases[0] != "writersguild-hemingway" || len(gm.AllModels) != 4 {
		t.Fatalf("gateway models = %+v", gm)
	}
	e.mock.Err = &llm.GatewayError{Status: 401, Message: "bad key"}
	rec := e.do("GET", "/api/gateway/models", nil, nil)
	if rec.Code != 502 || !strings.Contains(rec.Body.String(), "gateway_error") {
		t.Fatalf("gateway failure: %d %s", rec.Code, rec.Body.String())
	}
	e.mock.Err = nil

	// settings
	var st Settings
	e.want(e.do("GET", "/api/settings", nil, &st), 200, "GET", "/api/settings")
	if st.SceneTokenLimit != 6000 || st.AutosaveSnapshotMinutes != 10 {
		t.Fatalf("default settings = %+v", st)
	}
	e.want(e.do("PUT", "/api/settings", SettingsInput{SceneTokenLimit: 3000, AutosaveSnapshotMinutes: 5}, &st), 200, "PUT", "/api/settings")
	if st.SceneTokenLimit != 3000 || st.AutosaveSnapshotMinutes != 5 {
		t.Fatalf("updated settings = %+v", st)
	}
	e.want(e.do("PUT", "/api/settings", SettingsInput{SceneTokenLimit: 10, AutosaveSnapshotMinutes: 5}, nil), 400, "PUT", "bad settings")
}

// TestIntegrationIsolationSeed verifies that two accounts never see each
// other's rows through the API. Milestone 7 extends this into the
// router-walking test.
func TestIntegrationIsolationSeed(t *testing.T) {
	a := newEnv(t)
	b := newEnv(t)
	var p Project
	a.want(a.do("POST", "/api/projects", ProjectInput{Name: "A's novel"}, &p), 201, "POST", "/api/projects")
	b.want(b.do("GET", "/api/projects/"+p.Id.String(), nil, nil), 404, "GET", "other user's project")
	var list []ProjectSummary
	b.want(b.do("GET", "/api/projects", nil, &list), 200, "GET", "/api/projects")
	if len(list) != 0 {
		t.Fatalf("b sees %d projects of a", len(list))
	}
}
