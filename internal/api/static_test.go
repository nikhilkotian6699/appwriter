package api

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func testFS() fstest.MapFS {
	return fstest.MapFS{
		"index.html":          {Data: []byte("<html>app</html>")},
		"assets/app-abc12.js": {Data: []byte("console.log(1)")},
		"favicon.svg":         {Data: []byte("<svg/>")},
		"docs/index.html":     {Data: []byte("<html>docs</html>")},
	}
}

func get(t *testing.T, h http.Handler, p string) (*http.Response, string) {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, p, nil))
	res := rec.Result()
	body, _ := io.ReadAll(res.Body)
	return res, string(body)
}

func TestStaticSPA(t *testing.T) {
	h := Static(testFS(), true)

	res, body := get(t, h, "/")
	if res.StatusCode != 200 || body != "<html>app</html>" || res.Header.Get("Cache-Control") != "no-cache" {
		t.Fatalf("root: %d %q %q", res.StatusCode, body, res.Header.Get("Cache-Control"))
	}
	if ct := res.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Fatalf("content type %q", ct)
	}

	res, body = get(t, h, "/assets/app-abc12.js")
	if res.StatusCode != 200 || body != "console.log(1)" || !strings.Contains(res.Header.Get("Cache-Control"), "immutable") {
		t.Fatalf("asset: %d %q %q", res.StatusCode, body, res.Header.Get("Cache-Control"))
	}

	// client-side route falls back to the app shell
	res, body = get(t, h, "/projects/9f1a/chapters/2")
	if res.StatusCode != 200 || body != "<html>app</html>" {
		t.Fatalf("spa route: %d %q", res.StatusCode, body)
	}

	// a missing file with an extension is a real 404, not the shell
	res, _ = get(t, h, "/assets/missing.js")
	if res.StatusCode != 404 {
		t.Fatalf("missing asset: %d", res.StatusCode)
	}

	// directories resolve to their index
	res, body = get(t, h, "/docs/")
	if res.StatusCode != 200 || body != "<html>docs</html>" {
		t.Fatalf("dir index: %d %q", res.StatusCode, body)
	}
	res, body = get(t, h, "/docs")
	if res.StatusCode != 200 || body != "<html>docs</html>" {
		t.Fatalf("dir without slash: %d %q", res.StatusCode, body)
	}

	// path traversal is cleaned, never escapes
	res, body = get(t, h, "/../../index.html")
	if res.StatusCode != 200 || body != "<html>app</html>" {
		t.Fatalf("traversal: %d %q", res.StatusCode, body)
	}

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST: %d", rec.Code)
	}
}

func TestStaticWithoutFallback(t *testing.T) {
	h := Static(testFS(), false)
	res, _ := get(t, h, "/nope")
	if res.StatusCode != 404 {
		t.Fatalf("expected 404 without fallback, got %d", res.StatusCode)
	}
	res, body := get(t, h, "/")
	if res.StatusCode != 200 || body != "<html>app</html>" {
		t.Fatalf("root: %d %q", res.StatusCode, body)
	}
}
