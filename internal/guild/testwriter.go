package guild

import (
	"context"
	"time"

	"github.com/google/uuid"

	"writersguild/internal/db/sqlcgen"
	"writersguild/internal/llm"
	"writersguild/internal/runs"
	"writersguild/internal/text"
)

// TestWriterInput is a writer as it stands in the form, saved or not.
type TestWriterInput struct {
	WriterID     uuid.NullUUID
	Name         string
	Slug         string
	ModelAlias   string
	SystemPrompt string
	Temperature  float64
}

// TestWriterResult is what the "Test writer" button shows.
type TestWriterResult struct {
	RunID         uuid.UUID
	ModelAlias    string
	Reply         string
	Error         string
	Usage         llm.Usage
	CostUSD       float64
	CostKnown     bool
	CostEstimated bool
	Latency       time.Duration
}

const testPrompt = "In two or three sentences of your own words, introduce yourself as a member of the Writers' Guild and say what you look for first when you read a chapter. Do not quote anyone."

// TestWriter sends a short sample request through the gateway using the
// writer's alias and prompt, recording it as a writer_test run.
func (g *Guild) TestWriter(ctx context.Context, user sqlcgen.User, in TestWriterInput) (*TestWriterResult, error) {
	slug := in.Slug
	if slug == "" {
		slug = text.Slugify(in.Name)
	}
	run, err := g.tracker.Start(ctx, runs.StartParams{
		User: user,
		Kind: runs.KindWriterTest,
		Params: map[string]any{
			"writer_id":   nullableUUID(in.WriterID),
			"name":        in.Name,
			"slug":        slug,
			"model_alias": in.ModelAlias,
		},
	})
	if err != nil {
		return nil, err
	}
	req := llm.Request{
		Model:       in.ModelAlias,
		Temperature: llm.Float64(in.Temperature),
		MaxTokens:   llm.Int(300),
		Messages: []llm.Message{
			{Role: "system", Content: SystemPrompt(in.SystemPrompt)},
			{Role: "user", Content: testPrompt},
		},
	}
	resp, callErr := g.tracker.Call(ctx, run, runs.CallOpts{WriterID: in.WriterID, GenerationName: "test:" + slug}, req)
	res := &TestWriterResult{RunID: run.Row.ID, ModelAlias: in.ModelAlias}
	if callErr != nil {
		res.Error = FriendlyError(callErr)
		if _, err := g.tracker.Finish(ctx, run, nil, callErr); err != nil {
			return nil, err
		}
		return res, nil
	}
	res.Reply = resp.Content
	res.Usage = resp.Usage
	res.CostUSD, res.CostKnown, res.CostEstimated = resp.CostUSD, resp.CostKnown, resp.CostEstimated
	res.Latency = resp.Latency
	if _, err := g.tracker.Finish(ctx, run, map[string]any{"reply": resp.Content}, nil); err != nil {
		return nil, err
	}
	return res, nil
}

func nullableUUID(u uuid.NullUUID) any {
	if !u.Valid {
		return nil
	}
	return u.UUID.String()
}
