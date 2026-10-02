package runs

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"writersguild/internal/db/sqlcgen"
	"writersguild/internal/llm"
	"writersguild/internal/testutil"
)

func newEngine(t *testing.T, timeout time.Duration) (*Engine, sqlcgen.User) {
	t.Helper()
	pool := testutil.DB(t)
	q := sqlcgen.New(pool)
	user := testutil.NewUser(t, q, "author")
	tracker := NewTracker(q, llm.NewMock(), "writersguild")
	return NewEngine(tracker, q, timeout, slog.New(slog.NewTextHandler(io.Discard, nil))), user
}

func collect(t *testing.T, ch <-chan Event, within time.Duration) []Event {
	t.Helper()
	var out []Event
	deadline := time.After(within)
	for {
		select {
		case ev, ok := <-ch:
			if !ok {
				return out
			}
			out = append(out, ev)
		case <-deadline:
			t.Fatalf("stream did not close within %s; got %d events", within, len(out))
		}
	}
}

func seqs(evs []Event) []int {
	out := make([]int, len(evs))
	for i, e := range evs {
		out[i] = e.Seq
	}
	return out
}

func TestIntegrationEngineReplayAndLive(t *testing.T) {
	e, user := newEngine(t, 30*time.Second)
	ctx := context.Background()
	release := make(chan struct{})
	run, err := e.Launch(ctx, StartParams{User: user, Kind: KindCritique, Params: map[string]any{"n": 5}}, func(ctx context.Context, run *Run, em *Emitter) (any, error) {
		for i := 1; i <= 3; i++ {
			if err := em.Emit(ctx, "step", map[string]int{"i": i}); err != nil {
				return nil, err
			}
		}
		<-release
		for i := 4; i <= 5; i++ {
			if err := em.Emit(ctx, "step", map[string]int{"i": i}); err != nil {
				return nil, err
			}
		}
		return map[string]string{"done": "yes"}, nil
	})
	if err != nil {
		t.Fatal(err)
	}
	// Wait until the first three steps are stored.
	deadline := time.Now().Add(5 * time.Second)
	for {
		rows, _ := e.q.ListRunEvents(ctx, sqlcgen.ListRunEventsParams{RunID: run.Row.ID, Seq: 0})
		if len(rows) >= 4 || time.Now().After(deadline) {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	// A subscriber arriving mid-run replays seq 1..4 then follows live.
	mid := e.Subscribe(ctx, run.Row.ID, 2)
	close(release)
	got := collect(t, mid, 5*time.Second)
	want := []int{3, 4, 5, 6, 7} // run.started=1, steps 2..6, run.finished=7
	if len(got) != len(want) {
		t.Fatalf("seqs %v, want %v", seqs(got), want)
	}
	for i := range want {
		if got[i].Seq != want[i] {
			t.Fatalf("seqs %v, want %v", seqs(got), want)
		}
	}
	last := got[len(got)-1]
	if last.Type != EventRunFinished {
		t.Fatalf("last event %q, want run.finished", last.Type)
	}
	var fp FinishedPayload
	_ = json.Unmarshal(last.Payload, &fp)
	if fp.Status != StatusSucceeded {
		t.Fatalf("finished status %q: %s", fp.Status, fp.Error)
	}
	if e.Active(run.Row.ID) {
		t.Fatal("run still active after finish")
	}
	// A late subscriber gets a full replay from the database and the stream closes.
	late := collect(t, e.Subscribe(ctx, run.Row.ID, 0), 5*time.Second)
	if len(late) != 7 || late[0].Type != EventRunStarted || late[6].Type != EventRunFinished {
		t.Fatalf("late replay seqs %v", seqs(late))
	}
	row, err := e.q.GetRun(ctx, sqlcgen.GetRunParams{ID: run.Row.ID, UserID: user.ID})
	if err != nil {
		t.Fatal(err)
	}
	var result map[string]string
	_ = json.Unmarshal(row.Result, &result)
	if row.Status != StatusSucceeded || result["done"] != "yes" {
		t.Fatalf("run row status %q result %s", row.Status, row.Result)
	}
}

func TestIntegrationEngineCancel(t *testing.T) {
	e, user := newEngine(t, 30*time.Second)
	ctx := context.Background()
	started := make(chan struct{})
	run, err := e.Launch(ctx, StartParams{User: user, Kind: KindCowrite}, func(ctx context.Context, run *Run, em *Emitter) (any, error) {
		close(started)
		<-ctx.Done()
		_ = em.Emit(ctx, "cleanup", nil) // events after cancellation are still stored
		return nil, ctx.Err()
	})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(15 * time.Second):
		row, _ := e.q.GetRun(ctx, sqlcgen.GetRunParams{ID: run.Row.ID, UserID: user.ID})
		t.Fatalf("the workflow never started; run status %q error %q", row.Status, row.Error)
	}
	if !e.Cancel(run.Row.ID) {
		t.Fatal("cancel reported the run as not active")
	}
	waitCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := e.Wait(waitCtx, run.Row.ID); err != nil {
		t.Fatal(err)
	}
	row, err := e.q.GetRun(ctx, sqlcgen.GetRunParams{ID: run.Row.ID, UserID: user.ID})
	if err != nil {
		t.Fatal(err)
	}
	if row.Status != StatusCancelled {
		t.Fatalf("status %q, want cancelled (error %q)", row.Status, row.Error)
	}
	evs := collect(t, e.Subscribe(ctx, run.Row.ID, 0), 5*time.Second)
	types := map[string]bool{}
	for _, ev := range evs {
		types[ev.Type] = true
	}
	if !types["cleanup"] || !types[EventRunFinished] {
		t.Fatalf("events after cancel: %v", types)
	}
	if e.Cancel(run.Row.ID) {
		t.Fatal("cancel of a finished run reported active")
	}
}

func TestIntegrationEngineTimeoutAndFailure(t *testing.T) {
	e, user := newEngine(t, 200*time.Millisecond)
	ctx := context.Background()
	run, err := e.Launch(ctx, StartParams{User: user, Kind: KindCompare}, func(ctx context.Context, run *Run, em *Emitter) (any, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	})
	if err != nil {
		t.Fatal(err)
	}
	waitCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	_ = e.Wait(waitCtx, run.Row.ID)
	row, _ := e.q.GetRun(ctx, sqlcgen.GetRunParams{ID: run.Row.ID, UserID: user.ID})
	if row.Status != StatusFailed || row.Error != errRunTimeout.Error() {
		t.Fatalf("timed-out run: status %q error %q", row.Status, row.Error)
	}

	failing, err := e.Launch(ctx, StartParams{User: user, Kind: KindRevision}, func(ctx context.Context, run *Run, em *Emitter) (any, error) {
		return nil, errors.New("the lead writer refused")
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = e.Wait(waitCtx, failing.Row.ID)
	row, _ = e.q.GetRun(ctx, sqlcgen.GetRunParams{ID: failing.Row.ID, UserID: user.ID})
	if row.Status != StatusFailed || row.Error != "the lead writer refused" {
		t.Fatalf("failed run: status %q error %q", row.Status, row.Error)
	}

	panicking, err := e.Launch(ctx, StartParams{User: user, Kind: KindRevision}, func(ctx context.Context, run *Run, em *Emitter) (any, error) {
		panic("boom")
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = e.Wait(waitCtx, panicking.Row.ID)
	row, _ = e.q.GetRun(ctx, sqlcgen.GetRunParams{ID: panicking.Row.ID, UserID: user.ID})
	if row.Status != StatusFailed || row.Error != "the run crashed: boom" {
		t.Fatalf("panicking run: status %q error %q", row.Status, row.Error)
	}
}
