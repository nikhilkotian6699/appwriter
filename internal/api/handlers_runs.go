package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"writersguild/internal/db/sqlcgen"
	"writersguild/internal/runs"
)

var runKinds = map[string]bool{
	runs.KindCritique: true, runs.KindRevision: true, runs.KindBibleUpdate: true,
	runs.KindCowrite: true, runs.KindCompare: true, runs.KindWriterTest: true,
}

// sseHeartbeat is how often a comment line keeps an idle stream alive.
var sseHeartbeat = 15 * time.Second

// ListChapterRuns lists a chapter's runs, newest first, optionally by kind.
func (s *Server) ListChapterRuns(w http.ResponseWriter, r *http.Request, chapterId ChapterId, params ListChapterRunsParams) {
	u := currentUser(r.Context())
	if _, err := s.q.GetChapter(r.Context(), sqlcgen.GetChapterParams{ID: chapterId, UserID: u.ID}); err != nil {
		s.fail(w, err)
		return
	}
	kind := ""
	if params.Kind != nil {
		kind = string(*params.Kind)
		if !runKinds[kind] {
			s.fail(w, errBadRequest("unknown run kind %q", kind))
			return
		}
	}
	limit := 50
	if params.Limit != nil {
		limit = *params.Limit
	}
	if limit < 1 || limit > 200 {
		s.fail(w, errBadRequest("limit must be between 1 and 200"))
		return
	}
	rows, err := s.q.ListChapterRuns(r.Context(), sqlcgen.ListChapterRunsParams{
		ChapterID: nullUUID(&chapterId), UserID: u.ID, Kind: kind, RowLimit: int32(limit),
	})
	if err != nil {
		s.fail(w, err)
		return
	}
	out := make([]Run, 0, len(rows))
	for _, row := range rows {
		out = append(out, toRun(row))
	}
	writeJSON(w, http.StatusOK, out)
}

// GetRun returns one run of the account.
func (s *Server) GetRun(w http.ResponseWriter, r *http.Request, runId RunId) {
	u := currentUser(r.Context())
	row, err := s.q.GetRun(r.Context(), sqlcgen.GetRunParams{ID: runId, UserID: u.ID})
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toRun(row))
}

// CancelRun asks the engine to stop the run and waits briefly for it to be
// recorded, so the reply usually already says cancelled.
func (s *Server) CancelRun(w http.ResponseWriter, r *http.Request, runId RunId) {
	u := currentUser(r.Context())
	row, err := s.q.GetRun(r.Context(), sqlcgen.GetRunParams{ID: runId, UserID: u.ID})
	if err != nil {
		s.fail(w, err)
		return
	}
	if row.Status == runs.StatusRunning || row.Status == runs.StatusQueued {
		if s.engine.Cancel(row.ID) {
			waitCtx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
			defer cancel()
			_ = s.engine.Wait(waitCtx, row.ID)
		} else {
			// Not running in this process: a restart interrupted it.
			row, err = s.q.FinishRun(r.Context(), sqlcgen.FinishRunParams{ID: row.ID, Status: runs.StatusCancelled, Error: "cancelled"})
			if err != nil {
				s.fail(w, err)
				return
			}
		}
		row, err = s.q.GetRun(r.Context(), sqlcgen.GetRunParams{ID: runId, UserID: u.ID})
		if err != nil {
			s.fail(w, err)
			return
		}
	}
	writeJSON(w, http.StatusOK, toRun(row))
}

// StreamRunEvents replays and follows a run as Server-Sent Events.
func (s *Server) StreamRunEvents(w http.ResponseWriter, r *http.Request, runId RunId, params StreamRunEventsParams) {
	u := currentUser(r.Context())
	row, err := s.q.GetRun(r.Context(), sqlcgen.GetRunParams{ID: runId, UserID: u.ID})
	if err != nil {
		s.fail(w, err)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		s.fail(w, fmt.Errorf("streaming is not supported by this connection"))
		return
	}
	after := sseAfter(r.Header.Get("Last-Event-ID"), params.After)

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	events := s.engine.Subscribe(r.Context(), row.ID, after)
	ticker := time.NewTicker(sseHeartbeat)
	defer ticker.Stop()
	finished := false
	for !finished {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			if _, err := fmt.Fprint(w, ": ping\n\n"); err != nil {
				return
			}
			flusher.Flush()
		case ev, ok := <-events:
			if !ok {
				finished = true
				break
			}
			if err := writeSSE(w, ev); err != nil {
				return
			}
			flusher.Flush()
			if ev.Type == runs.EventRunFinished {
				return
			}
		}
	}
	// The run was already over (or not executing here): tell the browser to stop.
	fmt.Fprint(w, "event: end\ndata: {}\n\n")
	flusher.Flush()
}

// sseAfter picks the replay point: the Last-Event-ID header wins over ?after.
func sseAfter(lastEventID string, after *int) int {
	if v := strings.TrimSpace(lastEventID); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			return n
		}
	}
	if after != nil && *after > 0 {
		return *after
	}
	return 0
}

// writeSSE formats one event. Payload lines never contain raw newlines, since
// JSON escapes them, so a single data line is enough.
func writeSSE(w http.ResponseWriter, ev runs.Event) error {
	data := ev.Payload
	if len(data) == 0 {
		data = json.RawMessage("{}")
	}
	_, err := fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", ev.Seq, ev.Type, data)
	return err
}
