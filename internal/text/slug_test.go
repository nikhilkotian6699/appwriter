package text

import "testing"

func TestSlugify(t *testing.T) {
	cases := map[string]string{
		"Hemingway":            "hemingway",
		"García Márquez":       "garcia-marquez",
		"le Carré":             "le-carre",
		"Le Guin":              "le-guin",
		"Stephen King":         "stephen-king",
		"  Ursula K. Le Guin!": "ursula-k-le-guin",
		"editor-in-chief":      "editor-in-chief",
		"Ægir Ørn":             "gir-rn", // letters without a decomposition are dropped
		"":                     "writer",
		"编辑":                   "writer",
		"--- ---":              "writer",
		"Writer 2.0":           "writer-2-0",
	}
	for in, want := range cases {
		if got := Slugify(in); got != want {
			t.Errorf("Slugify(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestUniqueSlug(t *testing.T) {
	taken := map[string]bool{"hemingway": true, "hemingway-2": true}
	exists := func(s string) bool { return taken[s] }
	if got := UniqueSlug("le-guin", exists); got != "le-guin" {
		t.Fatalf("got %q", got)
	}
	if got := UniqueSlug("hemingway", exists); got != "hemingway-3" {
		t.Fatalf("got %q", got)
	}
}

func TestDefaultAlias(t *testing.T) {
	if got := DefaultAlias("writersguild", "garcia-marquez"); got != "writersguild-garcia-marquez" {
		t.Fatalf("got %q", got)
	}
}
