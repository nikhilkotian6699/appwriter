package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func newTestClient(t *testing.T, h http.Handler) (*LiteLLM, *[]time.Duration) {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	var sleeps []time.Duration
	c := NewLiteLLM(srv.URL, "test-key",
		WithRetry(3, time.Second, 10*time.Second),
		WithSleep(func(_ context.Context, d time.Duration) error { sleeps = append(sleeps, d); return nil }),
	)
	return c, &sleeps
}

func modelInfoHandler(w http.ResponseWriter) {
	json.NewEncoder(w).Encode(map[string]any{"data": []map[string]any{
		{"model_name": "writersguild-hemingway", "model_info": map[string]any{"input_cost_per_token": 0.000001, "output_cost_per_token": 0.000002}},
		{"model_name": "nullprice", "model_info": map[string]any{"input_cost_per_token": nil}},
	}})
}

func TestCompleteSendsMetadataAndReadsCostHeader(t *testing.T) {
	var got chatBody
	var auth string
	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" || r.Method != http.MethodPost {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		auth = r.Header.Get("Authorization")
		json.NewDecoder(r.Body).Decode(&got)
		w.Header().Set(CostHeader, "0.00042")
		json.NewEncoder(w).Encode(map[string]any{
			"model":   "gpt-x",
			"choices": []map[string]any{{"message": map[string]any{"role": "assistant", "content": "hello"}, "finish_reason": "stop"}},
			"usage":   map[string]any{"prompt_tokens": 10, "completion_tokens": 5, "total_tokens": 15},
		})
	}))
	resp, err := c.Complete(context.Background(), Request{
		Model:       "writersguild-hemingway",
		Messages:    []Message{{Role: "user", Content: "hi"}},
		JSONMode:    true,
		Temperature: Float64(0.3),
		Metadata: Metadata{TraceID: "run-1", SessionID: "chapter-1", GenerationName: "critic:hemingway",
			TraceUserID: "nikhil", Tags: []string{"writersguild", "Novel"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if auth != "Bearer test-key" {
		t.Errorf("auth header = %q", auth)
	}
	if got.Metadata.TraceID != "run-1" || got.Metadata.SessionID != "chapter-1" || got.Metadata.GenerationName != "critic:hemingway" ||
		got.Metadata.TraceUserID != "nikhil" || len(got.Metadata.Tags) != 2 {
		t.Errorf("metadata not forwarded: %+v", got.Metadata)
	}
	if got.ResponseFormat == nil || got.ResponseFormat.Type != "json_object" {
		t.Errorf("response_format missing: %+v", got.ResponseFormat)
	}
	if got.Stream {
		t.Error("stream must be false for Complete")
	}
	if resp.Content != "hello" || resp.Usage.CompletionTokens != 5 || resp.Model != "gpt-x" || resp.FinishReason != "stop" {
		t.Errorf("bad response: %+v", resp)
	}
	if !resp.CostKnown || resp.CostEstimated || resp.CostUSD != 0.00042 {
		t.Errorf("cost from header not used: %+v", resp)
	}
}

func TestCompleteEstimatesCostWhenHeaderMissing(t *testing.T) {
	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/model/info":
			modelInfoHandler(w)
		default:
			json.NewEncoder(w).Encode(map[string]any{
				"choices": []map[string]any{{"message": map[string]any{"content": "x"}}},
				"usage":   map[string]any{"prompt_tokens": 1000, "completion_tokens": 500},
			})
		}
	}))
	resp, err := c.Complete(context.Background(), Request{Model: "writersguild-hemingway", Messages: []Message{{Role: "user", Content: "hi"}}})
	if err != nil {
		t.Fatal(err)
	}
	want := 1000*0.000001 + 500*0.000002
	if !resp.CostKnown || !resp.CostEstimated || fmt.Sprintf("%.9f", resp.CostUSD) != fmt.Sprintf("%.9f", want) {
		t.Errorf("estimate wrong: %+v want %v", resp, want)
	}
	// unknown alias: no price, cost unknown
	resp, err = c.Complete(context.Background(), Request{Model: "unknown", Messages: []Message{{Role: "user", Content: "hi"}}})
	if err != nil {
		t.Fatal(err)
	}
	if resp.CostKnown {
		t.Errorf("cost should be unknown for unpriced alias: %+v", resp)
	}
}

func TestRetryOn429HonoursRetryAfter(t *testing.T) {
	var calls int32
	c, sleeps := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&calls, 1)
		if n == 1 {
			w.Header().Set("Retry-After", "7")
			w.WriteHeader(429)
			w.Write([]byte(`{"error":{"message":"rate limited"}}`))
			return
		}
		if n == 2 {
			w.WriteHeader(503)
			w.Write([]byte(`upstream down`))
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"choices": []map[string]any{{"message": map[string]any{"content": "ok"}}}})
	}))
	resp, err := c.Complete(context.Background(), Request{Model: "m", Messages: []Message{{Role: "user", Content: "hi"}}})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Content != "ok" || atomic.LoadInt32(&calls) != 3 {
		t.Fatalf("expected success on 3rd call, got calls=%d resp=%+v", calls, resp)
	}
	if len(*sleeps) != 2 || (*sleeps)[0] != 7*time.Second || (*sleeps)[1] != 2*time.Second {
		t.Fatalf("sleeps = %v, want [7s 2s]", *sleeps)
	}
}

func TestNoRetryOn400(t *testing.T) {
	var calls int32
	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(400)
		w.Write([]byte(`{"error":{"message":"Invalid model name passed in model=nope","type":"invalid_request_error"}}`))
	}))
	_, err := c.Complete(context.Background(), Request{Model: "nope", Messages: []Message{{Role: "user", Content: "hi"}}})
	var ge *GatewayError
	if !errors.As(err, &ge) || ge.Status != 400 || !strings.Contains(ge.Message, "Invalid model name") {
		t.Fatalf("want GatewayError 400 with message, got %v", err)
	}
	if calls != 1 {
		t.Fatalf("400 must not retry, calls=%d", calls)
	}
}

func TestRetriesExhausted(t *testing.T) {
	c, sleeps := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
	}))
	_, err := c.Complete(context.Background(), Request{Model: "m", Messages: []Message{{Role: "user", Content: "hi"}}})
	var ge *GatewayError
	if !errors.As(err, &ge) || ge.Status != 500 {
		t.Fatalf("want 500 after retries, got %v", err)
	}
	if len(*sleeps) != 2 {
		t.Fatalf("3 attempts should sleep twice, got %v", *sleeps)
	}
}

func TestStreamCollectsDeltasAndUsage(t *testing.T) {
	var got chatBody
	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/model/info" {
			modelInfoHandler(w)
			return
		}
		json.NewDecoder(r.Body).Decode(&got)
		w.Header().Set("Content-Type", "text/event-stream")
		fmt.Fprint(w, ": keep-alive\n\n")
		fmt.Fprint(w, `data: {"model":"gpt-x","choices":[{"delta":{"role":"assistant","content":"Hel"},"finish_reason":null}]}`+"\n\n")
		fmt.Fprint(w, `data: {"choices":[{"delta":{"content":"lo"},"finish_reason":null}]}`+"\n\n")
		fmt.Fprint(w, `data: {"choices":[{"delta":{},"finish_reason":"stop"}]}`+"\n\n")
		fmt.Fprint(w, `data: {"choices":[],"usage":{"prompt_tokens":100,"completion_tokens":10,"total_tokens":110}}`+"\n\n")
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	var deltas []string
	resp, err := c.Stream(context.Background(), Request{Model: "writersguild-hemingway", Messages: []Message{{Role: "user", Content: "hi"}}},
		func(d string) { deltas = append(deltas, d) })
	if err != nil {
		t.Fatal(err)
	}
	if !got.Stream || got.StreamOptions == nil || !got.StreamOptions.IncludeUsage {
		t.Errorf("stream flags missing: %+v", got)
	}
	if strings.Join(deltas, "") != "Hello" || resp.Content != "Hello" || resp.FinishReason != "stop" {
		t.Errorf("deltas=%v resp=%+v", deltas, resp)
	}
	if resp.Usage.PromptTokens != 100 || resp.Usage.CompletionTokens != 10 {
		t.Errorf("usage not parsed: %+v", resp.Usage)
	}
	if !resp.CostKnown || !resp.CostEstimated {
		t.Errorf("streamed cost should be estimated: %+v", resp)
	}
}

func TestStreamRetriesBeforeFirstByte(t *testing.T) {
	var calls int32
	c, sleeps := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if atomic.AddInt32(&calls, 1) == 1 {
			w.WriteHeader(502)
			return
		}
		fmt.Fprint(w, `data: {"choices":[{"delta":{"content":"ok"}}]}`+"\n\ndata: [DONE]\n\n")
	}))
	resp, err := c.Stream(context.Background(), Request{Model: "m", Messages: []Message{{Role: "user", Content: "hi"}}}, nil)
	if err != nil || resp.Content != "ok" {
		t.Fatalf("err=%v resp=%+v", err, resp)
	}
	if len(*sleeps) != 1 {
		t.Fatalf("expected one backoff sleep, got %v", *sleeps)
	}
}

func TestListModelsAndPricesCache(t *testing.T) {
	var infoCalls int32
	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models":
			json.NewEncoder(w).Encode(map[string]any{"data": []map[string]any{{"id": "writersguild-hemingway"}, {"id": "lumos-chat"}}})
		case "/model/info":
			atomic.AddInt32(&infoCalls, 1)
			modelInfoHandler(w)
		}
	}))
	models, err := c.ListModels(context.Background())
	if err != nil || len(models) != 2 || models[1] != "lumos-chat" {
		t.Fatalf("models=%v err=%v", models, err)
	}
	for i := 0; i < 3; i++ {
		p, err := c.ModelPrices(context.Background())
		if err != nil || p["writersguild-hemingway"].OutputPerToken != 0.000002 || p["nullprice"].InputPerToken != 0 {
			t.Fatalf("prices=%v err=%v", p, err)
		}
	}
	if infoCalls != 1 {
		t.Fatalf("model info should be cached, calls=%d", infoCalls)
	}
}

func TestParseRetryAfter(t *testing.T) {
	now := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	if d := parseRetryAfter("5", now); d != 5*time.Second {
		t.Errorf("seconds: %v", d)
	}
	if d := parseRetryAfter(now.Add(90*time.Second).UTC().Format(http.TimeFormat), now); d < 89*time.Second || d > 91*time.Second {
		t.Errorf("http date: %v", d)
	}
	if d := parseRetryAfter("garbage", now); d != 0 {
		t.Errorf("garbage: %v", d)
	}
	if d := parseRetryAfter("", now); d != 0 {
		t.Errorf("empty: %v", d)
	}
}

func TestContextCancelStopsRetry(t *testing.T) {
	c, _ := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(500) }))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := c.Complete(ctx, Request{Model: "m", Messages: []Message{{Role: "user", Content: "hi"}}})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestMockStreamsWords(t *testing.T) {
	m := NewMock()
	var got []string
	resp, err := m.Stream(context.Background(), Request{Model: "x", Metadata: Metadata{GenerationName: "test:x"}}, func(d string) { got = append(got, d) })
	if err != nil || strings.Join(got, "") != resp.Content || !resp.CostEstimated || len(m.Requests()) != 1 {
		t.Fatalf("got=%v resp=%+v err=%v", got, resp, err)
	}
}
