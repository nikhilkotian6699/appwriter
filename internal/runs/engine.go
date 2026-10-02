package runs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"

	"writersguild/internal/db/sqlcgen"
)

// Event is one stored step of a run. The sequence number is the SSE id, so a
// browser that reconnects asks for everything after the last one it saw.
type Event struct {
	RunID     uuid.UUID       `json:"run_id"`
	Seq       int             `json:"seq"`
	Type      string          `json:"type"`
	Payload   json.RawMessage `json:"payload"`
	CreatedAt time.Time       `json:"created_at"`
}

// Event types the engine itself emits. Workflows add their own.
const (
	EventRunStarted  = "run.started"
	EventRunFinished = "run.finished"
)

// FinishedPayload is the payload of the terminal run.finished event.
type FinishedPayload struct {
	Status           string  `json:"status"`
	Error            string  `json:"error,omitempty"`
	CostUSD          float64 `json:"cost_usd"`
	CostEstimated    bool    `json:"cost_estimated"`
	PromptTokens     int     `json:"prompt_tokens"`
	CompletionTokens int     `json:"completion_tokens"`
}

// WorkFn is a workflow body. It must return promptly once ctx is done; a
// context error marks the run cancelled, any other error marks it failed.
type WorkFn func(ctx context.Context, run *Run, em *Emitter) (result any, err error)

// Emitter appends events to one run.
type Emitter struct {
	e   *Engine
	run *Run
	seq atomic.Int32
	br  *broker
}

// Emit stores an event and delivers it to live subscribers. Storage uses a
// context that survives cancellation, so a cancelled run still records its
// last events.
func (em *Emitter) Emit(ctx context.Context, typ string, payload any) error {
	raw := json.RawMessage("{}")
	if payload != nil {
		b, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("runs: marshal event: %w", err)
		}
		raw = b
	}
	seq := int(em.seq.Add(1))
	row, err := em.e.q.AppendRunEvent(context.WithoutCancel(ctx), sqlcgen.AppendRunEventParams{
		RunID: em.run.Row.ID, Seq: int32(seq), Type: typ, Payload: raw,
	})
	if err != nil {
		return fmt.Errorf("runs: store event: %w", err)
	}
	em.br.publish(Event{RunID: row.RunID, Seq: int(row.Seq), Type: row.Type, Payload: row.Payload, CreatedAt: row.CreatedAt})
	return nil
}

// RunID is the run the emitter belongs to.
func (em *Emitter) RunID() uuid.UUID { return em.run.Row.ID }

type activeRun struct {
	cancel context.CancelFunc
	broker *broker
	done   chan struct{}
}

// Engine executes runs in the background, keeps their events and lets
// browsers follow or replay them.
type Engine struct {
	tracker *Tracker
	q       *sqlcgen.Queries
	log     *slog.Logger
	timeout time.Duration

	mu     sync.Mutex
	active map[uuid.UUID]*activeRun
	wg     sync.WaitGroup
}

// NewEngine wires an engine. timeout bounds one whole run; zero means none.
func NewEngine(tracker *Tracker, q *sqlcgen.Queries, timeout time.Duration, log *slog.Logger) *Engine {
	if log == nil {
		log = slog.Default()
	}
	return &Engine{tracker: tracker, q: q, log: log, timeout: timeout, active: map[uuid.UUID]*activeRun{}}
}

// Tracker exposes the run tracker for synchronous runs such as the writer test.
func (e *Engine) Tracker() *Tracker { return e.tracker }

// Launch creates the run and starts fn in the background. The returned run is
// a snapshot in the running state (the engine keeps its own copy up to date);
// follow it through Subscribe.
func (e *Engine) Launch(ctx context.Context, p StartParams, fn WorkFn) (*Run, error) {
	run, err := e.tracker.Start(ctx, p)
	if err != nil {
		return nil, err
	}
	// The run outlives the request: detach from its cancellation but keep its values.
	base := context.WithoutCancel(ctx)
	var cancel context.CancelFunc
	if e.timeout > 0 {
		base, cancel = context.WithTimeoutCause(base, e.timeout, errRunTimeout)
	} else {
		base, cancel = context.WithCancel(base)
	}
	ar := &activeRun{cancel: cancel, broker: newBroker(), done: make(chan struct{})}
	e.mu.Lock()
	e.active[run.Row.ID] = ar
	e.mu.Unlock()

	em := &Emitter{e: e, run: run, br: ar.broker}
	snapshot := *run
	if err := em.Emit(base, EventRunStarted, map[string]any{"kind": run.Row.Kind}); err != nil {
		e.finish(base, run, ar, nil, err)
		return &snapshot, nil
	}
	e.wg.Add(1)
	go func() {
		defer e.wg.Done()
		defer func() {
			if r := recover(); r != nil {
				e.log.Error("run panicked", "run", run.Row.ID, "panic", r)
				e.finish(base, run, ar, nil, fmt.Errorf("the run crashed: %v", r))
			}
		}()
		result, err := fn(base, run, em)
		e.finish(base, run, ar, result, err)
	}()
	return &snapshot, nil
}

var errRunTimeout = errors.New("the run did not finish within the time limit")

func (e *Engine) finish(ctx context.Context, run *Run, ar *activeRun, result any, err error) {
	if err != nil && errors.Is(err, context.DeadlineExceeded) {
		err = errRunTimeout
	}
	if err != nil && errors.Is(err, context.Canceled) {
		if cause := context.Cause(ctx); cause != nil && !errors.Is(cause, context.Canceled) {
			err = cause
		}
	}
	bg := context.WithoutCancel(ctx)
	row, ferr := e.tracker.Finish(bg, run, result, err)
	if ferr != nil {
		e.log.Error("finish run", "run", run.Row.ID, "err", ferr)
	}
	em := &Emitter{e: e, run: run, br: ar.broker}
	em.seq.Store(e.lastSeq(bg, run.Row.ID))
	payload := FinishedPayload{Status: row.Status, Error: row.Error, CostUSD: row.CostUsd, CostEstimated: row.CostEstimated,
		PromptTokens: int(row.PromptTokens), CompletionTokens: int(row.CompletionTokens)}
	if ferr != nil {
		payload.Status = StatusFailed
		payload.Error = "the run could not be recorded"
	}
	if eerr := em.Emit(bg, EventRunFinished, payload); eerr != nil {
		e.log.Error("emit finish", "run", run.Row.ID, "err", eerr)
	}
	ar.broker.close()
	ar.cancel()
	e.mu.Lock()
	delete(e.active, run.Row.ID)
	e.mu.Unlock()
	close(ar.done)
}

func (e *Engine) lastSeq(ctx context.Context, runID uuid.UUID) int32 {
	n, err := e.q.LastRunEventSeq(ctx, runID)
	if err != nil {
		return 0
	}
	return n
}

// Cancel stops a running run. It reports whether the run was active here.
func (e *Engine) Cancel(runID uuid.UUID) bool {
	e.mu.Lock()
	ar, ok := e.active[runID]
	e.mu.Unlock()
	if !ok {
		return false
	}
	ar.cancel()
	return true
}

// Active reports whether the run is executing in this process.
func (e *Engine) Active(runID uuid.UUID) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	_, ok := e.active[runID]
	return ok
}

// Wait blocks until the run has finished or ctx is done. It returns at once
// for a run this process is not executing.
func (e *Engine) Wait(ctx context.Context, runID uuid.UUID) error {
	e.mu.Lock()
	ar, ok := e.active[runID]
	e.mu.Unlock()
	if !ok {
		return nil
	}
	select {
	case <-ar.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Subscribe streams the run's events with a sequence number above after:
// first what the database holds, then live events until the run finishes.
// The channel closes when the run is over or ctx is done. A run that is not
// active here (finished, or interrupted by a restart) replays and closes.
func (e *Engine) Subscribe(ctx context.Context, runID uuid.UUID, after int) <-chan Event {
	out := make(chan Event, 64)
	go func() {
		defer close(out)
		last := after
		for {
			e.mu.Lock()
			ar, active := e.active[runID]
			e.mu.Unlock()
			var sub *subscriber
			if active {
				sub = ar.broker.subscribe() // nil when it closed in between
			}
			rows, err := e.q.ListRunEvents(ctx, sqlcgen.ListRunEventsParams{RunID: runID, Seq: int32(last)})
			if err != nil {
				if sub != nil {
					ar.broker.unsubscribe(sub)
				}
				if ctx.Err() == nil {
					e.log.Error("replay run events", "run", runID, "err", err)
				}
				return
			}
			for _, r := range rows {
				ev := Event{RunID: r.RunID, Seq: int(r.Seq), Type: r.Type, Payload: r.Payload, CreatedAt: r.CreatedAt}
				if ev.Seq <= last {
					continue
				}
				select {
				case out <- ev:
					last = ev.Seq
				case <-ctx.Done():
					if sub != nil {
						ar.broker.unsubscribe(sub)
					}
					return
				}
			}
			if sub == nil {
				return
			}
			lagged := false
			for {
				var ev Event
				var ok bool
				select {
				case ev, ok = <-sub.ch:
				case <-ctx.Done():
					ar.broker.unsubscribe(sub)
					return
				}
				if !ok {
					lagged = sub.lagged
					break
				}
				if ev.Seq <= last {
					continue
				}
				select {
				case out <- ev:
					last = ev.Seq
				case <-ctx.Done():
					ar.broker.unsubscribe(sub)
					return
				}
			}
			if !lagged {
				// The broker closed because the run finished; pick up any
				// event stored after the last one delivered live.
				rows, err := e.q.ListRunEvents(ctx, sqlcgen.ListRunEventsParams{RunID: runID, Seq: int32(last)})
				if err == nil {
					for _, r := range rows {
						select {
						case out <- Event{RunID: r.RunID, Seq: int(r.Seq), Type: r.Type, Payload: r.Payload, CreatedAt: r.CreatedAt}:
						case <-ctx.Done():
							return
						}
					}
				}
				return
			}
			// Lagged: loop to catch up from the database and subscribe again.
		}
	}()
	return out
}

// Shutdown cancels every active run and waits for them to finish recording,
// or for ctx to end.
func (e *Engine) Shutdown(ctx context.Context) error {
	e.mu.Lock()
	for _, ar := range e.active {
		ar.cancel()
	}
	e.mu.Unlock()
	done := make(chan struct{})
	go func() {
		e.wg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
