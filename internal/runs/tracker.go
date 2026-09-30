// Package runs owns the run records: every workflow is a run, and every
// gateway request made on behalf of a run is recorded as a model call.
package runs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"writersguild/internal/db/sqlcgen"
	"writersguild/internal/llm"
)

// Run kinds and statuses, matching the database check constraints.
const (
	KindCritique    = "critique"
	KindRevision    = "revision"
	KindBibleUpdate = "bible_update"
	KindCowrite     = "cowrite"
	KindCompare     = "compare"
	KindWriterTest  = "writer_test"

	StatusQueued    = "queued"
	StatusRunning   = "running"
	StatusSucceeded = "succeeded"
	StatusFailed    = "failed"
	StatusCancelled = "cancelled"
)

// Tracker creates runs, performs recorded gateway calls and finishes runs.
type Tracker struct {
	q       *sqlcgen.Queries
	client  llm.Client
	appName string
}

// NewTracker wires the tracker.
func NewTracker(q *sqlcgen.Queries, client llm.Client, appName string) *Tracker {
	return &Tracker{q: q, client: client, appName: appName}
}

// Client exposes the underlying gateway client for calls that are not runs
// (listing models).
func (t *Tracker) Client() llm.Client { return t.client }

// StartParams describes a new run.
type StartParams struct {
	User        sqlcgen.User
	ProjectID   uuid.NullUUID
	ProjectName string
	ChapterID   uuid.NullUUID
	Kind        string
	Params      any
}

// Run is a started run plus what the metadata needs.
type Run struct {
	Row         sqlcgen.Run
	Username    string
	ProjectName string
}

// Start inserts a run in the running state. The run id doubles as the
// Langfuse trace id.
func (t *Tracker) Start(ctx context.Context, p StartParams) (*Run, error) {
	params, err := json.Marshal(p.Params)
	if err != nil {
		return nil, fmt.Errorf("runs: marshal params: %w", err)
	}
	if p.Params == nil {
		params = []byte("{}")
	}
	id := uuid.New()
	now := time.Now()
	row, err := t.q.CreateRun(ctx, sqlcgen.CreateRunParams{
		ID:        id,
		UserID:    p.User.ID,
		ProjectID: p.ProjectID,
		ChapterID: p.ChapterID,
		Kind:      p.Kind,
		Status:    StatusRunning,
		TraceID:   id.String(),
		Params:    params,
		StartedAt: &now,
	})
	if err != nil {
		return nil, fmt.Errorf("runs: create: %w", err)
	}
	return &Run{Row: row, Username: p.User.Username, ProjectName: p.ProjectName}, nil
}

// CallOpts names the writer and generation for one gateway call.
type CallOpts struct {
	WriterID       uuid.NullUUID
	GenerationName string
	// OnDelta switches the call to streaming when set.
	OnDelta func(delta string)
}

// Call performs one gateway request for the run, fills in the Langfuse
// metadata, and records a model_calls row whether it succeeded or failed.
func (t *Tracker) Call(ctx context.Context, run *Run, opts CallOpts, req llm.Request) (*llm.Response, error) {
	req.Metadata = t.metadata(run, opts.GenerationName)
	start := time.Now()
	var resp *llm.Response
	var err error
	if opts.OnDelta != nil {
		resp, err = t.client.Stream(ctx, req, opts.OnDelta)
	} else {
		resp, err = t.client.Complete(ctx, req)
	}
	latency := time.Since(start)

	// Bookkeeping must survive a cancelled request context.
	bg := context.WithoutCancel(ctx)
	call := sqlcgen.CreateModelCallParams{
		UserID:         run.Row.UserID,
		RunID:          uuid.NullUUID{UUID: run.Row.ID, Valid: true},
		WriterID:       opts.WriterID,
		GenerationName: opts.GenerationName,
		ModelAlias:     req.Model,
		LatencyMs:      int32(latency.Milliseconds()),
		Status:         "ok",
	}
	if err != nil {
		call.Status = "error"
		call.Error = err.Error()
	}
	if resp != nil {
		call.PromptTokens = int32(resp.Usage.PromptTokens)
		call.CompletionTokens = int32(resp.Usage.CompletionTokens)
		if resp.CostKnown {
			call.CostUsd = resp.CostUSD
			call.CostEstimated = resp.CostEstimated
		}
	}
	if _, dbErr := t.q.CreateModelCall(bg, call); dbErr != nil {
		return resp, errors.Join(err, fmt.Errorf("runs: record call: %w", dbErr))
	}
	if resp != nil {
		if dbErr := t.q.AddRunUsage(bg, sqlcgen.AddRunUsageParams{
			ID:               run.Row.ID,
			CostUsd:          call.CostUsd,
			CostEstimated:    call.CostEstimated,
			PromptTokens:     call.PromptTokens,
			CompletionTokens: call.CompletionTokens,
		}); dbErr != nil {
			return resp, errors.Join(err, fmt.Errorf("runs: add usage: %w", dbErr))
		}
	}
	return resp, err
}

func (t *Tracker) metadata(run *Run, generation string) llm.Metadata {
	m := llm.Metadata{
		TraceID:        run.Row.TraceID,
		GenerationName: generation,
		TraceUserID:    run.Username,
		Tags:           []string{t.appName},
	}
	if run.Row.ChapterID.Valid {
		m.SessionID = run.Row.ChapterID.UUID.String()
	}
	if run.ProjectName != "" {
		m.Tags = append(m.Tags, run.ProjectName)
	}
	return m
}

// Finish closes the run. A nil runErr means success; a context error means
// cancelled; anything else means failed with the error text stored.
func (t *Tracker) Finish(ctx context.Context, run *Run, result any, runErr error) (sqlcgen.Run, error) {
	bg := context.WithoutCancel(ctx)
	var resJSON []byte
	if result != nil {
		b, err := json.Marshal(result)
		if err != nil {
			return sqlcgen.Run{}, fmt.Errorf("runs: marshal result: %w", err)
		}
		resJSON = b
	}
	status, msg := StatusSucceeded, ""
	if runErr != nil {
		msg = runErr.Error()
		if errors.Is(runErr, context.Canceled) {
			status = StatusCancelled
		} else {
			status = StatusFailed
		}
	}
	row, err := t.q.FinishRun(bg, sqlcgen.FinishRunParams{ID: run.Row.ID, Status: status, Result: resJSON, Error: msg})
	if err != nil {
		return sqlcgen.Run{}, fmt.Errorf("runs: finish: %w", err)
	}
	run.Row = row
	return row, nil
}
