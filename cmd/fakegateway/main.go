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
	case strings.HasPrefix(gen, "test:"):
		return fmt.Sprintf("I am %s, answering through the fake gateway as the model %q. When I read a chapter I look first for the sentence where the writer stopped trusting the reader, then for the one that earned its place. This reply is canned, so the real voice will have to wait for the real gateway.", name, req.Model)
	case strings.HasPrefix(gen, "cowrite:"):
		return "The lamp had been burning since before anyone remembered lighting it. She crossed the room without looking at it, the way you avoid looking at a person who has been talking for too long, and put her hand flat against the window to feel whether the cold outside was the honest kind. It was not. It was the kind that waits."
	default:
		return fmt.Sprintf("This is a canned reply from the fake gateway for %q using model %q.", gen, req.Model)
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, msg, typ string) {
	writeJSON(w, status, map[string]any{"error": map[string]any{"message": msg, "type": typ, "param": nil, "code": strconv.Itoa(status)}})
}
