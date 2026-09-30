// Package llm is the only place that talks to the model gateway. Everything
// else depends on the Client interface, so tests use the Mock.
package llm

import (
	"context"
	"fmt"
	"time"
)

// Message is one chat turn.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// Metadata is forwarded by the gateway to Langfuse.
type Metadata struct {
	TraceID        string   `json:"trace_id,omitempty"`
	SessionID      string   `json:"session_id,omitempty"`
	GenerationName string   `json:"generation_name,omitempty"`
	TraceUserID    string   `json:"trace_user_id,omitempty"`
	Tags           []string `json:"tags,omitempty"`
}

// Request describes one completion.
type Request struct {
	Model       string
	Messages    []Message
	Temperature *float64
	MaxTokens   *int
	JSONMode    bool
	Metadata    Metadata
}

// Usage mirrors the OpenAI usage object.
type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// Response is a completed (or fully streamed) reply.
type Response struct {
	Content      string
	FinishReason string
	Model        string
	Usage        Usage
	// CostUSD is valid when CostKnown. CostEstimated marks a value derived
	// from token usage and gateway prices instead of the cost header.
	CostUSD       float64
	CostKnown     bool
	CostEstimated bool
	Latency       time.Duration
}

// Price is the per-token price of a gateway model.
type Price struct {
	InputPerToken  float64
	OutputPerToken float64
}

// Client is implemented by LiteLLM and Mock.
type Client interface {
	Complete(ctx context.Context, req Request) (*Response, error)
	Stream(ctx context.Context, req Request, onDelta func(delta string)) (*Response, error)
	ListModels(ctx context.Context) ([]string, error)
	ModelPrices(ctx context.Context) (map[string]Price, error)
}

// GatewayError is a non-2xx reply from the gateway.
type GatewayError struct {
	Status     int
	Message    string
	RetryAfter time.Duration
}

func (e *GatewayError) Error() string {
	return fmt.Sprintf("gateway returned %d: %s", e.Status, e.Message)
}

// Retryable reports whether the brief's backoff rule applies (429 or 5xx).
func (e *GatewayError) Retryable() bool {
	return e.Status == 429 || e.Status >= 500
}

// EstimateCost prices a usage record.
func EstimateCost(u Usage, p Price) float64 {
	return float64(u.PromptTokens)*p.InputPerToken + float64(u.CompletionTokens)*p.OutputPerToken
}

// Float64 and Int are small helpers for optional request fields.
func Float64(v float64) *float64 { return &v }
func Int(v int) *int             { return &v }
