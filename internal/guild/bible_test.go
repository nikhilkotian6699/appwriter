package guild

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"

	"writersguild/internal/db/sqlcgen"
)

func sampleBible() (map[uuid.UUID]sqlcgen.BibleEntry, sqlcgen.BibleEntry, sqlcgen.BibleEntry) {
	mara := sqlcgen.BibleEntry{ID: uuid.New(), Section: "character", Title: "Mara", Fields: []byte(`{"role":"harbour master","voice":"clipped"}`)}
	gate := sqlcgen.BibleEntry{ID: uuid.New(), Section: "setting", Title: "The gate", Fields: []byte(`{"text":"Iron, always locked at dusk."}`)}
	return map[uuid.UUID]sqlcgen.BibleEntry{mara.ID: mara, gate.ID: gate}, mara, gate
}

func TestParseProposals(t *testing.T) {
	entries, mara, gate := sampleBible()
	reply := "```json\n" + `{"proposals": [
	  {"action": "UPDATE", "entry_id": "` + mara.ID.String() + `", "section": "character", "title": "", "fields": {"arc": "learns to wait"}, "rationale": "She waits for the boat all night."},
	  {"action": "add", "section": "character", "title": "The porter", "fields": {"role": "porter", "notes": 3}, "rationale": "He leaves before morning."},
	  {"action": "delete", "entry_id": "The gate", "section": "setting", "rationale": "The gate stands open now and is never locked again."},
	  {"action": "update", "entry_id": "00000000-0000-0000-0000-000000000000", "section": "setting", "fields": {"text": "x"}, "rationale": "r"}
	]}` + "\n```"
	set, err := ParseProposals(reply, entries)
	if err != nil {
		t.Fatal(err)
	}
	if len(set.Proposals) != 3 || len(set.Warnings) != 1 || !strings.Contains(set.Warnings[0], "does not exist") {
		t.Fatalf("set %+v", set)
	}
	up := set.Proposals[0]
	if up.Action != "update" || up.EntryID == nil || *up.EntryID != mara.ID || up.Title != "Mara" || up.Section != "character" {
		t.Fatalf("update %+v", up)
	}
	if up.Fields["role"] != "harbour master" || up.Fields["voice"] != "clipped" || up.Fields["arc"] != "learns to wait" {
		t.Fatalf("update fields should be merged over the entry: %v", up.Fields)
	}
	add := set.Proposals[1]
	if add.Action != "add" || add.EntryID != nil || add.Title != "The porter" || add.Fields["notes"] != "3" {
		t.Fatalf("add %+v", add)
	}
	del := set.Proposals[2]
	if del.Action != "delete" || del.EntryID == nil || *del.EntryID != gate.ID || del.Title != "The gate" || del.Fields["text"] == "" {
		t.Fatalf("delete resolved by title %+v", del)
	}
	// An empty list is fine.
	set, err = ParseProposals(`{"proposals": []}`, entries)
	if err != nil || len(set.Proposals) != 0 {
		t.Fatalf("empty: %v %+v", err, set)
	}
}

func TestParseProposalsRejects(t *testing.T) {
	entries, mara, _ := sampleBible()
	cases := map[string]string{
		"no json":      "Nothing.",
		"bad action":   `{"proposals": [{"action": "rename", "entry_id": "` + mara.ID.String() + `", "rationale": "r"}]}`,
		"bad section":  `{"proposals": [{"action": "add", "section": "villains", "title": "X", "rationale": "r"}]}`,
		"no rationale": `{"proposals": [{"action": "add", "section": "character", "title": "X", "rationale": ""}]}`,
		"empty add":    `{"proposals": [{"action": "add", "section": "character", "title": "", "fields": {}, "rationale": "r"}]}`,
	}
	for name, reply := range cases {
		_, err := ParseProposals(reply, entries)
		var verr *ValidationError
		if !errors.As(err, &verr) {
			t.Errorf("%s: want ValidationError, got %v", name, err)
		}
	}
}

func TestBibleKeeperPrompt(t *testing.T) {
	_, mara, gate := sampleBible()
	hunks, _ := json.Marshal([]map[string]any{
		{"index": 0, "old_start": 0, "old_end": 3, "old_text": "cat", "new_text": "dog", "ops": []any{}, "context_before": "The ", "context_after": " sat."},
		{"index": 1, "old_start": 10, "old_end": 13, "old_text": "red", "new_text": "blue", "ops": []any{}, "context_before": "", "context_after": ""},
	})
	rev := sqlcgen.Revision{ID: uuid.New(), Hunks: hunks, AppliedHunks: []byte(`[0]`)}
	in := BibleUpdateInput{Project: sqlcgen.Project{Name: "P"}, Chapter: sqlcgen.Chapter{Title: "C", ContentMd: "The dog sat."}, Bible: []sqlcgen.BibleEntry{mara, gate}, Revision: &rev, SceneTokenLimit: 6000}
	p := BibleKeeperUserPrompt(in)
	for _, want := range []string{"[" + mara.ID.String() + "] **Mara**", "[" + gate.ID.String() + "] **The gate**", ChapterBegin, "The dog sat.", "# What the revision changed", `1. Before: "The cat sat."`, `After: "The dog sat."`} {
		if !strings.Contains(p, want) {
			t.Errorf("prompt lacks %q", want)
		}
	}
	if strings.Contains(p, "red") {
		t.Error("a hunk that was not applied should not be listed")
	}
	if !strings.Contains(BibleKeeperSystemPrompt("You keep the bible."), FixedSuffix) {
		t.Error("system prompt lacks the fixed suffix")
	}
}
