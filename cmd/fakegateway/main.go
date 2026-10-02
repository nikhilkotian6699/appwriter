// Command fakegateway is a small OpenAI-compatible stand-in for the LiteLLM
// gateway, for development without a real one. It knows the app's alias
// convention, streams replies, reports usage and a cost header, and can
// simulate errors: any model id containing "429" or "500" fails that way.
package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

type message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatRequest struct {
	Model    string    `json:"model"`
	Messages []message `json:"messages"`
	Stream   bool      `json:"stream"`
	Metadata struct {
		GenerationName string `json:"generation_name"`
		TraceID        string `json:"trace_id"`
	} `json:"metadata"`
	ResponseFormat *struct {
		Type string `json:"type"`
	} `json:"response_format"`
}

type gateway struct {
	models  []string
	prices  map[string][2]float64
	delay   time.Duration
	require string
}

func main() {
	port := env("PORT", "4000")
	app := env("APP_NAME", "writersguild")
	models := strings.Split(env("FAKE_MODELS", ""), ",")
	if len(models) == 1 && models[0] == "" {
		models = nil
		for _, s := range []string{"hemingway", "garcia-marquez", "le-carre", "le-guin", "stephen-king", "editor-in-chief", "lead-writer", "bible-keeper"} {
			models = append(models, app+"-"+s)
		}
		models = append(models, "lumos-chat")
	}
	delayMs, _ := strconv.Atoi(env("FAKE_DELAY_MS", "12"))
	g := &gateway{models: models, prices: map[string][2]float64{}, delay: time.Duration(delayMs) * time.Millisecond, require: os.Getenv("FAKE_REQUIRE_KEY")}
	for _, m := range models {
		g.prices[m] = [2]float64{0.000001, 0.000002}
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/models", g.listModels)
	mux.HandleFunc("/model/info", g.modelInfo)
	mux.HandleFunc("/v1/chat/completions", g.chat)
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("ok")) })
	log.Printf("fake gateway listening on :%s with %d models", port, len(models))
	log.Fatal(http.ListenAndServe(":"+port, logRequests(mux)))
}

func env(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.Printf("%s %s", r.Method, r.URL.Path)
		next.ServeHTTP(w, r)
	})
}

func (g *gateway) authorized(w http.ResponseWriter, r *http.Request) bool {
	if g.require == "" {
		return true
	}
	if r.Header.Get("Authorization") != "Bearer "+g.require {
		writeErr(w, 401, "Authentication Error, Invalid proxy server token passed", "auth_error")
		return false
	}
	return true
}

func (g *gateway) listModels(w http.ResponseWriter, r *http.Request) {
	if !g.authorized(w, r) {
		return
	}
	data := make([]map[string]any, 0, len(g.models))
	for _, m := range g.models {
		data = append(data, map[string]any{"id": m, "object": "model", "owned_by": "fakegateway"})
	}
	writeJSON(w, 200, map[string]any{"object": "list", "data": data})
}

func (g *gateway) modelInfo(w http.ResponseWriter, r *http.Request) {
	if !g.authorized(w, r) {
		return
	}
	data := make([]map[string]any, 0, len(g.models))
	for _, m := range g.models {
		p := g.prices[m]
		data = append(data, map[string]any{
			"model_name":     m,
			"litellm_params": map[string]any{"model": "fake/" + m},
			"model_info":     map[string]any{"input_cost_per_token": p[0], "output_cost_per_token": p[1], "max_tokens": 8192},
		})
	}
	writeJSON(w, 200, map[string]any{"data": data})
}

func (g *gateway) chat(w http.ResponseWriter, r *http.Request) {
	if !g.authorized(w, r) {
		return
	}
	var req chatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, 400, "invalid JSON body: "+err.Error(), "invalid_request_error")
		return
	}
	switch {
	case strings.Contains(req.Model, "429"):
		w.Header().Set("Retry-After", "1")
		writeErr(w, 429, "Rate limit reached for "+req.Model, "rate_limit_error")
		return
	case strings.Contains(req.Model, "500"):
		writeErr(w, 500, "Internal server error simulated for "+req.Model, "internal_error")
		return
	}
	known := false
	for _, m := range g.models {
		if m == req.Model {
			known = true
			break
		}
	}
	if !known {
		writeErr(w, 400, fmt.Sprintf("Invalid model name passed in model=%s. Call `/v1/models` to view available models for your key.", req.Model), "invalid_request_error")
		return
	}
	reply := g.reply(req)
	promptTokens := 0
	for _, m := range req.Messages {
		promptTokens += len(m.Content) / 4
	}
	usage := map[string]any{"prompt_tokens": promptTokens, "completion_tokens": len(reply) / 4, "total_tokens": promptTokens + len(reply)/4}
	if req.Stream {
		g.stream(w, req.Model, reply, usage)
		return
	}
	p := g.prices[req.Model]
	cost := float64(promptTokens)*p[0] + float64(len(reply)/4)*p[1]
	w.Header().Set("x-litellm-response-cost", strconv.FormatFloat(cost, 'f', -1, 64))
	writeJSON(w, 200, map[string]any{
		"id": "chatcmpl-fake", "object": "chat.completion", "created": time.Now().Unix(), "model": req.Model,
		"choices": []map[string]any{{"index": 0, "message": map[string]any{"role": "assistant", "content": reply}, "finish_reason": "stop"}},
		"usage":   usage,
	})
}

func (g *gateway) stream(w http.ResponseWriter, model, reply string, usage map[string]any) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	flusher, _ := w.(http.Flusher)
	send := func(v any) {
		b, _ := json.Marshal(v)
		fmt.Fprintf(w, "data: %s\n\n", b)
		if flusher != nil {
			flusher.Flush()
		}
	}
	words := strings.SplitAfter(reply, " ")
	for i, wd := range words {
		if wd == "" {
			continue
		}
		delta := map[string]any{"content": wd}
		if i == 0 {
			delta["role"] = "assistant"
		}
		send(map[string]any{"id": "chatcmpl-fake", "object": "chat.completion.chunk", "model": model,
			"choices": []map[string]any{{"index": 0, "delta": delta, "finish_reason": nil}}})
		time.Sleep(g.delay)
	}
	send(map[string]any{"id": "chatcmpl-fake", "object": "chat.completion.chunk", "model": model,
		"choices": []map[string]any{{"index": 0, "delta": map[string]any{}, "finish_reason": "stop"}}})
	send(map[string]any{"id": "chatcmpl-fake", "object": "chat.completion.chunk", "model": model, "choices": []any{}, "usage": usage})
	fmt.Fprint(w, "data: [DONE]\n\n")
	if flusher != nil {
		flusher.Flush()
	}
}

// reply picks a canned answer by generation name.
func (g *gateway) reply(req chatRequest) string {
	gen := req.Metadata.GenerationName
	name := strings.TrimPrefix(req.Model, "")
	if i := strings.Index(gen, ":"); i >= 0 {
		name = gen[i+1:]
	}
	switch {
	case gen == "lead-writer":
		return leadWriterReply(req.Messages)
	case gen == "editor-in-chief":
		if strings.Contains(req.Model, "invalid") {
			return "The editor declines to answer in JSON today."
		}
		return editorReply(req.Messages)
	case strings.HasPrefix(gen, "critic:"):
		if strings.Contains(req.Model, "invalid") {
			return "I would rather talk about the weather than return JSON."
		}
		return critiqueReply(name, req.Messages)
	case strings.HasPrefix(gen, "test:"):
		return fmt.Sprintf("I am %s, answering through the fake gateway as the model %q. When I read a chapter I look first for the sentence where the writer stopped trusting the reader, then for the one that earned its place. This reply is canned, so the real voice will have to wait for the real gateway.", name, req.Model)
	case strings.HasPrefix(gen, "cowrite:"):
		return "The lamp had been burning since before anyone remembered lighting it. She crossed the room without looking at it, the way you avoid looking at a person who has been talking for too long, and put her hand flat against the window to feel whether the cold outside was the honest kind. It was not. It was the kind that waits."
	default:
		return fmt.Sprintf("This is a canned reply from the fake gateway for %q using model %q.", gen, req.Model)
	}
}

// critiqueReply builds a valid critique that quotes real sentences from the
// chapter text found between the app's chapter markers in the last user
// message, so quote validation passes.
func critiqueReply(name string, msgs []message) string {
	var chapter string
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role != "user" {
			continue
		}
		c := msgs[i].Content
		if a := strings.Index(c, "--- CHAPTER TEXT BEGIN ---"); a >= 0 {
			c = c[a+len("--- CHAPTER TEXT BEGIN ---"):]
			if b := strings.Index(c, "--- CHAPTER TEXT END ---"); b >= 0 {
				c = c[:b]
			}
			chapter = strings.TrimSpace(c)
			break
		}
	}
	var sentences []string
	for _, line := range strings.Split(chapter, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "---") || strings.HasPrefix(line, "* * *") {
			continue
		}
		start := 0
		for i, r := range line {
			if r == '.' || r == '!' || r == '?' {
				if s := strings.TrimSpace(line[start : i+1]); len(s) > 12 {
					sentences = append(sentences, s)
				}
				start = i + 1
			}
		}
		if len(sentences) >= 6 {
			break
		}
	}
	type issue struct {
		ID           string `json:"id"`
		Severity     string `json:"severity"`
		Quote        string `json:"quote"`
		Problem      string `json:"problem"`
		SuggestedFix string `json:"suggested_fix"`
	}
	type conflict struct {
		Quote         string `json:"quote"`
		ConflictsWith string `json:"conflicts_with"`
	}
	out := map[string]any{
		"writer":          name,
		"overall":         fmt.Sprintf("A canned reading from %s through the fake gateway. The chapter moves, though a few sentences carry more than they need to.", name),
		"issues":          []issue{},
		"bible_conflicts": []conflict{},
	}
	sev := []string{"high", "medium", "low"}
	issues := []issue{}
	for i, s := range sentences {
		if i >= 3 {
			break
		}
		issues = append(issues, issue{ID: fmt.Sprintf("i%d", i+1), Severity: sev[i], Quote: s,
			Problem:      fmt.Sprintf("%s would stop here: the sentence tells the reader what the scene already shows.", name),
			SuggestedFix: "Cut the explanation and let the action stand; end the sentence one clause earlier."})
	}
	out["issues"] = issues
	if len(sentences) > 3 {
		out["bible_conflicts"] = []conflict{{Quote: sentences[3], ConflictsWith: "the story bible's note on this character (canned conflict from the fake gateway)"}}
	}
	b, _ := json.MarshalIndent(out, "", "  ")
	return string(b)
}

// leadWriterReply applies the accepted notes found in the prompt by
// tightening each quoted passage: the word before the last one is dropped,
// so the change stays inside the sentence the author accepted.
func leadWriterReply(msgs []message) string {
	var prompt string
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == "user" {
			prompt = msgs[i].Content
			break
		}
	}
	chapter := prompt
	if a := strings.Index(prompt, "--- CHAPTER TEXT BEGIN ---"); a >= 0 {
		chapter = prompt[a+len("--- CHAPTER TEXT BEGIN ---\n"):]
		if b := strings.Index(chapter, "--- CHAPTER TEXT END ---"); b >= 0 {
			chapter = chapter[:b]
		}
	}
	re := regexp.MustCompile(`passage: ("(?:[^"\\]|\\.)*")`)
	for _, m := range re.FindAllStringSubmatch(prompt, -1) {
		q, err := strconv.Unquote(m[1])
		if err != nil {
			continue
		}
		chapter = strings.Replace(chapter, q, tightenSentence(q), 1)
	}
	return chapter
}

// tightenSentence drops the second-to-last word of a sentence of four or
// more words, keeping its final punctuation.
func tightenSentence(s string) string {
	words := strings.Fields(s)
	if len(words) < 4 {
		return s + " Nothing more was said."
	}
	words = append(words[:len(words)-2], words[len(words)-1])
	return strings.Join(words, " ")
}

// editorReply merges the critics' notes found in the prompt: source ids are
// the "[slug/id]" labels, quotes are taken from the chapter text so they
// validate. The first issue cites two sources to exercise merging.
func editorReply(msgs []message) string {
	var prompt string
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Role == "user" {
			prompt = msgs[i].Content
			break
		}
	}
	re := regexp.MustCompile(`\[([a-z0-9-]+/[A-Za-z0-9_-]+)\] (high|medium|low) — quote: "((?:[^"\\]|\\.)*)"`)
	type src struct{ id, sev, quote string }
	var sources []src
	for _, m := range re.FindAllStringSubmatch(prompt, -1) {
		q, err := strconv.Unquote(`"` + m[3] + `"`)
		if err != nil {
			q = m[3]
		}
		sources = append(sources, src{m[1], m[2], q})
	}
	type issue struct {
		ID           string   `json:"id"`
		Severity     string   `json:"severity"`
		Quote        string   `json:"quote"`
		Problem      string   `json:"problem"`
		SuggestedFix string   `json:"suggested_fix"`
		Sources      []string `json:"sources"`
	}
	issues := []issue{}
	byQuote := map[string][]src{}
	var order []string
	for _, s := range sources {
		if _, ok := byQuote[s.quote]; !ok {
			order = append(order, s.quote)
		}
		byQuote[s.quote] = append(byQuote[s.quote], s)
	}
	for i, q := range order {
		if i >= 5 {
			break
		}
		group := byQuote[q]
		ids := make([]string, 0, len(group))
		for _, g := range group {
			ids = append(ids, g.id)
		}
		problem := fmt.Sprintf("%d critic(s) flagged this passage; the fake editor-in-chief agrees it carries more than it needs to.", len(group))
		issues = append(issues, issue{ID: fmt.Sprintf("e%d", i+1), Severity: group[0].sev, Quote: q, Problem: problem,
			SuggestedFix: "Trim the sentence to its action and let the reader supply the rest.", Sources: ids})
	}
	b, _ := json.MarshalIndent(map[string]any{"issues": issues}, "", "  ")
	return string(b)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg, typ string) {
	writeJSON(w, status, map[string]any{"error": map[string]any{"message": msg, "type": typ, "param": nil, "code": strconv.Itoa(status)}})
}
