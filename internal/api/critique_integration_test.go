package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"writersguild/internal/db/sqlcgen"
	"writersguild/internal/guild"
	"writersguild/internal/llm"
	"writersguild/internal/runs"
)

// editorJSON merges the critics' notes found in the editor-in-chief prompt
// the way a cooperative model would: one issue per distinct quote, citing
// every source that quoted it.
var sourceLine = regexp.MustCompile(`\[([a-z0-9-]+/[A-Za-z0-9_-]+)\] (high|medium|low) — quote: ("(?:[^"\\]|\\.)*")`)

func editorJSON(prompt string) string {
	type group struct {
		sev   string
		quote string
		ids   []string
	}
	var order []string
	groups := map[string]*group{}
	for _, m := range sourceLine.FindAllStringSubmatch(prompt, -1) {
		q, err := strconv.Unquote(m[3])
		if err != nil {
			q = strings.Trim(m[3], `"`)
		}
		g, ok := groups[q]
		if !ok {
			g = &group{sev: m[2], quote: q}
			groups[q] = g
			order = append(order, q)
		}
		g.ids = append(g.ids, m[1])
	}
	var issues []map[string]any
	for i, q := range order {
		g := groups[q]
		issues = append(issues, map[string]any{"id": fmt.Sprintf("e%d", i+1), "severity": g.sev, "quote": g.quote, "problem": fmt.Sprintf("%d critics agree.", len(g.ids)), "suggested_fix": "Cut.", "sources": g.ids})
	}
	b, _ := json.Marshal(map[string]any{"issues": issues})
	return string(b)
}

// reviseText plays the lead writer: it tightens each accepted passage named
// in the prompt the same way the fake gateway does.
var passageLine = regexp.MustCompile(`passage: ("(?:[^"\\]|\\.)*")`)

func reviseText(prompt string) string {
	chapter := sceneText(llm.Request{Messages: []llm.Message{{Role: "user", Content: prompt}}})
	for _, m := range passageLine.FindAllStringSubmatch(prompt, -1) {
		q, err := strconv.Unquote(m[1])
		if err != nil {
			continue
		}
		words := strings.Fields(q)
		if len(words) >= 4 {
			words = append(words[:len(words)-2], words[len(words)-1])
		}
		chapter = strings.Replace(chapter, q, strings.Join(words, " "), 1)
	}
	return chapter
}

// createSystemWriter gives the account one of its system agents.
func (e *env) createSystemWriter(name, slug, alias string) sqlcgen.Writer {
	e.t.Helper()
	w, err := e.q.CreateWriter(context.Background(), sqlcgen.CreateWriterParams{
		UserID: e.user.ID, Name: name, Slug: slug, ModelAlias: alias, SystemPrompt: "You are " + name + ".", Roles: []string{}, Enabled: true, Temperature: 0.3, IsSystem: true,
	})
	if err != nil {
		e.t.Fatal(err)
	}
	return w
}

// createEditor gives the account its editor-in-chief system agent.
func (e *env) createEditor(alias string) sqlcgen.Writer {
	e.t.Helper()
	w, err := e.q.CreateWriter(context.Background(), sqlcgen.CreateWriterParams{
		UserID: e.user.ID, Name: "Editor-in-chief", Slug: "editor-in-chief", ModelAlias: alias, SystemPrompt: "You are the editor.", Roles: []string{}, Enabled: true, Temperature: 0.3, IsSystem: true,
	})
	if err != nil {
		e.t.Fatal(err)
	}
	return w
}

// sceneText pulls the chapter or scene out of a critic prompt.
func sceneText(req llm.Request) string {
	for i := len(req.Messages) - 1; i >= 0; i-- {
		c := req.Messages[i].Content
		if a := strings.Index(c, guild.ChapterBegin); a >= 0 {
			c = c[a+len(guild.ChapterBegin):]
			if b := strings.Index(c, guild.ChapterEnd); b >= 0 {
				c = c[:b]
			}
			return strings.TrimSpace(c)
		}
	}
	return ""
}

// sentencesOf returns the first n sentences of prose lines in a text.
func sentencesOf(text string, n int) []string {
	var out []string
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "*") {
			continue
		}
		start := 0
		for i, r := range line {
			if r == '.' {
				if s := strings.TrimSpace(line[start : i+1]); len(s) > 10 {
					out = append(out, s)
				}
				start = i + 1
			}
			if len(out) == n {
				return out
			}
		}
	}
	return out
}

func critiqueJSON(writer string, quotes []string, conflict string) string {
	var issues []map[string]string
	sev := []string{"high", "medium", "low"}
	for i, q := range quotes {
		issues = append(issues, map[string]string{"id": fmt.Sprintf("q%d", i+1), "severity": sev[i%3], "quote": q, "problem": "flat", "suggested_fix": "cut"})
	}
	body := map[string]any{"writer": writer, "overall": "Fine. Tighten it.", "issues": issues, "bible_conflicts": []any{}}
	if conflict != "" {
		body["bible_conflicts"] = []map[string]string{{"quote": conflict, "conflicts_with": "the bible says otherwise"}}
	}
	b, _ := json.Marshal(body)
	return string(b)
}

// streamJSON delivers content in a few chunks like a real gateway would.
func streamJSON(content string, onDelta func(string)) *llm.Response {
	for i := 0; i < len(content); i += 40 {
		end := i + 40
		if end > len(content) {
			end = len(content)
		}
		onDelta(content[i:end])
	}
	return &llm.Response{Content: content, FinishReason: "stop", Usage: llm.Usage{PromptTokens: 900, CompletionTokens: 120, TotalTokens: 1020}, CostUSD: 0.002, CostKnown: true, CostEstimated: true}
}

func (e *env) createWriter(name, alias string, roles []WriterRole, enabled bool) Writer {
	e.t.Helper()
	var w Writer
	e.want(e.do("POST", "/api/writers", WriterInput{Name: name, ModelAlias: ptr(alias), Roles: roles, Enabled: enabled, SystemPrompt: "You are " + name + ".", Temperature: 0.5}, &w), 201, "POST", "/api/writers")
	return w
}

func (e *env) streamEvents(runID uuid.UUID) []runs.Event {
	e.t.Helper()
	req := httptest.NewRequest("GET", "/api/runs/"+runID.String()+"/events", nil)
	rec := httptest.NewRecorder()
	e.handler.ServeHTTP(rec, req)
	var out []runs.Event
	for _, block := range strings.Split(rec.Body.String(), "\n\n") {
		var ev runs.Event
		for _, line := range strings.Split(block, "\n") {
			switch {
			case strings.HasPrefix(line, "id: "):
				fmt.Sscanf(line, "id: %d", &ev.Seq)
			case strings.HasPrefix(line, "event: "):
				ev.Type = strings.TrimPrefix(line, "event: ")
			case strings.HasPrefix(line, "data: "):
				ev.Payload = json.RawMessage(strings.TrimPrefix(line, "data: "))
			}
		}
		if ev.Type != "" && ev.Type != "end" {
			out = append(out, ev)
		}
	}
	return out
}

func TestIntegrationCritiqueWorkflow(t *testing.T) {
	e := newEnv(t)
	var p Project
	e.want(e.do("POST", "/api/projects", ProjectInput{Name: "Guild novel"}, &p), 201, "POST", "/api/projects")

	// Two long scenes separated by a break; with a 500-token limit the chapter splits in two.
	var sb strings.Builder
	sb.WriteString("# The Harbour\n\n")
	for i := 0; i < 40; i++ {
		fmt.Fprintf(&sb, "The first boat left the harbour at dawn number %d while the town slept. ", i)
	}
	sb.WriteString("\n\n* * *\n\n")
	for i := 0; i < 40; i++ {
		fmt.Fprintf(&sb, "By evening the second boat had still not returned on day %d and nobody spoke of it. ", i)
	}
	sb.WriteString("\n")
	chapterMD := sb.String()
	var ch Chapter
	e.want(e.do("POST", "/api/projects/"+p.Id.String()+"/chapters", ChapterCreateInput{Title: "Harbour", ContentMd: ptr(chapterMD)}, &ch), 201, "POST", "chapter")
	e.want(e.do("PUT", "/api/settings", SettingsInput{SceneTokenLimit: 500, AutosaveSnapshotMinutes: 10}, nil), 200, "PUT", "/api/settings")
	e.want(e.do("POST", "/api/projects/"+p.Id.String()+"/bible", BibleEntryInput{Section: "character", Title: "Mara", Fields: map[string]string{"role": "harbour master", "voice": "clipped"}}, nil), 201, "POST", "bible")

	e.createEditor("writersguild-editor")
	e.createSystemWriter("Lead writer", "lead-writer", "writersguild-lead")
	var editorFails atomic.Bool
	critic := []WriterRole{"critic"}
	good := e.createWriter("Good", "writersguild-good", critic, true)
	flaky := e.createWriter("Flaky", "writersguild-flaky", critic, true)
	failing := e.createWriter("Failing", "writersguild-fail", critic, true)
	e.createWriter("Disabled", "writersguild-disabled", critic, false)
	cowriter := e.createWriter("Drafter", "writersguild-drafter", []WriterRole{"co-writer"}, true)

	var flakyCalls atomic.Int32
	e.mock.StreamFn = func(ctx context.Context, req llm.Request, onDelta func(string)) (*llm.Response, error) {
		if req.Metadata.GenerationName == "lead-writer" {
			if req.Model != "writersguild-lead" || req.JSONMode || !strings.Contains(req.Messages[1].Content, "# Accepted notes to apply") {
				t.Errorf("lead writer request wrong: model %q json %v", req.Model, req.JSONMode)
			}
			return streamJSON(reviseText(req.Messages[1].Content), onDelta), nil
		}
		if req.Metadata.GenerationName == "editor-in-chief" {
			if req.Model != "writersguild-editor" || !req.JSONMode || !strings.Contains(req.Messages[1].Content, "[writersguild-good/") && !strings.Contains(req.Messages[1].Content, "[good/") {
				t.Errorf("editor request wrong: model %q json %v", req.Model, req.JSONMode)
			}
			if editorFails.Load() {
				return nil, &llm.GatewayError{Status: 500, Message: "editor exploded"}
			}
			return streamJSON(editorJSON(req.Messages[1].Content), onDelta), nil
		}
		if !req.JSONMode || !strings.HasPrefix(req.Metadata.GenerationName, "critic:") || req.Metadata.TraceID == "" || req.Metadata.SessionID != ch.Id.String() {
			t.Errorf("critic request lacks json mode or metadata: %+v", req.Metadata)
		}
		if !strings.Contains(req.Messages[0].Content, guild.FixedSuffix) || !strings.Contains(req.Messages[1].Content, "**Mara**") {
			t.Errorf("prompt missing fixed suffix or bible context")
		}
		scene := sceneText(req)
		quotes := sentencesOf(scene, 2)
		switch {
		case strings.Contains(req.Model, "fail"):
			return nil, &llm.GatewayError{Status: 500, Message: "upstream exploded"}
		case strings.Contains(req.Model, "flaky") && flakyCalls.Add(1)%2 == 1:
			return streamJSON("Sorry, here is prose instead of JSON.", onDelta), nil
		}
		// Two good quotes plus one invented sentence in a bible conflict (dropped with a warning).
		return streamJSON(critiqueJSON(req.Model, quotes, "The dragon ate the harbour."), onDelta), nil
	}

	var run Run
	e.want(e.do("POST", "/api/chapters/"+ch.Id.String()+"/critiques", CritiqueStartInput{WriterIds: ptr([]uuid.UUID{good.Id, flaky.Id, failing.Id})}, &run), 202, "POST", "critiques")
	if run.Status != "running" || run.Kind != "critique" {
		t.Fatalf("started run %+v", run)
	}
	waitCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := e.engine.Wait(waitCtx, run.Id); err != nil {
		t.Fatal(err)
	}
	e.want(e.do("GET", "/api/runs/"+run.Id.String(), nil, &run), 200, "GET", "run")
	if run.Status != "succeeded" {
		t.Fatalf("run status %q error %q", run.Status, run.Error)
	}
	res := *run.Result
	if res["succeeded"] != float64(2) || res["failed"] != float64(1) || res["scenes"] != float64(2) {
		t.Fatalf("run result %v", res)
	}
	if res["synthesis"] != "ok" || res["issues"] != float64(4) {
		t.Fatalf("synthesis result %v", res)
	}

	// The editor-in-chief merged the two critics' notes: both quoted the same
	// two sentences per scene, so four issues each citing two sources.
	var issues []Issue
	e.want(e.do("GET", "/api/runs/"+run.Id.String()+"/issues", nil, &issues), 200, "GET", "issues")
	if len(issues) != 4 {
		t.Fatalf("want 4 merged issues, got %d", len(issues))
	}
	for i, is := range issues {
		if is.Position != i || is.Key != fmt.Sprintf("e%d", i+1) || is.Decision != "pending" || is.EditedFix != nil || is.ContentHash != ch.ContentHash {
			t.Fatalf("issue %d fields %+v", i, is)
		}
		if chapterMD[is.Start:is.End] != is.Quote {
			t.Fatalf("issue %s not anchored: %q", is.Key, chapterMD[is.Start:is.End])
		}
		if len(is.Sources) != 2 {
			t.Fatalf("issue %s sources %+v", is.Key, is.Sources)
		}
		names := map[string]bool{}
		for _, src := range is.Sources {
			names[src.WriterName] = true
			if src.WriterId != good.Id && src.WriterId != flaky.Id || src.IssueId == "" || !strings.HasPrefix(src.Id, src.WriterSlug+"/") {
				t.Fatalf("source %+v", src)
			}
		}
		if !names["Good"] || !names["Flaky"] {
			t.Fatalf("issue %s should cite both critics: %v", is.Key, names)
		}
	}
	if !strings.Contains(issues[0].Quote, "first boat") || !strings.Contains(issues[3].Quote, "second boat") {
		t.Fatalf("issue order %q / %q", issues[0].Quote, issues[3].Quote)
	}

	// Decisions: accept, reject, edit the fix, undo.
	var decided Issue
	path := "/api/issues/" + issues[0].Id.String() + "/decision"
	e.want(e.do("PUT", path, IssueDecisionInput{Decision: "accepted"}, &decided), 200, "PUT", "accept")
	if decided.Decision != "accepted" || decided.DecidedAt == nil || decided.EditedFix != nil {
		t.Fatalf("accepted %+v", decided)
	}
	e.want(e.do("PUT", "/api/issues/"+issues[1].Id.String()+"/decision", IssueDecisionInput{Decision: "rejected"}, &decided), 200, "PUT", "reject")
	if decided.Decision != "rejected" || decided.DecidedAt == nil {
		t.Fatalf("rejected %+v", decided)
	}
	e.want(e.do("PUT", "/api/issues/"+issues[2].Id.String()+"/decision", IssueDecisionInput{Decision: "accepted", EditedFix: ptr("  Cut the clause after the comma.  ")}, &decided), 200, "PUT", "edit")
	if decided.Decision != "accepted" || decided.EditedFix == nil || *decided.EditedFix != "Cut the clause after the comma." {
		t.Fatalf("edited %+v", decided)
	}
	// Undo keeps the author's wording unless it is cleared explicitly.
	e.want(e.do("PUT", "/api/issues/"+issues[2].Id.String()+"/decision", IssueDecisionInput{Decision: "pending"}, &decided), 200, "PUT", "undo")
	if decided.Decision != "pending" || decided.DecidedAt != nil || decided.EditedFix == nil {
		t.Fatalf("undone %+v", decided)
	}
	e.want(e.do("PUT", "/api/issues/"+issues[2].Id.String()+"/decision", IssueDecisionInput{Decision: "pending", EditedFix: ptr("")}, &decided), 200, "PUT", "clear")
	if decided.EditedFix != nil {
		t.Fatalf("edited fix should be cleared: %+v", decided)
	}
	e.want(e.do("PUT", path, map[string]string{"decision": "maybe"}, nil), 400, "PUT", "bad decision")
	e.want(e.do("PUT", path, IssueDecisionInput{Decision: "accepted", EditedFix: ptr(strings.Repeat("x", 5001))}, nil), 400, "PUT", "too long")
	e.want(e.do("PUT", "/api/issues/"+uuid.New().String()+"/decision", IssueDecisionInput{Decision: "accepted"}, nil), 404, "PUT", "unknown issue")
	e.want(e.do("GET", "/api/runs/"+run.Id.String()+"/issues", nil, &issues), 200, "GET", "issues")
	if issues[0].Decision != "accepted" || issues[1].Decision != "rejected" || issues[2].Decision != "pending" || issues[3].Decision != "pending" {
		t.Fatalf("decisions not persisted: %s %s %s %s", issues[0].Decision, issues[1].Decision, issues[2].Decision, issues[3].Decision)
	}

	other := newEnv(t)

	// Revision: the lead writer applies the one accepted issue (scene 1 only;
	// scene 2 has no accepted issue and must come back untouched).
	critiqueRun := run
	var revRun Run
	e.want(e.do("POST", "/api/chapters/"+ch.Id.String()+"/revisions", RevisionStartInput{RunId: critiqueRun.Id}, &revRun), 202, "POST", "revisions")
	if revRun.Kind != "revision" || revRun.Status != "running" {
		t.Fatalf("revision run %+v", revRun)
	}
	if err := e.engine.Wait(waitCtx, revRun.Id); err != nil {
		t.Fatal(err)
	}
	e.want(e.do("GET", "/api/runs/"+revRun.Id.String(), nil, &revRun), 200, "GET", "revision run")
	if revRun.Status != "succeeded" {
		t.Fatalf("revision run %q: %s", revRun.Status, revRun.Error)
	}
	rres := *revRun.Result
	if rres["hunks"] != float64(1) || rres["applied_notes"] != float64(1) {
		t.Fatalf("revision result %v", rres)
	}
	revID := rres["revision_id"].(string)
	var rev Revision
	e.want(e.do("GET", "/api/revisions/"+revID, nil, &rev), 200, "GET", "revision")
	if rev.Status != "proposed" || rev.Stale || rev.BaseHash != ch.ContentHash || len(rev.Hunks) != 1 || len(rev.IssueIds) != 1 || rev.IssueIds[0] != issues[0].Id || len(rev.Skipped) != 0 {
		t.Fatalf("revision %+v", rev)
	}
	h := rev.Hunks[0]
	if chapterMD[h.OldStart:h.OldEnd] != h.OldText || h.OldText != "town " || h.NewText != "" || len(h.Ops) != 1 || h.Ops[0].Kind != "delete" {
		t.Fatalf("hunk %+v", h)
	}
	if strings.Contains(rev.RevisedMd, "number 0 while the town slept") || !strings.Contains(rev.RevisedMd, "number 0 while the slept.") || !strings.Contains(rev.RevisedMd, "number 1 while the town slept") {
		t.Fatalf("revised text should drop one word of the accepted passage: %q", rev.RevisedMd[:120])
	}
	if rev.Stats.WordsRemoved != 1 || rev.Stats.Hunks != 1 {
		t.Fatalf("stats %+v", rev.Stats)
	}
	if !strings.Contains(rev.RevisedMd, "second boat had still not returned on day 0") {
		t.Fatal("scene 2 should be untouched in the revised text")
	}
	var revList []Revision
	e.want(e.do("GET", "/api/chapters/"+ch.Id.String()+"/revisions?status=proposed", nil, &revList), 200, "GET", "revisions")
	if len(revList) != 1 || revList[0].Id != rev.Id {
		t.Fatalf("revision list %+v", revList)
	}
	revCounts := map[string]int{}
	for _, ev := range e.streamEvents(revRun.Id) {
		revCounts[ev.Type]++
	}
	if revCounts[guild.EventRevisionStarted] != 1 || revCounts[guild.EventRevisionDelta] < 1 || revCounts[guild.EventRevisionDone] != 1 {
		t.Fatalf("revision events %v", revCounts)
	}
	// Validation of the start request.
	e.want(e.do("POST", "/api/chapters/"+ch.Id.String()+"/revisions", RevisionStartInput{RunId: revRun.Id}, nil), 400, "POST", "revision of a revision run")
	e.want(e.do("POST", "/api/chapters/"+ch.Id.String()+"/revisions", RevisionStartInput{RunId: uuid.New()}, nil), 404, "POST", "unknown run")

	// Apply the hunk: snapshot before, chapter updated, revision applied.
	var applied RevisionApplyResult
	e.want(e.do("POST", "/api/revisions/"+revID+"/apply", RevisionApplyInput{HunkIndexes: ptr([]int{0})}, &applied), 200, "POST", "apply")
	if applied.Revision.Status != "applied" || applied.Revision.AppliedHunks == nil || len(*applied.Revision.AppliedHunks) != 1 || applied.Chapter.ContentMd != rev.RevisedMd || applied.Chapter.ContentHash != applied.Revision.ResultHash {
		t.Fatalf("applied %+v", applied.Revision)
	}
	var versions []ChapterVersionSummary
	e.want(e.do("GET", "/api/chapters/"+ch.Id.String()+"/versions", nil, &versions), 200, "GET", "versions")
	if len(versions) == 0 || versions[0].Kind != "pre_revision" || versions[0].ContentHash != ch.ContentHash {
		t.Fatalf("expected a pre_revision snapshot first: %+v", versions)
	}
	e.want(e.do("POST", "/api/revisions/"+revID+"/apply", nil, nil), 409, "POST", "apply twice")
	e.want(e.do("POST", "/api/revisions/"+revID+"/discard", nil, nil), 409, "POST", "discard applied")
	e.want(e.do("GET", "/api/chapters/"+ch.Id.String(), nil, &ch), 200, "GET", "chapter")

	// A second revision: accept the last issue too. The first issue's passage
	// has changed, so it is skipped; the chapter then changes under the
	// proposal, which makes it stale and unapplicable.
	e.want(e.do("PUT", "/api/issues/"+issues[3].Id.String()+"/decision", IssueDecisionInput{Decision: "accepted"}, nil), 200, "PUT", "accept last")
	e.want(e.do("POST", "/api/chapters/"+ch.Id.String()+"/revisions", RevisionStartInput{RunId: critiqueRun.Id}, &revRun), 202, "POST", "revisions 2")
	_ = e.engine.Wait(waitCtx, revRun.Id)
	e.want(e.do("GET", "/api/runs/"+revRun.Id.String(), nil, &revRun), 200, "GET", "revision run 2")
	if revRun.Status != "succeeded" {
		t.Fatalf("revision run 2 %q: %s", revRun.Status, revRun.Error)
	}
	rev2ID := (*revRun.Result)["revision_id"].(string)
	e.want(e.do("GET", "/api/revisions/"+rev2ID, nil, &rev), 200, "GET", "revision 2")
	if rev.Stale || len(rev.Skipped) != 1 || rev.Skipped[0].Key != "e1" || len(rev.IssueIds) != 1 || rev.IssueIds[0] != issues[3].Id || len(rev.Hunks) != 1 {
		t.Fatalf("revision 2 %+v", rev)
	}
	e.want(e.do("PUT", "/api/chapters/"+ch.Id.String()+"/content", ChapterContentInput{ContentMd: ch.ContentMd + "\nA new last line.\n", BaseHash: ptr(ch.ContentHash)}, nil), 200, "PUT", "edit chapter")
	e.want(e.do("GET", "/api/revisions/"+rev2ID, nil, &rev), 200, "GET", "revision 2 stale")
	if !rev.Stale {
		t.Fatal("revision should be stale after the chapter changed")
	}
	e.want(e.do("POST", "/api/revisions/"+rev2ID+"/apply", nil, nil), 409, "POST", "apply stale")
	e.want(e.do("POST", "/api/revisions/"+rev2ID+"/discard", nil, &rev), 200, "POST", "discard")
	if rev.Status != "discarded" || rev.Stale {
		t.Fatalf("discarded %+v", rev)
	}
	e.want(e.do("POST", "/api/revisions/"+rev2ID+"/apply", nil, nil), 409, "POST", "apply discarded")
	other.want(other.do("GET", "/api/revisions/"+rev2ID, nil, nil), 404, "GET", "other's revision")
	other.want(other.do("POST", "/api/revisions/"+rev2ID+"/apply", nil, nil), 404, "POST", "other's apply")
	other.want(other.do("POST", "/api/chapters/"+ch.Id.String()+"/revisions", RevisionStartInput{RunId: critiqueRun.Id}, nil), 404, "POST", "other's revision start")
	if run.PromptTokens == 0 || run.CostUsd == 0 || !run.CostEstimated {
		t.Fatalf("run usage not accumulated: %+v", run)
	}

	var recs []CritiqueRecord
	e.want(e.do("GET", "/api/runs/"+run.Id.String()+"/critiques", nil, &recs), 200, "GET", "critiques")
	if len(recs) != 3 {
		t.Fatalf("want 3 critique records, got %d", len(recs))
	}
	byName := map[string]CritiqueRecord{}
	for _, r := range recs {
		byName[r.WriterName] = r
	}
	g := byName["Good"]
	if g.Status != "succeeded" || g.Critique == nil || g.SceneCount != 2 {
		t.Fatalf("good critique %+v", g)
	}
	if len(g.Critique.Issues) != 4 {
		t.Fatalf("want 2 issues per scene, got %d", len(g.Critique.Issues))
	}
	for _, is := range g.Critique.Issues {
		if chapterMD[is.Start:is.End] != is.Quote || !is.QuoteExact {
			t.Fatalf("issue %s not anchored in the chapter: %q vs %q", is.Id, chapterMD[is.Start:is.End], is.Quote)
		}
		if !strings.HasPrefix(is.Id, "s1-") && !strings.HasPrefix(is.Id, "s2-") {
			t.Fatalf("issue id %q lacks a scene prefix", is.Id)
		}
	}
	if !strings.Contains(g.Critique.Issues[3].Quote, "second boat") || !strings.Contains(g.Critique.Issues[0].Quote, "first boat") {
		t.Fatalf("scene order lost: %q / %q", g.Critique.Issues[0].Quote, g.Critique.Issues[3].Quote)
	}
	if len(g.Critique.BibleConflicts) != 0 || g.Critique.Warnings == nil || !strings.Contains(strings.Join(*g.Critique.Warnings, " "), "bible conflict") {
		t.Fatalf("invented bible conflict should be dropped with a warning: %+v", g.Critique)
	}
	if !strings.HasPrefix(g.Critique.Overall, "Scene 1:") || g.PromptTokens != 1800 || g.CostUsd == 0 {
		t.Fatalf("good overall/usage %q %d %f", g.Critique.Overall, g.PromptTokens, g.CostUsd)
	}
	f := byName["Flaky"]
	if f.Status != "succeeded" || f.Critique == nil || !strings.Contains(f.RawText, "prose instead of JSON") {
		t.Fatalf("flaky critique should succeed on retry: %+v", f)
	}
	x := byName["Failing"]
	if x.Status != "failed" || !strings.Contains(x.Error, "500") || x.Critique != nil {
		t.Fatalf("failing critique %+v", x)
	}

	events := e.streamEvents(run.Id)
	counts := map[string]int{}
	var plan guild.PlanPayload
	for _, ev := range events {
		counts[ev.Type]++
		if ev.Type == guild.EventCritiquePlan {
			_ = json.Unmarshal(ev.Payload, &plan)
		}
	}
	if counts[guild.EventCritiquePlan] != 1 || len(plan.Scenes) != 2 || len(plan.Writers) != 3 || plan.Scenes[1].Start == 0 {
		t.Fatalf("plan %+v (counts %v)", plan, counts)
	}
	if counts[guild.EventWriterStarted] != 3 || counts[guild.EventWriterDone] != 2 || counts[guild.EventWriterFailed] != 1 || counts[guild.EventWriterRetry] < 1 || counts[guild.EventWriterDelta] < 4 || counts[runs.EventRunFinished] != 1 {
		t.Fatalf("event counts %v", counts)
	}
	if counts[guild.EventEditorStarted] != 1 || counts[guild.EventEditorDelta] < 1 || counts[guild.EventEditorDone] != 1 {
		t.Fatalf("editor event counts %v", counts)
	}
	for _, ev := range events {
		if ev.Type == guild.EventEditorDone {
			var ed guild.EditorEvent
			_ = json.Unmarshal(ev.Payload, &ed)
			if ed.Fallback || len(ed.Issues) != 4 || ed.Issues[0].Key != "e1" || len(ed.Issues[0].Sources) != 2 || ed.Usage == nil || ed.Usage.PromptTokens == 0 {
				t.Fatalf("editor.done payload %+v", ed)
			}
		}
	}
	for i := 1; i < len(events); i++ {
		if events[i].Seq != events[i-1].Seq+1 {
			t.Fatalf("event sequence has a gap at %d: %v -> %v", i, events[i-1].Seq, events[i].Seq)
		}
	}
	var chapterRuns []Run
	e.want(e.do("GET", "/api/chapters/"+ch.Id.String()+"/runs?kind=critique", nil, &chapterRuns), 200, "GET", "chapter runs")
	if len(chapterRuns) != 1 || chapterRuns[0].Id != run.Id {
		t.Fatalf("chapter runs %+v", chapterRuns)
	}

	// Validation of the start request.
	e.want(e.do("POST", "/api/chapters/"+ch.Id.String()+"/critiques", CritiqueStartInput{WriterIds: ptr([]uuid.UUID{cowriter.Id})}, nil), 400, "POST", "co-writer as critic")
	e.want(e.do("POST", "/api/chapters/"+ch.Id.String()+"/critiques", CritiqueStartInput{WriterIds: ptr([]uuid.UUID{uuid.New()})}, nil), 404, "POST", "unknown writer")
	var empty Chapter
	e.want(e.do("POST", "/api/projects/"+p.Id.String()+"/chapters", ChapterCreateInput{Title: "Empty"}, &empty), 201, "POST", "chapter")
	e.want(e.do("POST", "/api/chapters/"+empty.Id.String()+"/critiques", nil, nil), 400, "POST", "empty chapter")
	other.want(other.do("POST", "/api/chapters/"+ch.Id.String()+"/critiques", nil, nil), 404, "POST", "other's chapter")
	other.want(other.do("GET", "/api/runs/"+run.Id.String()+"/critiques", nil, nil), 404, "GET", "other's critiques")
	other.want(other.do("GET", "/api/runs/"+run.Id.String()+"/issues", nil, nil), 404, "GET", "other's issues")
	other.want(other.do("PUT", "/api/issues/"+issues[0].Id.String()+"/decision", IssueDecisionInput{Decision: "rejected"}, nil), 404, "PUT", "other's decision")

	// Default selection: every enabled critic (good, flaky, failing; not the
	// disabled one or the co-writer). This time the editor-in-chief fails, so
	// the list falls back to the critics' notes unmerged.
	editorFails.Store(true)
	e.want(e.do("POST", "/api/chapters/"+ch.Id.String()+"/critiques", nil, &run), 202, "POST", "critiques default")
	_ = e.engine.Wait(waitCtx, run.Id)
	e.want(e.do("GET", "/api/runs/"+run.Id.String()+"/critiques", nil, &recs), 200, "GET", "critiques")
	if len(recs) != 3 {
		t.Fatalf("default selection convened %d critics, want 3", len(recs))
	}
	e.want(e.do("GET", "/api/runs/"+run.Id.String(), nil, &run), 200, "GET", "run")
	// The fallback lists every issue of every critic that succeeded, unmerged.
	wantIssues := 0
	for _, rec := range recs {
		if rec.Critique != nil {
			wantIssues += len(rec.Critique.Issues)
		}
	}
	if run.Status != "succeeded" || (*run.Result)["synthesis"] != "fallback" || (*run.Result)["issues"] != float64(wantIssues) || wantIssues < 4 {
		t.Fatalf("fallback run result %v", *run.Result)
	}
	e.want(e.do("GET", "/api/runs/"+run.Id.String()+"/issues", nil, &issues), 200, "GET", "issues")
	if len(issues) != wantIssues || len(issues[0].Sources) != 1 || issues[0].Severity != "high" || issues[len(issues)-1].Severity != "medium" {
		t.Fatalf("fallback issues: %d, first %+v", len(issues), issues[0])
	}
	fallbackSeen := false
	for _, ev := range e.streamEvents(run.Id) {
		if ev.Type == guild.EventEditorDone {
			var ed guild.EditorEvent
			_ = json.Unmarshal(ev.Payload, &ed)
			fallbackSeen = ed.Fallback && strings.Contains(ed.Error, "500") && len(ed.Warnings) > 0
		}
	}
	if !fallbackSeen {
		t.Fatal("editor.done should report the fallback with the gateway error")
	}
}

func TestIntegrationCritiqueCancel(t *testing.T) {
	e := newEnv(t)
	var p Project
	e.want(e.do("POST", "/api/projects", ProjectInput{Name: "Cancel"}, &p), 201, "POST", "/api/projects")
	var ch Chapter
	e.want(e.do("POST", "/api/projects/"+p.Id.String()+"/chapters", ChapterCreateInput{Title: "Slow", ContentMd: ptr("The tide waited for no one that morning. She knew it.")}, &ch), 201, "POST", "chapter")
	e.createWriter("Slow", "writersguild-slow", []WriterRole{"critic"}, true)

	var started sync.WaitGroup
	started.Add(1)
	var once sync.Once
	e.mock.StreamFn = func(ctx context.Context, req llm.Request, onDelta func(string)) (*llm.Response, error) {
		onDelta("{\"writer\": \"Slow\", ")
		once.Do(started.Done)
		<-ctx.Done()
		return nil, ctx.Err()
	}
	var run Run
	e.want(e.do("POST", "/api/chapters/"+ch.Id.String()+"/critiques", nil, &run), 202, "POST", "critiques")
	started.Wait()
	e.want(e.do("POST", "/api/runs/"+run.Id.String()+"/cancel", nil, &run), 200, "POST", "cancel")
	if run.Status != "cancelled" {
		t.Fatalf("run status after cancel %q", run.Status)
	}
	var recs []CritiqueRecord
	e.want(e.do("GET", "/api/runs/"+run.Id.String()+"/critiques", nil, &recs), 200, "GET", "critiques")
	if len(recs) != 1 || recs[0].Status != "cancelled" {
		t.Fatalf("critique after cancel %+v", recs)
	}
	events := e.streamEvents(run.Id)
	last := events[len(events)-1]
	if last.Type != runs.EventRunFinished || !strings.Contains(string(last.Payload), `"cancelled"`) {
		t.Fatalf("last event %s %s", last.Type, last.Payload)
	}
}
