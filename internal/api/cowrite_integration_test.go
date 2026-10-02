package api

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"writersguild/internal/guild"
	"writersguild/internal/llm"
	"writersguild/internal/runs"
)

func TestIntegrationCowrite(t *testing.T) {
	e := newEnv(t)
	var p Project
	e.want(e.do("POST", "/api/projects", ProjectInput{Name: "Co-write"}, &p), 201, "POST", "/api/projects")
	chapterMD := "# The Lamp\n\nThe lamp had been lit before anyone remembered lighting it. She crossed the room without looking at it.\n\nBy morning the gate stood open.\n"
	var ch Chapter
	e.want(e.do("POST", "/api/projects/"+p.Id.String()+"/chapters", ChapterCreateInput{Title: "Lamp", ContentMd: ptr(chapterMD)}, &ch), 201, "POST", "chapter")
	e.want(e.do("POST", "/api/projects/"+p.Id.String()+"/bible", BibleEntryInput{Section: "character", Title: "Mara", Fields: map[string]string{"role": "harbour master"}}, nil), 201, "POST", "bible")

	hem := e.createWriter("Hemingway", "writersguild-hem", []WriterRole{"co-writer", "critic"}, true)
	leguin := e.createWriter("Le Guin", "writersguild-leguin", []WriterRole{"co-writer"}, true)
	criticOnly := e.createWriter("Critic", "writersguild-critic", []WriterRole{"critic"}, true)
	disabled := e.createWriter("Sleeper", "writersguild-sleeper", []WriterRole{"co-writer"}, false)

	e.mock.StreamFn = func(ctx context.Context, req llm.Request, onDelta func(string)) (*llm.Response, error) {
		if !strings.HasPrefix(req.Metadata.GenerationName, "cowrite:") || req.JSONMode || req.Metadata.SessionID != ch.Id.String() {
			t.Errorf("co-write request wrong: %+v json=%v", req.Metadata, req.JSONMode)
		}
		prompt := req.Messages[1].Content
		if !strings.Contains(prompt, "**Mara**") || !strings.Contains(req.Messages[0].Content, guild.FixedSuffix) {
			t.Errorf("prompt lacks bible or suffix")
		}
		switch {
		case strings.Contains(req.Model, "leguin") && strings.Contains(prompt, "fail please"):
			return nil, &llm.GatewayError{Status: 503, Message: "down"}
		case strings.Contains(prompt, "selected this passage"):
			return streamJSON("Here is the rewrite:\n\nShe did not look at the lamp as she crossed the room.", onDelta), nil
		default:
			return streamJSON("The gate had been open since before the frost, and nobody would say who had opened it.", onDelta), nil
		}
	}
	waitCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	// Co-write from a selection.
	var run Run
	e.want(e.do("POST", "/api/chapters/"+ch.Id.String()+"/drafts", CowriteStartInput{
		WriterIds: []uuid.UUID{hem.Id}, Instruction: "Tighten this passage.", Selection: ptr("She crossed the room without looking at it."),
		ContextBefore: ptr("The lamp had been lit before anyone remembered lighting it. "), ContextAfter: ptr("\n\nBy morning"),
	}, &run), 202, "POST", "drafts")
	if run.Kind != "cowrite" || run.Status != "running" || run.Params["mode"] != "selection" {
		t.Fatalf("run %+v", run)
	}
	if err := e.engine.Wait(waitCtx, run.Id); err != nil {
		t.Fatal(err)
	}
	e.want(e.do("GET", "/api/runs/"+run.Id.String(), nil, &run), 200, "GET", "run")
	if run.Status != "succeeded" || (*run.Result)["succeeded"] != float64(1) || (*run.Result)["mode"] != "selection" {
		t.Fatalf("finished run %+v", run)
	}
	var drafts []Draft
	e.want(e.do("GET", "/api/runs/"+run.Id.String()+"/drafts", nil, &drafts), 200, "GET", "run drafts")
	if len(drafts) != 1 {
		t.Fatalf("drafts %+v", drafts)
	}
	d := drafts[0]
	if d.Status != "succeeded" || d.Mode != "selection" || d.Text != "She did not look at the lamp as she crossed the room." || d.Decision != "pending" || d.WriterName != "Hemingway" || d.PromptTokens == 0 {
		t.Fatalf("draft %+v", d)
	}
	counts := map[string]int{}
	for _, ev := range e.streamEvents(run.Id) {
		counts[ev.Type]++
	}
	if counts[guild.EventDraftStarted] != 1 || counts[guild.EventDraftDelta] < 1 || counts[guild.EventDraftDone] != 1 || counts[runs.EventRunFinished] != 1 {
		t.Fatalf("events %v", counts)
	}
	// Decisions: replace, discard, take back, and the rules.
	var decided Draft
	e.want(e.do("PUT", "/api/drafts/"+d.Id.String()+"/decision", DraftDecisionInput{Decision: "replaced"}, &decided), 200, "PUT", "replace")
	if decided.Decision != "replaced" || decided.DecidedAt == nil {
		t.Fatalf("replaced %+v", decided)
	}
	e.want(e.do("PUT", "/api/drafts/"+d.Id.String()+"/decision", DraftDecisionInput{Decision: "discarded"}, &decided), 200, "PUT", "discard")
	e.want(e.do("PUT", "/api/drafts/"+d.Id.String()+"/decision", DraftDecisionInput{Decision: "pending"}, &decided), 200, "PUT", "take back")
	if decided.Decision != "pending" || decided.DecidedAt != nil {
		t.Fatalf("taken back %+v", decided)
	}
	e.want(e.do("PUT", "/api/drafts/"+d.Id.String()+"/decision", map[string]string{"decision": "kept"}, nil), 400, "PUT", "bad decision")
	e.want(e.do("PUT", "/api/drafts/"+uuid.New().String()+"/decision", DraftDecisionInput{Decision: "discarded"}, nil), 404, "PUT", "unknown draft")

	// Continue from the cursor: no selection, so "replaced" is refused.
	e.want(e.do("POST", "/api/chapters/"+ch.Id.String()+"/drafts", CowriteStartInput{
		WriterIds: []uuid.UUID{leguin.Id}, Instruction: "Continue from here.", ContextBefore: ptr(chapterMD), ContextAfter: ptr(""), Notes: ptr("The frost matters."),
	}, &run), 202, "POST", "drafts continue")
	_ = e.engine.Wait(waitCtx, run.Id)
	e.want(e.do("GET", "/api/runs/"+run.Id.String()+"/drafts", nil, &drafts), 200, "GET", "run drafts")
	if len(drafts) != 1 || drafts[0].Mode != "continue" || !strings.Contains(drafts[0].Text, "open since before the frost") || drafts[0].Notes != "The frost matters." {
		t.Fatalf("continue draft %+v", drafts)
	}
	e.want(e.do("PUT", "/api/drafts/"+drafts[0].Id.String()+"/decision", DraftDecisionInput{Decision: "replaced"}, nil), 400, "PUT", "replace without selection")
	e.want(e.do("PUT", "/api/drafts/"+drafts[0].Id.String()+"/decision", DraftDecisionInput{Decision: "inserted"}, &decided), 200, "PUT", "insert")
	if decided.Decision != "inserted" {
		t.Fatalf("inserted %+v", decided)
	}
	var recent []Draft
	e.want(e.do("GET", "/api/chapters/"+ch.Id.String()+"/drafts?limit=5", nil, &recent), 200, "GET", "chapter drafts")
	if len(recent) != 2 || recent[0].Id != drafts[0].Id {
		t.Fatalf("recent drafts %+v", recent)
	}

	// A failing writer gives a failed draft and a failed run.
	e.want(e.do("POST", "/api/chapters/"+ch.Id.String()+"/drafts", CowriteStartInput{WriterIds: []uuid.UUID{leguin.Id}, Instruction: "fail please", ContextBefore: ptr("x")}, &run), 202, "POST", "drafts fail")
	_ = e.engine.Wait(waitCtx, run.Id)
	e.want(e.do("GET", "/api/runs/"+run.Id.String(), nil, &run), 200, "GET", "run")
	e.want(e.do("GET", "/api/runs/"+run.Id.String()+"/drafts", nil, &drafts), 200, "GET", "run drafts")
	if run.Status != "failed" || len(drafts) != 1 || drafts[0].Status != "failed" || !strings.Contains(drafts[0].Error, "503") {
		t.Fatalf("failed run %q drafts %+v", run.Status, drafts)
	}
	e.want(e.do("PUT", "/api/drafts/"+drafts[0].Id.String()+"/decision", DraftDecisionInput{Decision: "inserted"}, nil), 400, "PUT", "insert a failed draft")

	// Validation of the start request.
	e.want(e.do("POST", "/api/chapters/"+ch.Id.String()+"/drafts", CowriteStartInput{WriterIds: []uuid.UUID{hem.Id}, Instruction: "   "}, nil), 400, "POST", "no instruction")
	e.want(e.do("POST", "/api/chapters/"+ch.Id.String()+"/drafts", CowriteStartInput{WriterIds: []uuid.UUID{}, Instruction: "Go."}, nil), 400, "POST", "no writers")
	e.want(e.do("POST", "/api/chapters/"+ch.Id.String()+"/drafts", CowriteStartInput{WriterIds: []uuid.UUID{criticOnly.Id}, Instruction: "Go."}, nil), 400, "POST", "critic as co-writer")
	e.want(e.do("POST", "/api/chapters/"+ch.Id.String()+"/drafts", CowriteStartInput{WriterIds: []uuid.UUID{disabled.Id}, Instruction: "Go."}, nil), 400, "POST", "disabled co-writer")
	e.want(e.do("POST", "/api/chapters/"+ch.Id.String()+"/drafts", CowriteStartInput{WriterIds: []uuid.UUID{uuid.New()}, Instruction: "Go."}, nil), 404, "POST", "unknown writer")
	e.want(e.do("POST", "/api/chapters/"+ch.Id.String()+"/drafts", CowriteStartInput{WriterIds: []uuid.UUID{hem.Id, leguin.Id, hem.Id, uuid.New()}, Instruction: "Go."}, nil), 400, "POST", "too many writers")

	// Compare: three co-writers, the same request, side by side; one fails
	// and the other two still count.
	marquez := e.createWriter("García Márquez", "writersguild-marquez", []WriterRole{"co-writer"}, true)
	e.want(e.do("POST", "/api/chapters/"+ch.Id.String()+"/drafts", CowriteStartInput{
		WriterIds: []uuid.UUID{hem.Id, leguin.Id, marquez.Id}, Instruction: "fail please — but only Le Guin does", Selection: ptr("She crossed the room without looking at it."),
	}, &run), 202, "POST", "compare")
	if run.Kind != "compare" || len(run.Params["writer_ids"].([]any)) != 3 {
		t.Fatalf("compare run %+v", run)
	}
	_ = e.engine.Wait(waitCtx, run.Id)
	e.want(e.do("GET", "/api/runs/"+run.Id.String(), nil, &run), 200, "GET", "compare run")
	if run.Status != "succeeded" || (*run.Result)["succeeded"] != float64(2) || (*run.Result)["failed"] != float64(1) {
		t.Fatalf("compare result %v (%s)", *run.Result, run.Error)
	}
	e.want(e.do("GET", "/api/runs/"+run.Id.String()+"/drafts", nil, &drafts), 200, "GET", "compare drafts")
	if len(drafts) != 3 || drafts[0].WriterName != "Hemingway" || drafts[1].WriterName != "Le Guin" || drafts[2].WriterName != "García Márquez" {
		t.Fatalf("compare drafts order %+v", drafts)
	}
	if drafts[0].Status != "succeeded" || drafts[1].Status != "failed" || drafts[2].Status != "succeeded" || drafts[0].Position != 0 || drafts[2].Position != 2 {
		t.Fatalf("compare statuses %s %s %s", drafts[0].Status, drafts[1].Status, drafts[2].Status)
	}
	compareEvents := map[string]int{}
	for _, ev := range e.streamEvents(run.Id) {
		compareEvents[ev.Type]++
	}
	if compareEvents[guild.EventDraftStarted] != 3 || compareEvents[guild.EventDraftDone] != 2 || compareEvents[guild.EventDraftFailed] != 1 {
		t.Fatalf("compare events %v", compareEvents)
	}
	// Each draft is decided on its own: replace one, discard the other.
	e.want(e.do("PUT", "/api/drafts/"+drafts[2].Id.String()+"/decision", DraftDecisionInput{Decision: "replaced"}, &decided), 200, "PUT", "replace compared")
	e.want(e.do("PUT", "/api/drafts/"+drafts[0].Id.String()+"/decision", DraftDecisionInput{Decision: "discarded"}, &decided), 200, "PUT", "discard compared")
	e.want(e.do("GET", "/api/runs/"+run.Id.String()+"/drafts", nil, &drafts), 200, "GET", "compare drafts")
	if drafts[0].Decision != "discarded" || drafts[2].Decision != "replaced" || drafts[1].Decision != "pending" {
		t.Fatalf("compare decisions %s %s %s", drafts[0].Decision, drafts[1].Decision, drafts[2].Decision)
	}

	// History shows the drafts: one co-write run per request, the compare run with its three drafts.
	var hist HistoryPage
	e.want(e.do("GET", "/api/chapters/"+ch.Id.String()+"/history?kind=compare", nil, &hist), 200, "GET", "history compare")
	if len(hist.Items) != 1 || hist.Items[0].Drafts == nil || hist.Items[0].Drafts.Count != 3 || hist.Items[0].Drafts.Replaced != 1 || hist.Items[0].Drafts.Discarded != 1 || !strings.Contains(hist.Items[0].Summary, "3 drafts compared") {
		t.Fatalf("compare history %+v", hist.Items)
	}
	e.want(e.do("GET", "/api/chapters/"+ch.Id.String()+"/history?kind=cowrite", nil, &hist), 200, "GET", "history cowrite")
	if hist.Totals.Runs != 3 || len(hist.Items) != 3 {
		t.Fatalf("cowrite history %+v", hist.Totals)
	}
	for _, it := range hist.Items {
		if it.Run.Status == "failed" && !strings.HasPrefix(it.Summary, "Failed:") {
			t.Fatalf("failed run summary %q", it.Summary)
		}
		if it.Run.Status == "succeeded" && (it.Drafts == nil || it.Drafts.Count != 1 || !strings.Contains(it.Summary, "“")) {
			t.Fatalf("cowrite summary %+v", it)
		}
	}

	other := newEnv(t)
	other.want(other.do("POST", "/api/chapters/"+ch.Id.String()+"/drafts", CowriteStartInput{WriterIds: []uuid.UUID{hem.Id}, Instruction: "Go."}, nil), 404, "POST", "other's chapter")
	other.want(other.do("GET", "/api/runs/"+run.Id.String()+"/drafts", nil, nil), 404, "GET", "other's drafts")
	other.want(other.do("PUT", "/api/drafts/"+d.Id.String()+"/decision", DraftDecisionInput{Decision: "discarded"}, nil), 404, "PUT", "other's decision")
	other.want(other.do("GET", "/api/chapters/"+ch.Id.String()+"/drafts", nil, nil), 404, "GET", "other's chapter drafts")
}
