// Command build turns the guide's Markdown chapters, screenshots and marks
// into the static site served under /guide/ and into one self-contained
// HTML file to pass on. It makes no requests to other servers and refuses
// to emit a page that would.
//
//	go run ./guide/build            # guide/src -> guide/dist
//	go run ./guide/build -src S -out O
package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"html"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	ghtml "github.com/yuin/goldmark/renderer/html"
	xdraw "golang.org/x/image/draw"
	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"
)

func main() {
	src := flag.String("src", "guide/src", "guide sources: NN-*.md chapters, marks.json, img/")
	out := flag.String("out", "guide/dist", "output directory")
	flag.Parse()
	if err := Build(*src, *out); err != nil {
		fmt.Fprintln(os.Stderr, "guide build:", err)
		os.Exit(1)
	}
}

// Mark is one lettered marker on a screenshot. X and Y are pixel
// coordinates in the source image; without them the letter appears only in
// the legend.
type Mark struct {
	Letter string `json:"letter"`
	Label  string `json:"label"`
	X      *int   `json:"x,omitempty"`
	Y      *int   `json:"y,omitempty"`
}

// Figure describes one screenshot: its caption and its marks.
type Figure struct {
	Title string `json:"title"`
	Marks []Mark `json:"marks"`
}

// Build renders the guide from src into out.
func Build(src, out string) error {
	chapters, err := filepath.Glob(filepath.Join(src, "*.md"))
	if err != nil {
		return err
	}
	sort.Strings(chapters)
	if len(chapters) == 0 {
		return fmt.Errorf("no chapters in %s", src)
	}
	var md strings.Builder
	for _, path := range chapters {
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		md.Write(b)
		md.WriteString("\n\n")
	}
	figures := map[string]Figure{}
	if b, err := os.ReadFile(filepath.Join(src, "marks.json")); err == nil {
		if err := json.Unmarshal(b, &figures); err != nil {
			return fmt.Errorf("marks.json: %w", err)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.MkdirAll(filepath.Join(out, "img"), 0o755); err != nil {
		return err
	}
	// Render figures first so the page can refer to the annotated images.
	images := map[string][]byte{} // dist-relative path -> PNG bytes
	for name, fig := range figures {
		data, ok, err := renderFigure(filepath.Join(src, "img", name+".png"), fig)
		if err != nil {
			return fmt.Errorf("figure %s: %w", name, err)
		}
		if ok {
			rel := "img/" + name + ".png"
			images[rel] = data
			if err := os.WriteFile(filepath.Join(out, rel), data, 0o644); err != nil {
				return err
			}
		}
	}
	body, err := RenderMarkdown(md.String(), figures, func(name string) bool { _, ok := images["img/"+name+".png"]; return ok })
	if err != nil {
		return err
	}
	toc := TableOfContents(body)
	page := Page(body, toc)
	if err := CheckNoExternalRequests(page); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(out, "index.html"), []byte(page), 0o644); err != nil {
		return err
	}
	single := Inline(page, images)
	if err := os.WriteFile(filepath.Join(out, "writers-guild-guide.html"), []byte(single), 0o644); err != nil {
		return err
	}
	fmt.Printf("guide: %d chapters, %d figures (%d with screenshots) -> %s\n", len(chapters), len(figures), len(images), out)
	return nil
}

var figureFence = regexp.MustCompile("(?m)^```figure\\n([a-z0-9-]+)\\n```$")

// RenderMarkdown converts the chapters to HTML. Chapter titles become h2
// (the page's h1 is the guide's name), figure fences become figures with a
// legend, and a figure whose screenshot is missing gets a labelled
// placeholder so the text still reads.
func RenderMarkdown(src string, figures map[string]Figure, hasImage func(name string) bool) (string, error) {
	// Figures are swapped for tokens that survive Markdown untouched.
	src = figureFence.ReplaceAllString(src, "\n\nFIGURE-TOKEN-$1\n\n")
	gm := goldmark.New(
		goldmark.WithExtensions(extension.Table),
		goldmark.WithParserOptions(parser.WithAutoHeadingID()),
		goldmark.WithRendererOptions(ghtml.WithUnsafe()),
	)
	var buf bytes.Buffer
	if err := gm.Convert([]byte(src), &buf); err != nil {
		return "", err
	}
	out := buf.String()
	// Shift headings down one level: chapter titles are h2 under the guide's h1.
	for level := 5; level >= 1; level-- {
		out = strings.ReplaceAll(out, fmt.Sprintf("<h%d ", level), fmt.Sprintf("<h%d ", level+1))
		out = strings.ReplaceAll(out, fmt.Sprintf("<h%d>", level), fmt.Sprintf("<h%d>", level+1))
		out = strings.ReplaceAll(out, fmt.Sprintf("</h%d>", level), fmt.Sprintf("</h%d>", level+1))
	}
	tokenRe := regexp.MustCompile(`<p>FIGURE-TOKEN-([a-z0-9-]+)</p>`)
	out = tokenRe.ReplaceAllStringFunc(out, func(m string) string {
		name := tokenRe.FindStringSubmatch(m)[1]
		return figureHTML(name, figures[name], hasImage(name))
	})
	return out, nil
}

func figureHTML(name string, fig Figure, has bool) string {
	title := fig.Title
	if title == "" {
		title = strings.ReplaceAll(name, "-", " ")
	}
	var b strings.Builder
	if has {
		fmt.Fprintf(&b, `<figure class="shot"><img src="img/%s.png" alt="%s" loading="lazy">`, name, html.EscapeString(title))
	} else {
		fmt.Fprintf(&b, `<figure class="shot missing"><div class="placeholder">Screenshot: %s</div>`, html.EscapeString(title))
	}
	fmt.Fprintf(&b, `<figcaption><span class="cap">%s</span>`, html.EscapeString(title))
	if len(fig.Marks) > 0 {
		b.WriteString(`<ol class="legend">`)
		for _, m := range fig.Marks {
			fmt.Fprintf(&b, `<li><b>%s</b> %s</li>`, html.EscapeString(m.Letter), html.EscapeString(m.Label))
		}
		b.WriteString(`</ol>`)
	}
	b.WriteString(`</figcaption></figure>`)
	return b.String()
}

// TOCEntry is one line of the table of contents.
type TOCEntry struct {
	Level int
	ID    string
	Title string
}

var headingRe = regexp.MustCompile(`<h([23]) id="([^"]+)">(.*?)</h[23]>`)
var tagRe = regexp.MustCompile(`<[^>]+>`)

// TableOfContents lists the chapter titles (h2) and their sections (h3).
func TableOfContents(body string) []TOCEntry {
	var out []TOCEntry
	for _, m := range headingRe.FindAllStringSubmatch(body, -1) {
		level := 2
		if m[1] == "3" {
			level = 3
		}
		out = append(out, TOCEntry{Level: level, ID: m[2], Title: html.UnescapeString(tagRe.ReplaceAllString(m[3], ""))})
	}
	return out
}

// Page wraps the body in the full document: inline styles only, a sidebar
// table of contents, nothing fetched from anywhere.
func Page(body string, toc []TOCEntry) string {
	var nav strings.Builder
	for _, e := range toc {
		cls := "l2"
		if e.Level == 3 {
			cls = "l3"
		}
		fmt.Fprintf(&nav, `<a class="%s" href="#%s">%s</a>`, cls, e.ID, html.EscapeString(e.Title))
	}
	return strings.Replace(strings.Replace(pageTemplate, "{{NAV}}", nav.String(), 1), "{{BODY}}", body, 1)
}

// Inline embeds every local image as a data URI, for the single-file copy.
func Inline(page string, images map[string][]byte) string {
	for rel, data := range images {
		uri := "data:image/png;base64," + base64.StdEncoding.EncodeToString(data)
		page = strings.ReplaceAll(page, `src="`+rel+`"`, `src="`+uri+`"`)
	}
	return page
}

var externalRe = regexp.MustCompile(`(?i)(src|href|url\()\s*=?\s*["']?\s*(https?:)?//`)

// CheckNoExternalRequests fails when the page would load anything from
// another server.
func CheckNoExternalRequests(page string) error {
	if m := externalRe.FindString(page); m != "" {
		return fmt.Errorf("the guide must not request other servers; found %q", m)
	}
	return nil
}

// renderFigure loads a screenshot and draws its marks. It reports false
// when the screenshot does not exist yet.
func renderFigure(path string, fig Figure) ([]byte, bool, error) {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	if err != nil {
		return nil, false, err
	}
	canvas := image.NewRGBA(img.Bounds())
	draw.Draw(canvas, canvas.Bounds(), img, img.Bounds().Min, draw.Src)
	for _, m := range fig.Marks {
		if m.X == nil || m.Y == nil {
			continue
		}
		drawMark(canvas, *m.X, *m.Y, m.Letter)
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, canvas); err != nil {
		return nil, false, err
	}
	return buf.Bytes(), true, nil
}

// drawMark highlights the target with an amber ring, so the element stays
// readable, and hangs a small lettered badge off the ring's upper right,
// like a callout on a map.
func drawMark(dst *image.RGBA, x, y int, letter string) {
	amber := color.RGBA{R: 0xF5, G: 0x9E, B: 0x0B, A: 0xFF}
	ink := color.RGBA{R: 0x1C, G: 0x19, B: 0x17, A: 0xFF}
	const ringR, ringW = 20, 3
	for dy := -ringR - 1; dy <= ringR+1; dy++ {
		for dx := -ringR - 1; dx <= ringR+1; dx++ {
			d := dx*dx + dy*dy
			px, py := x+dx, y+dy
			if !(image.Point{px, py}).In(dst.Bounds()) {
				continue
			}
			switch {
			case d >= (ringR-ringW)*(ringR-ringW) && d <= ringR*ringR:
				dst.SetRGBA(px, py, amber)
			case d > ringR*ringR && d <= (ringR+1)*(ringR+1):
				dst.SetRGBA(px, py, ink)
			case d >= (ringR-ringW-1)*(ringR-ringW-1) && d < (ringR-ringW)*(ringR-ringW):
				dst.SetRGBA(px, py, ink)
			}
		}
	}
	// The badge: a filled disc at the ring's upper right with the letter.
	bx, by := x+ringR+6, y-ringR-6
	const badgeR = 13
	for dy := -badgeR - 2; dy <= badgeR+2; dy++ {
		for dx := -badgeR - 2; dx <= badgeR+2; dx++ {
			d := dx*dx + dy*dy
			px, py := bx+dx, by+dy
			if !(image.Point{px, py}).In(dst.Bounds()) {
				continue
			}
			switch {
			case d <= badgeR*badgeR:
				dst.SetRGBA(px, py, amber)
			case d <= (badgeR+2)*(badgeR+2):
				dst.SetRGBA(px, py, ink)
			}
		}
	}
	// Render the letter small, then scale it up so it stays crisp enough.
	face := basicfont.Face7x13
	glyph := image.NewRGBA(image.Rect(0, 0, 7, 13))
	d := &font.Drawer{Dst: glyph, Src: image.NewUniform(ink), Face: face, Dot: fixed.P(0, 10)}
	d.DrawString(letter)
	const scale = 2
	big := image.NewRGBA(image.Rect(0, 0, 7*scale, 13*scale))
	xdraw.NearestNeighbor.Scale(big, big.Bounds(), glyph, glyph.Bounds(), draw.Over, nil)
	target := image.Rect(bx-7*scale/2, by-13*scale/2, bx-7*scale/2+7*scale, by-13*scale/2+13*scale)
	draw.Draw(dst, target, big, image.Point{}, draw.Over)
}

const pageTemplate = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Writers' Guild: the guide</title>
<style>
  :root { --ink: #1c1917; --muted: #57534e; --line: #e7e5e4; --paper: #fafaf9; --amber: #f59e0b; }
  * { box-sizing: border-box; }
  html { scroll-behavior: smooth; }
  body { margin: 0; font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, Helvetica, Arial, sans-serif; color: var(--ink); background: var(--paper); line-height: 1.6; }
  .wrap { display: grid; grid-template-columns: 16rem 1fr; min-height: 100vh; }
  nav { border-right: 1px solid var(--line); background: #fff; padding: 1.25rem 1rem; position: sticky; top: 0; height: 100vh; overflow-y: auto; }
  nav .brand { display: flex; align-items: center; gap: .5rem; font-weight: 600; margin-bottom: 1rem; }
  nav .brand span { display: inline-flex; width: 1.75rem; height: 1.75rem; align-items: center; justify-content: center; border-radius: .375rem; background: var(--ink); color: var(--amber); font-family: Georgia, serif; }
  nav a { display: block; text-decoration: none; color: var(--muted); font-size: .875rem; padding: .15rem 0; }
  nav a.l2 { color: var(--ink); font-weight: 500; margin-top: .5rem; }
  nav a.l3 { padding-left: .9rem; }
  nav a:hover { text-decoration: underline; }
  main { max-width: 46rem; padding: 2rem 2.5rem 6rem; }
  main > h1 { font-size: 2rem; margin: 0 0 .25rem; }
  main > p.lead { color: var(--muted); margin-top: 0; }
  h2 { font-size: 1.5rem; margin: 3rem 0 .75rem; padding-top: 1.5rem; border-top: 1px solid var(--line); }
  h3 { font-size: 1.125rem; margin: 1.75rem 0 .5rem; }
  p, li { font-size: 1rem; }
  a { color: #1d4ed8; }
  code { font-family: ui-monospace, SFMono-Regular, Menlo, monospace; font-size: .9em; background: #f5f5f4; padding: 0 .25em; border-radius: 3px; }
  table { border-collapse: collapse; width: 100%; margin: 1rem 0; font-size: .95rem; }
  th, td { text-align: left; vertical-align: top; padding: .5rem .6rem; border-bottom: 1px solid var(--line); }
  th { font-size: .75rem; text-transform: uppercase; letter-spacing: .04em; color: var(--muted); }
  blockquote { margin: 1.25rem 0; padding: .75rem 1rem; border-left: 3px solid var(--amber); background: #fff; }
  blockquote > p:first-child > strong:only-child { display: block; font-size: 1.25rem; font-family: Georgia, serif; margin-bottom: .25rem; }
  blockquote p { font-family: Georgia, "Iowan Old Style", serif; font-size: 1.05rem; }
  blockquote hr { border: 0; text-align: center; }
  blockquote hr::before { content: "* * *"; color: #a8a29e; letter-spacing: .5em; }
  figure.shot { margin: 1.5rem 0; padding: 1rem; background: #fff; border: 1px solid var(--line); border-radius: .5rem; }
  figure.shot img { display: block; max-width: 100%; height: auto; border: 1px solid var(--line); border-radius: .25rem; }
  figure.shot .placeholder { display: flex; align-items: center; justify-content: center; height: 10rem; border: 1px dashed #d6d3d1; border-radius: .25rem; color: var(--muted); font-size: .9rem; background: #fafaf9; }
  figcaption { margin-top: .6rem; font-size: .875rem; color: var(--muted); }
  figcaption .cap { font-weight: 600; color: var(--ink); }
  ol.legend { list-style: none; padding: 0; margin: .4rem 0 0; display: grid; grid-template-columns: repeat(auto-fill, minmax(14rem, 1fr)); gap: .15rem .75rem; }
  ol.legend b { display: inline-flex; width: 1.3rem; height: 1.3rem; align-items: center; justify-content: center; border-radius: 50%; background: var(--amber); color: var(--ink); font-size: .7rem; margin-right: .35rem; }
  @media (max-width: 54rem) { .wrap { grid-template-columns: 1fr; } nav { position: static; height: auto; border-right: 0; border-bottom: 1px solid var(--line); } main { padding: 1.5rem 1rem 4rem; } }
  @media print { nav { display: none; } .wrap { display: block; } main { max-width: none; padding: 0; } figure.shot { break-inside: avoid; } }
</style>
</head>
<body>
<div class="wrap">
<nav aria-label="Contents">
  <div class="brand"><span>W</span>Writers' Guild</div>
  {{NAV}}
</nav>
<main>
<h1>The guide</h1>
<p class="lead">From signing in to a revised chapter, screen by screen, for people who may never have written a story.</p>
{{BODY}}
</main>
</div>
</body>
</html>
`
