package text

import (
	"strings"
	"testing"
)

func applyAll(t *testing.T, old string, hunks []Hunk) string {
	t.Helper()
	out, err := ApplyHunks(old, hunks, nil)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestDiffIdentical(t *testing.T) {
	if h := Diff("same text", "same text"); len(h) != 0 {
		t.Fatalf("identical texts gave %d hunks", len(h))
	}
}

func TestDiffWordChange(t *testing.T) {
	old := "The cat sat on the mat. It purred."
	new := "The dog sat on the mat. It purred."
	hunks := Diff(old, new)
	if len(hunks) != 1 {
		t.Fatalf("want 1 hunk, got %d: %+v", len(hunks), hunks)
	}
	h := hunks[0]
	if h.OldText != "cat" || h.NewText != "dog" || old[h.OldStart:h.OldEnd] != "cat" {
		t.Fatalf("hunk %+v", h)
	}
	if len(h.Ops) != 2 || h.Ops[0].Kind != OpDelete || h.Ops[0].Text != "cat" || h.Ops[1].Kind != OpInsert || h.Ops[1].Text != "dog" {
		t.Fatalf("ops %+v", h.Ops)
	}
	if h.ContextBefore != "The " || !strings.HasPrefix(h.ContextAfter, " sat on the mat.") {
		t.Fatalf("context %q / %q", h.ContextBefore, h.ContextAfter)
	}
	if applyAll(t, old, hunks) != new {
		t.Fatal("applying every hunk should give the new text")
	}
	st := Stats(hunks)
	if st.WordsAdded != 1 || st.WordsRemoved != 1 || st.Hunks != 1 {
		t.Fatalf("stats %+v", st)
	}
}

func TestDiffInsertAndDelete(t *testing.T) {
	old := "One. Two. Three."
	ins := "One. Two. And a half. Three."
	hunks := Diff(old, ins)
	if len(hunks) != 1 || hunks[0].OldText != "" || hunks[0].NewText != "And a half. " {
		t.Fatalf("insert hunks %+v", hunks)
	}
	if applyAll(t, old, hunks) != ins {
		t.Fatal("insert round trip")
	}
	del := "One. Three."
	hunks = Diff(old, del)
	if len(hunks) != 1 || hunks[0].NewText != "" || hunks[0].OldText != "Two. " {
		t.Fatalf("delete hunks %+v", hunks)
	}
	if applyAll(t, old, hunks) != del {
		t.Fatal("delete round trip")
	}
	if Stats(hunks).WordsRemoved != 1 {
		t.Fatalf("stats %+v", Stats(hunks))
	}
}

func TestDiffGroupsNearbyChanges(t *testing.T) {
	old := "alpha bravo charlie delta echo foxtrot golf hotel india"
	near := "alpha BRAVO charlie DELTA echo foxtrot golf hotel india"
	hunks := Diff(old, near)
	if len(hunks) != 1 {
		t.Fatalf("changes one word apart should form one hunk, got %d", len(hunks))
	}
	if hunks[0].OldText != "bravo charlie delta" || hunks[0].NewText != "BRAVO charlie DELTA" {
		t.Fatalf("hunk %+v", hunks[0])
	}
	far := "alpha BRAVO charlie delta echo foxtrot golf hotel INDIA"
	hunks = Diff(old, far)
	if len(hunks) != 2 || hunks[0].Index != 0 || hunks[1].Index != 1 {
		t.Fatalf("changes far apart should form two hunks, got %d", len(hunks))
	}
	if applyAll(t, old, hunks) != far {
		t.Fatal("round trip")
	}
}

func TestApplySubsetAndNone(t *testing.T) {
	old := "She opened the door. The room was cold and dark. Nobody spoke. She left."
	new := "She pushed the door open. The room was cold. Nobody spoke. She left at once."
	hunks := Diff(old, new)
	if len(hunks) != 3 {
		t.Fatalf("want 3 hunks, got %d: %+v", len(hunks), hunks)
	}
	none, err := ApplyHunks(old, hunks, func(Hunk) bool { return false })
	if err != nil || none != old {
		t.Fatalf("accepting nothing should give the old text: %q", none)
	}
	if applyAll(t, old, hunks) != new {
		t.Fatal("accepting all should give the new text")
	}
	second, err := ApplyHunks(old, hunks, func(h Hunk) bool { return h.Index == 1 })
	if err != nil {
		t.Fatal(err)
	}
	want := "She opened the door. The room was cold. Nobody spoke. She left."
	if second != want {
		t.Fatalf("accepting the middle hunk gave %q", second)
	}
	// Order of the given hunks does not matter.
	reversed := []Hunk{hunks[2], hunks[0], hunks[1]}
	if applyAll(t, old, reversed) != new {
		t.Fatal("hunk order should not matter")
	}
}

func TestApplyStale(t *testing.T) {
	old := "The cat sat."
	hunks := Diff(old, "The dog sat.")
	if _, err := ApplyHunks("The bat sat.", hunks, nil); err == nil {
		t.Fatal("a changed text should be rejected")
	}
	if _, err := ApplyHunks("", hunks, nil); err == nil {
		t.Fatal("an out-of-range hunk should be rejected")
	}
}

func TestDiffRoundTrips(t *testing.T) {
	cases := [][2]string{
		{"", "Something from nothing."},
		{"Everything goes.", ""},
		{"“Où est-elle ?” demanded García.\n\nNobody answered.", "“Où est-il ?” demanded García.\n\nNobody answered him."},
		{"Line one.\nLine two.\n\nLine four.", "Line one.\nLine two.\nLine three.\n\nLine four."},
		{"double  space  here", "double space here"},
		{"don't stop", "don’t stop now"},
		{strings.Repeat("word ", 300) + "end", strings.Repeat("word ", 150) + "middle " + strings.Repeat("word ", 150) + "end"},
		{"a completely different text about rain", "nothing in common whatsoever here"},
	}
	for i, c := range cases {
		hunks := Diff(c[0], c[1])
		if got := applyAll(t, c[0], hunks); got != c[1] {
			t.Errorf("case %d: apply all gave %q, want %q", i, got, c[1])
		}
		none, _ := ApplyHunks(c[0], hunks, func(Hunk) bool { return false })
		if none != c[0] {
			t.Errorf("case %d: apply none gave %q", i, none)
		}
		for _, h := range hunks {
			var oldSide, newSide strings.Builder
			for _, op := range h.Ops {
				if op.Kind != OpInsert {
					oldSide.WriteString(op.Text)
				}
				if op.Kind != OpDelete {
					newSide.WriteString(op.Text)
				}
			}
			if oldSide.String() != h.OldText || newSide.String() != h.NewText {
				t.Errorf("case %d: hunk ops do not rebuild its texts: %+v", i, h)
			}
		}
	}
}

func TestTokenize(t *testing.T) {
	toks := tokenize("Don't stop—now,  “please”.")
	want := []string{"Don't", " ", "stop", "—", "now", ",", "  ", "“", "please", "”", "."}
	if strings.Join(toks, "|") != strings.Join(want, "|") {
		t.Fatalf("tokens %q", toks)
	}
	if strings.Join(toks, "") != "Don't stop—now,  “please”." {
		t.Fatal("tokens must concatenate back to the text")
	}
}

func TestDiffLargeRewriteStaysBounded(t *testing.T) {
	var a, b strings.Builder
	for i := 0; i < 4000; i++ {
		a.WriteString("alpha ")
		b.WriteString("beta ")
	}
	hunks := Diff(a.String(), b.String())
	if applyAll(t, a.String(), hunks) != b.String() {
		t.Fatal("large rewrite round trip")
	}
}
