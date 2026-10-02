package text

import (
	"strings"
	"testing"
)

func TestEstimateTokens(t *testing.T) {
	if EstimateTokens("") != 0 {
		t.Fatal("empty text should cost nothing")
	}
	prose := strings.Repeat("The rain had stopped before she reached the gate. ", 20) // ~1000 chars, 200 words
	n := EstimateTokens(prose)
	if n < 230 || n > 300 {
		t.Fatalf("prose estimate %d, want roughly 250", n)
	}
	if EstimateTokens("a b c d e f g h") < 8 {
		t.Fatal("short words should count at least one token each")
	}
	if EstimateTokens("éàü") == 0 {
		t.Fatal("non-ASCII text should cost tokens")
	}
}

func TestSplitScenesFitsInOne(t *testing.T) {
	md := "# One\n\nShort chapter.\n\n---\n\nStill short."
	scenes := SplitScenes(md, 1000)
	if len(scenes) != 1 || scenes[0].Text != md || scenes[0].Start != 0 || scenes[0].End != len(md) {
		t.Fatalf("got %+v", scenes)
	}
	if got := SplitScenes(md, 0); len(got) != 1 {
		t.Fatalf("no limit should give one scene, got %d", len(got))
	}
}

func para(word string, n int) string {
	return strings.TrimSpace(strings.Repeat(word+" ", n))
}

func TestSplitScenesAtBreaksAndHeadings(t *testing.T) {
	a := para("alpha", 60)
	b := para("bravo", 60)
	c := para("charlie", 60)
	md := "# Chapter One\n\n" + a + "\n\n* * *\n\n" + b + "\n\n## Later that night\n\n" + c + "\n"
	limit := EstimateTokens(a) + 40 // each section fits alone, no two together
	scenes := SplitScenes(md, limit)
	if len(scenes) != 3 {
		t.Fatalf("want 3 scenes, got %d: %+v", len(scenes), titles(scenes))
	}
	if scenes[0].Title != "Chapter One" || scenes[1].Title != "Scene 2" || scenes[2].Title != "Later that night" {
		t.Fatalf("titles %v", titles(scenes))
	}
	checkCoverage(t, md, scenes)
	if !strings.Contains(scenes[1].Text, "bravo") || strings.Contains(scenes[1].Text, "alpha") {
		t.Fatalf("scene 2 text wrong: %q", scenes[1].Text)
	}
	if !strings.HasPrefix(scenes[1].Text, "* * *") {
		t.Fatalf("the break should open the next scene: %q", scenes[1].Text[:20])
	}
}

func TestSplitScenesMergesSmallNeighbours(t *testing.T) {
	parts := []string{para("one", 10), para("two", 10), para("three", 10), para("four", 10)}
	md := strings.Join(parts, "\n\n---\n\n")
	limit := EstimateTokens(parts[0]+"\n\n---\n\n"+parts[1]) + 5 // two sections fit, three do not
	scenes := SplitScenes(md, limit)
	if len(scenes) != 2 {
		t.Fatalf("want 2 merged scenes, got %d: %v", len(scenes), titles(scenes))
	}
	checkCoverage(t, md, scenes)
	for _, sc := range scenes {
		if EstimateTokens(sc.Text) > limit {
			t.Fatalf("scene over limit: %d > %d", EstimateTokens(sc.Text), limit)
		}
	}
}

func TestSplitScenesFallsBackToParagraphs(t *testing.T) {
	var paras []string
	for i := 0; i < 8; i++ {
		paras = append(paras, para("word", 30))
	}
	md := strings.Join(paras, "\n\n") // no breaks or headings at all
	limit := EstimateTokens(paras[0]+"\n\n"+paras[1]+"\n\n"+paras[2]) + 2
	scenes := SplitScenes(md, limit)
	if len(scenes) < 3 {
		t.Fatalf("want at least 3 scenes, got %d", len(scenes))
	}
	checkCoverage(t, md, scenes)
	for i, sc := range scenes {
		if EstimateTokens(sc.Text) > limit {
			t.Fatalf("scene %d over limit", i)
		}
		if strings.HasPrefix(sc.Text, "\n") {
			t.Fatalf("scene %d starts mid-break: %q", i, sc.Text[:5])
		}
	}
	// A single paragraph above the limit stays whole rather than being cut mid-sentence.
	huge := para("huge", 500)
	got := SplitScenes(huge, 50)
	if len(got) != 1 || got[0].Text != huge {
		t.Fatalf("oversized paragraph should stay whole, got %d scenes", len(got))
	}
}

func TestSplitScenesFoldsHeadingsAndBreaks(t *testing.T) {
	a := para("alpha", 200)
	b := para("bravo", 200)
	md := "# Title\n\n" + a + "\n\n* * *\n\n" + b + "\n"
	limit := EstimateTokens(a) + 5 // each paragraph only just fits; the heading and break must not become scenes
	scenes := SplitScenes(md, limit)
	if len(scenes) != 2 {
		t.Fatalf("want 2 scenes, got %d: %v", len(scenes), titles(scenes))
	}
	checkCoverage(t, md, scenes)
	if scenes[0].Title != "Title" || !strings.HasPrefix(scenes[1].Text, "* * *") {
		t.Fatalf("scenes %v / %q", titles(scenes), scenes[1].Text[:10])
	}
}

func TestIsBoundary(t *testing.T) {
	yes := []string{"---", "***", "* * *", "___", "- - -", "# Title", "###### Six", "  ---  "}
	no := []string{"", "--", "#hashtag", "####### seven", "-- not a rule", "* item", "text"}
	for _, l := range yes {
		if !isBoundary(l) {
			t.Errorf("%q should be a boundary", l)
		}
	}
	for _, l := range no {
		if isBoundary(l) {
			t.Errorf("%q should not be a boundary", l)
		}
	}
}

func TestFindQuote(t *testing.T) {
	text := "She said, “I won’t go back—not tonight.”\n\nHe *almost* believed her… and then the   lamp went out."
	cases := []struct {
		quote string
		want  string
		exact bool
	}{
		{"not tonight", "not tonight", true},
		{`"I won't go back-not tonight."`, "“I won’t go back—not tonight.”", false},
		{"He almost believed her...", "He *almost* believed her…", false},
		{"and then the lamp went out", "and then the   lamp went out", false},
		{"he almost believed her", "He *almost* believed her", false},
	}
	for _, c := range cases {
		m, ok := FindQuote(text, c.quote)
		if !ok {
			t.Errorf("quote %q not found", c.quote)
			continue
		}
		if got := text[m.Start:m.End]; got != c.want || m.Exact != c.exact {
			t.Errorf("quote %q -> %q (exact %v), want %q (exact %v)", c.quote, got, m.Exact, c.want, c.exact)
		}
	}
	for _, q := range []string{"", "ab", "the dog barked", "I will go back"} {
		if _, ok := FindQuote(text, q); ok {
			t.Errorf("quote %q should not match", q)
		}
	}
}

func TestFindQuoteAcrossMarkdownAndUnicode(t *testing.T) {
	text := "## The Café\n\n> “Où est-elle ?” demanded García.\n\nNobody answered."
	m, ok := FindQuote(text, `"Où est-elle ?" demanded García.`)
	if !ok || text[m.Start:m.End] != "“Où est-elle ?” demanded García." {
		t.Fatalf("unicode quote: ok=%v got %q", ok, text[m.Start:m.End])
	}
	m, ok = FindQuote(text, "The Café")
	if !ok || text[m.Start:m.End] != "The Café" || !m.Exact {
		t.Fatalf("heading quote: ok=%v got %q exact=%v", ok, text[m.Start:m.End], m.Exact)
	}
	if Normalise("# A *b* _c_ `d`  e\n> f") != `A b c d e f` {
		t.Fatalf("normalise: %q", Normalise("# A *b* _c_ `d`  e\n> f"))
	}
}

func titles(scenes []Scene) []string {
	out := make([]string, len(scenes))
	for i, s := range scenes {
		out[i] = s.Title
	}
	return out
}

// checkCoverage asserts the scenes tile the chapter in order without gaps.
func checkCoverage(t *testing.T, md string, scenes []Scene) {
	t.Helper()
	pos := 0
	for i, sc := range scenes {
		if sc.Start != pos || sc.Index != i || md[sc.Start:sc.End] != sc.Text {
			t.Fatalf("scene %d: start %d (want %d) text mismatch", i, sc.Start, pos)
		}
		pos = sc.End
	}
	if pos != len(md) {
		t.Fatalf("scenes end at %d, chapter is %d bytes", pos, len(md))
	}
}
