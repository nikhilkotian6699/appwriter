package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// CostHeader is set by LiteLLM on non-streamed replies.
const CostHeader = "x-litellm-response-cost"

// LiteLLM talks to a LiteLLM gateway over its OpenAI-compatible API.
type LiteLLM struct {
	baseURL     string
	apiKey      string
	http        *http.Client
	maxAttempts int
	backoffBase time.Duration
	backoffMax  time.Duration
	priceTTL    time.Duration
	sleep       func(context.Context, time.Duration) error
	now         func() time.Time

	mu       sync.Mutex
	prices   map[string]Price
	pricesAt time.Time
}

// Option configures LiteLLM.
type Option func(*LiteLLM)

// WithHTTPClient replaces the HTTP client (tests, custom transports).
func WithHTTPClient(c *http.Client) Option { return func(l *LiteLLM) { l.http = c } }

// WithRetry sets the attempt budget and backoff window for 429/5xx replies.
func WithRetry(maxAttempts int, base, max time.Duration) Option {
	return func(l *LiteLLM) { l.maxAttempts, l.backoffBase, l.backoffMax = maxAttempts, base, max }
}

// WithSleep replaces the wait function so tests run instantly.
func WithSleep(fn func(context.Context, time.Duration) error) Option {
	return func(l *LiteLLM) { l.sleep = fn }
}

// WithPriceTTL sets how long GET /model/info is cached.
func WithPriceTTL(d time.Duration) Option { return func(l *LiteLLM) { l.priceTTL = d } }

// NewLiteLLM builds a client. The API key may be empty for gateways that do
// not need one (the fake gateway).
func NewLiteLLM(baseURL, apiKey string, opts ...Option) *LiteLLM {
	l := &LiteLLM{
		baseURL:     strings.TrimRight(baseURL, "/"),
		apiKey:      apiKey,
		http:        &http.Client{},
		maxAttempts: 4,
		backoffBase: time.Second,
		backoffMax:  30 * time.Second,
		priceTTL:    5 * time.Minute,
		sleep:       sleepCtx,
		now:         time.Now,
	}
	for _, o := range opts {
		o(l)
	}
	return l
}

type responseFormat struct {
	Type string `json:"type"`
}

type streamOptions struct {
	IncludeUsage bool `json:"include_usage"`
}

type chatBody struct {
	Model          string          `json:"model"`
	Messages       []Message       `json:"messages"`
	Temperature    *float64        `json:"temperature,omitempty"`
	MaxTokens      *int            `json:"max_tokens,omitempty"`
	ResponseFormat *responseFormat `json:"response_format,omitempty"`
	Stream         bool            `json:"stream,omitempty"`
	StreamOptions  *streamOptions  `json:"stream_options,omitempty"`
	Metadata       Metadata        `json:"metadata"`
}

type chatResponse struct {
	Model   string `json:"model"`
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage Usage `json:"usage"`
}

type streamChunk struct {
	Model   string `json:"model"`
	Choices []struct {
		Delta struct {
			Content *string `json:"content"`
		} `json:"delta"`
		FinishReason *string `json:"finish_reason"`
	} `json:"choices"`
	Usage *Usage `json:"usage"`
}

// permanentError stops the retry loop.
type permanentError struct{ err error }

func (p *permanentError) Error() string { return p.err.Error() }
func (p *permanentError) Unwrap() error { return p.err }

func (l *LiteLLM) body(req Request, stream bool) chatBody {
	b := chatBody{
		Model:       req.Model,
		Messages:    req.Messages,
		Temperature: req.Temperature,
		MaxTokens:   req.MaxTokens,
		Metadata:    req.Metadata,
		Stream:      stream,
	}
	if req.JSONMode {
		b.ResponseFormat = &responseFormat{Type: "json_object"}
	}
	if stream {
		b.StreamOptions = &streamOptions{IncludeUsage: true}
	}
	return b
}

// Complete performs a non-streamed completion.
func (l *LiteLLM) Complete(ctx context.Context, req Request) (*Response, error) {
	if req.Model == "" {
		return nil, errors.New("llm: model alias is empty")
	}
	body := l.body(req, false)
	var out *Response
	err := l.withRetry(ctx, func() error {
		start := l.now()
		httpResp, err := l.post(ctx, "/v1/chat/completions", body)
		if err != nil {
			return err
		}
		defer httpResp.Body.Close()
		if httpResp.StatusCode >= 300 {
			return gatewayError(httpResp)
		}
		var cr chatResponse
		if err := json.NewDecoder(httpResp.Body).Decode(&cr); err != nil {
			return &permanentError{fmt.Errorf("llm: decode reply: %w", err)}
		}
		out = &Response{Model: cr.Model, Usage: cr.Usage, Latency: l.now().Sub(start)}
		if len(cr.Choices) > 0 {
			out.Content = cr.Choices[0].Message.Content
			out.FinishReason = cr.Choices[0].FinishReason
		}
		l.fillCost(ctx, out, req.Model, httpResp.Header)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// Stream performs a streamed completion, calling onDelta for each text
// fragment. Retries happen only before the first fragment arrived.
func (l *LiteLLM) Stream(ctx context.Context, req Request, onDelta func(string)) (*Response, error) {
	if req.Model == "" {
		return nil, errors.New("llm: model alias is empty")
	}
	body := l.body(req, true)
	var out *Response
	err := l.withRetry(ctx, func() error {
		start := l.now()
		httpResp, err := l.post(ctx, "/v1/chat/completions", body)
		if err != nil {
			return err
		}
		defer httpResp.Body.Close()
		if httpResp.StatusCode >= 300 {
			return gatewayError(httpResp)
		}
		delivered := false
		resp, err := readSSE(httpResp.Body, func(d string) {
			delivered = true
			if onDelta != nil {
				onDelta(d)
			}
		})
		if err != nil {
			if delivered || ctx.Err() != nil {
				return &permanentError{err}
			}
			return err
		}
		resp.Latency = l.now().Sub(start)
		l.fillCost(ctx, resp, req.Model, httpResp.Header)
		out = resp
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

func readSSE(r io.Reader, onDelta func(string)) (*Response, error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	resp := &Response{}
	var content strings.Builder
	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "" {
			continue
		}
		if data == "[DONE]" {
			break
		}
		var ch streamChunk
		if err := json.Unmarshal([]byte(data), &ch); err != nil {
			return nil, fmt.Errorf("llm: bad stream chunk: %w", err)
		}
		if ch.Model != "" {
			resp.Model = ch.Model
		}
		for _, c := range ch.Choices {
			if c.Delta.Content != nil && *c.Delta.Content != "" {
				content.WriteString(*c.Delta.Content)
				onDelta(*c.Delta.Content)
			}
			if c.FinishReason != nil && *c.FinishReason != "" {
				resp.FinishReason = *c.FinishReason
			}
		}
		if ch.Usage != nil {
			resp.Usage = *ch.Usage
		}
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("llm: read stream: %w", err)
	}
	resp.Content = content.String()
	return resp, nil
}

// fillCost reads the cost header or estimates from cached prices.
func (l *LiteLLM) fillCost(ctx context.Context, resp *Response, alias string, h http.Header) {
	if v := strings.TrimSpace(h.Get(CostHeader)); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			resp.CostUSD, resp.CostKnown, resp.CostEstimated = f, true, false
			return
		}
	}
	if resp.Usage.PromptTokens == 0 && resp.Usage.CompletionTokens == 0 {
		return
	}
	prices, err := l.ModelPrices(ctx)
	if err != nil {
		return
	}
	for _, key := range []string{alias, resp.Model} {
		if p, ok := prices[key]; ok && key != "" {
			resp.CostUSD, resp.CostKnown, resp.CostEstimated = EstimateCost(resp.Usage, p), true, true
			return
		}
	}
}

// ListModels returns every model id the gateway exposes.
func (l *LiteLLM) ListModels(ctx context.Context) ([]string, error) {
	httpResp, err := l.get(ctx, "/v1/models")
	if err != nil {
		return nil, err
	}
	defer httpResp.Body.Close()
	if httpResp.StatusCode >= 300 {
		return nil, gatewayError(httpResp)
	}
	var body struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(httpResp.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("llm: decode models: %w", err)
	}
	ids := make([]string, 0, len(body.Data))
	for _, d := range body.Data {
		if d.ID != "" {
			ids = append(ids, d.ID)
		}
	}
	return ids, nil
}

// ModelPrices returns per-token prices from GET /model/info, cached.
func (l *LiteLLM) ModelPrices(ctx context.Context) (map[string]Price, error) {
	l.mu.Lock()
	if l.prices != nil && l.now().Sub(l.pricesAt) < l.priceTTL {
		p := l.prices
		l.mu.Unlock()
		return p, nil
	}
	l.mu.Unlock()

	httpResp, err := l.get(ctx, "/model/info")
	if err != nil {
		return l.stalePrices(err)
	}
	defer httpResp.Body.Close()
	if httpResp.StatusCode >= 300 {
		return l.stalePrices(gatewayError(httpResp))
	}
	var body struct {
		Data []struct {
			ModelName string `json:"model_name"`
			ModelInfo struct {
				InputCostPerToken  *float64 `json:"input_cost_per_token"`
				OutputCostPerToken *float64 `json:"output_cost_per_token"`
			} `json:"model_info"`
		} `json:"data"`
	}
	if err := json.NewDecoder(httpResp.Body).Decode(&body); err != nil {
		return l.stalePrices(fmt.Errorf("llm: decode model info: %w", err))
	}
	prices := make(map[string]Price, len(body.Data))
	for _, d := range body.Data {
		if d.ModelName == "" {
			continue
		}
		var p Price
		if d.ModelInfo.InputCostPerToken != nil {
			p.InputPerToken = *d.ModelInfo.InputCostPerToken
		}
		if d.ModelInfo.OutputCostPerToken != nil {
			p.OutputPerToken = *d.ModelInfo.OutputCostPerToken
		}
		prices[d.ModelName] = p
	}
	l.mu.Lock()
	l.prices, l.pricesAt = prices, l.now()
	l.mu.Unlock()
	return prices, nil
}

func (l *LiteLLM) stalePrices(err error) (map[string]Price, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.prices != nil {
		return l.prices, nil
	}
	return nil, err
}

func (l *LiteLLM) withRetry(ctx context.Context, attempt func() error) error {
	var last error
	for i := 1; i <= l.maxAttempts; i++ {
		err := attempt()
		if err == nil {
			return nil
		}
		last = err
		if ctx.Err() != nil {
			return ctx.Err()
		}
		var perm *permanentError
		if errors.As(err, &perm) {
			return perm.err
		}
		wait := l.backoffBase * (1 << (i - 1))
		if wait > l.backoffMax {
			wait = l.backoffMax
		}
		var ge *GatewayError
		if errors.As(err, &ge) {
			if !ge.Retryable() {
				return err
			}
			if ge.RetryAfter > 0 {
				wait = ge.RetryAfter
			}
		}
		if i == l.maxAttempts {
			break
		}
		if err := l.sleep(ctx, wait); err != nil {
			return err
		}
	}
	return last
}

func (l *LiteLLM) post(ctx context.Context, path string, body any) (*http.Response, error) {
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, &permanentError{err}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, l.baseURL+path, bytes.NewReader(raw))
	if err != nil {
		return nil, &permanentError{err}
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	l.auth(req)
	return l.http.Do(req)
}

func (l *LiteLLM) get(ctx context.Context, path string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, l.baseURL+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	l.auth(req)
	return l.http.Do(req)
}

func (l *LiteLLM) auth(req *http.Request) {
	if l.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+l.apiKey)
	}
}

func gatewayError(resp *http.Response) error {
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	msg := strings.TrimSpace(string(raw))
	var parsed struct {
		Error  any `json:"error"`
		Detail any `json:"detail"`
	}
	if json.Unmarshal(raw, &parsed) == nil {
		switch e := parsed.Error.(type) {
		case map[string]any:
			if m, ok := e["message"].(string); ok && m != "" {
				msg = m
			}
		case string:
			if e != "" {
				msg = e
			}
		}
		if msg == strings.TrimSpace(string(raw)) {
			switch d := parsed.Detail.(type) {
			case string:
				if d != "" {
					msg = d
				}
			case map[string]any:
				if m, ok := d["error"].(string); ok && m != "" {
					msg = m
				}
			}
		}
	}
	if msg == "" {
		msg = http.StatusText(resp.StatusCode)
	}
	return &GatewayError{Status: resp.StatusCode, Message: msg, RetryAfter: parseRetryAfter(resp.Header.Get("Retry-After"), time.Now())}
}

// parseRetryAfter accepts delay-seconds or an HTTP date.
func parseRetryAfter(v string, now time.Time) time.Duration {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0
	}
	if secs, err := strconv.Atoi(v); err == nil {
		if secs < 0 {
			return 0
		}
		return time.Duration(secs) * time.Second
	}
	if t, err := http.ParseTime(v); err == nil {
		if d := t.Sub(now); d > 0 {
			return d
		}
	}
	return 0
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
