package main

import (
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var mustCompile = regexp.MustCompile

func TestRenderMarkdownFiguresAndHeadings(t *testing.T) {
	src := "# Signing in\n\nText.\n\n```figure\nlogin\n```\n\n## Step two\n\nMore.\n\n```figure\nmissing-one\n```\n"
	figures := map[string]Figure{
		"login":       {Title: "The sign-in page", Marks: []Mark{{Letter: "A", Label: "Username"}, {Letter: "B", Label: "Sign in"}}},
		"missing-one": {Title: "Not yet shot"},
	}
	out, err := RenderMarkdown(src, figures, func(name string) bool { return name == "login" })
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`<h2 id="signing-in">Signing in</h2>`, `<h3 id="step-two">Step two</h3>`,
		`<figure class="shot"><img src="img/login.png" alt="The sign-in page"`, `<li><b>A</b> Username</li>`, `<li><b>B</b> Sign in</li>`,
		`<figure class="shot missing"><div class="placeholder">Screenshot: Not yet shot</div>`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q\n%s", want, out)
		}
	}
	if strings.Contains(out, "FIGURE-TOKEN") || strings.Contains(out, "<h1") {
		t.Fatalf("tokens or h1 left in output:\n%s", out)
	}
	toc := TableOfContents(out)
	if len(toc) != 2 || toc[0].ID != "signing-in" || toc[0].Level != 2 || toc[1].ID != "step-two" || toc[1].Level != 3 || toc[1].Title != "Step two" {
		t.Fatalf("toc %+v", toc)
	}
}

func TestAnchorsUsedInTheTextExist(t *testing.T) {
	// The chapters link to each other by heading id; every target must exist.
	files, _ := filepath.Glob("../src/*.md")
	if len(files) == 0 {
		t.Skip("guide sources not found")
	}
	var md strings.Builder
	for _, f := range files {
		b, _ := os.ReadFile(f)
		md.Write(b)
		md.WriteString("\n\n")
	}
	out, err := RenderMarkdown(md.String(), map[string]Figure{}, func(string) bool { return false })
	if err != nil {
		t.Fatal(err)
	}
	ids := map[string]bool{}
	for _, e := range TableOfContents(out) {
		ids[e.ID] = true
	}
	for _, m := range regexpAnchors.FindAllStringSubmatch(out, -1) {
		if !ids[m[1]] {
			t.Errorf("link to #%s has no heading", m[1])
		}
	}
	if len(ids) < 18 {
		t.Fatalf("expected at least 18 headings, got %d", len(ids))
	}
}

func TestPageInlineAndExternalCheck(t *testing.T) {
	body := `<h2 id="a">A</h2><figure class="shot"><img src="img/login.png" alt="x"></figure>`
	page := Page(body, TableOfContents(body))
	if !strings.Contains(page, `<a class="l2" href="#a">A</a>`) || !strings.Contains(page, "<!doctype html>") {
		t.Fatal("page lacks nav or doctype")
	}
	if err := CheckNoExternalRequests(page); err != nil {
		t.Fatalf("clean page flagged: %v", err)
	}
	if err := CheckNoExternalRequests(page + `<script src="https://cdn.example.com/x.js"></script>`); err == nil {
		t.Fatal("external script not caught")
	}
	if err := CheckNoExternalRequests(page + `<link href="//fonts.example.com/css">`); err == nil {
		t.Fatal("protocol-relative link not caught")
	}
	single := Inline(page, map[string][]byte{"img/login.png": []byte{0x89, 'P', 'N', 'G'}})
	if strings.Contains(single, `src="img/login.png"`) || !strings.Contains(single, `src="data:image/png;base64,`) {
		t.Fatal("image not inlined")
	}
}

func TestRenderFigureDrawsMarks(t *testing.T) {
	dir := t.TempDir()
	img := image.NewRGBA(image.Rect(0, 0, 120, 80))
	for y := 0; y < 80; y++ {
		for x := 0; x < 120; x++ {
			img.Set(x, y, color.White)
		}
	}
	path := filepath.Join(dir, "shot.png")
	f, _ := os.Create(path)
	_ = png.Encode(f, img)
	f.Close()
	x, y := 30, 40
	data, ok, err := renderFigure(path, Figure{Marks: []Mark{{Letter: "A", Label: "here", X: &x, Y: &y}, {Letter: "B", Label: "legend only"}}})
	if err != nil || !ok {
		t.Fatalf("render: ok=%v err=%v", ok, err)
	}
	decoded, err := png.Decode(strings.NewReader(string(data)))
	if err != nil {
		t.Fatal(err)
	}
	isAmber := func(px, py int) bool {
		r, g, b, _ := decoded.At(px, py).RGBA()
		return r>>8 >= 0xE0 && g>>8 >= 0x80 && b>>8 <= 0x40
	}
	if !isAmber(30+19, 40) { // on the ring
		t.Fatal("expected an amber ring around the mark")
	}
	if !isAmber(30+26, 40-26-5) { // inside the badge, beside the letter
		t.Fatal("expected an amber badge at the ring's upper right")
	}
	if r, _, _, _ := decoded.At(30, 40).RGBA(); r>>8 != 0xFF {
		t.Fatal("the centre of the ring must stay untouched so the element shows")
	}
	if r, _, _, _ := decoded.At(110, 70).RGBA(); r>>8 != 0xFF {
		t.Fatal("the rest of the image must stay untouched")
	}
	if _, ok, err := renderFigure(filepath.Join(dir, "nope.png"), Figure{}); ok || err != nil {
		t.Fatalf("missing screenshot: ok=%v err=%v", ok, err)
	}
}

func TestBuildEndToEnd(t *testing.T) {
	if _, err := os.Stat("../src/00-welcome.md"); err != nil {
		t.Skip("guide sources not found")
	}
	out := t.TempDir()
	if err := Build("../src", out); err != nil {
		t.Fatal(err)
	}
	page, err := os.ReadFile(filepath.Join(out, "index.html"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"Welcome to the Writers' Guild", "A sample chapter to paste", "Glossary", `href="#troubleshooting"`} {
		if !strings.Contains(string(page), want) {
			t.Errorf("index lacks %q", want)
		}
	}
	if _, err := os.Stat(filepath.Join(out, "writers-guild-guide.html")); err != nil {
		t.Fatal("single-file guide missing")
	}
}

var regexpAnchors = mustCompile(`href="#([^"]+)"`)
