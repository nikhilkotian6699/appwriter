package api

import (
	"encoding/json"
	"net/http/httptest"
	"testing"

	"writersguild/internal/runs"
)

func TestSSEAfter(t *testing.T) {
	five := 5
	cases := []struct {
		header string
		after  *int
		want   int
	}{
		{"", nil, 0},
		{"", &five, 5},
		{"12", &five, 12},
		{" 7 ", nil, 7},
		{"nonsense", &five, 5},
		{"-3", nil, 0},
	}
	for _, c := range cases {
		if got := sseAfter(c.header, c.after); got != c.want {
			t.Errorf("sseAfter(%q, %v) = %d, want %d", c.header, c.after, got, c.want)
		}
	}
}

func TestWriteSSE(t *testing.T) {
	rec := httptest.NewRecorder()
	ev := runs.Event{Seq: 3, Type: "writer.delta", Payload: json.RawMessage(`{"text":"line one\nline two"}`)}
	if err := writeSSE(rec, ev); err != nil {
		t.Fatal(err)
	}
	want := "id: 3\nevent: writer.delta\ndata: {\"text\":\"line one\\nline two\"}\n\n"
	if rec.Body.String() != want {
		t.Fatalf("got %q, want %q", rec.Body.String(), want)
	}
	rec = httptest.NewRecorder()
	_ = writeSSE(rec, runs.Event{Seq: 1, Type: "run.started"})
	if rec.Body.String() != "id: 1\nevent: run.started\ndata: {}\n\n" {
		t.Fatalf("empty payload: %q", rec.Body.String())
	}
}
