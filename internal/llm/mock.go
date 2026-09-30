package llm

import (
	"context"
	"fmt"
	"strings"
	"sync"
)

// Mock is an in-memory Client for tests. Set CompleteFn or StreamFn to shape
// replies; every request is recorded.
type Mock struct {
	mu         sync.Mutex
	CompleteFn func(ctx context.Context, req Request) (*Response, error)
	StreamFn   func(ctx context.Context, req Request, onDelta func(string)) (*Response, error)
	Models     []string
	Prices     map[string]Price
	Err        error
	requests   []Request
}

// NewMock returns a Mock with a plain default reply.
func NewMock() *Mock {
	return &Mock{Models: []string{"writersguild-hemingway", "lumos-chat"}, Prices: map[string]Price{}}
}

func (m *Mock) record(req Request) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.requests = append(m.requests, req)
}

// Requests returns a copy of every recorded request.
func (m *Mock) Requests() []Request {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Request, len(m.requests))
	copy(out, m.requests)
	return out
}

func (m *Mock) defaultResponse(req Request) *Response {
	return &Response{
		Content:      fmt.Sprintf("Mock reply from %s to %q.", req.Model, req.Metadata.GenerationName),
		FinishReason: "stop",
		Model:        req.Model,
		Usage:        Usage{PromptTokens: 100, CompletionTokens: 50, TotalTokens: 150},
		CostUSD:      0.0003,
		CostKnown:    true,
	}
}

// Complete implements Client.
func (m *Mock) Complete(ctx context.Context, req Request) (*Response, error) {
	m.record(req)
	if m.Err != nil {
		return nil, m.Err
	}
	if m.CompleteFn != nil {
		return m.CompleteFn(ctx, req)
	}
	return m.defaultResponse(req), nil
}

// Stream implements Client. Without StreamFn it streams the Complete reply
// word by word.
func (m *Mock) Stream(ctx context.Context, req Request, onDelta func(string)) (*Response, error) {
	if m.StreamFn != nil {
		m.record(req)
		if m.Err != nil {
			return nil, m.Err
		}
		return m.StreamFn(ctx, req, onDelta)
	}
	resp, err := m.Complete(ctx, req)
	if err != nil {
		return nil, err
	}
	words := strings.SplitAfter(resp.Content, " ")
	for _, w := range words {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if onDelta != nil && w != "" {
			onDelta(w)
		}
	}
	// streamed replies carry no cost header: mark as estimated
	out := *resp
	out.CostEstimated = true
	return &out, nil
}

// ListModels implements Client.
func (m *Mock) ListModels(ctx context.Context) ([]string, error) {
	if m.Err != nil {
		return nil, m.Err
	}
	return append([]string(nil), m.Models...), nil
}

// ModelPrices implements Client.
func (m *Mock) ModelPrices(ctx context.Context) (map[string]Price, error) {
	if m.Err != nil {
		return nil, m.Err
	}
	return m.Prices, nil
}
