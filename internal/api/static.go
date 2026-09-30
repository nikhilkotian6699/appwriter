package api

import (
	"bytes"
	"io"
	"io/fs"
	"net/http"
	"path"
	"strings"
	"time"
)

// Static serves an embedded directory. With spaFallback, unknown paths get
// index.html so client-side routes work on reload; without it they get 404.
func Static(fsys fs.FS, spaFallback bool) http.Handler {
	return &staticHandler{fsys: fsys, spaFallback: spaFallback, modTime: time.Now()}
}

type staticHandler struct {
	fsys        fs.FS
	spaFallback bool
	modTime     time.Time
}

func (h *staticHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	name := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
	if name == "" || strings.HasSuffix(r.URL.Path, "/") {
		name = path.Join(name, "index.html")
	}
	if h.serve(w, r, name) {
		return
	}
	// a directory without a trailing slash
	if h.serve(w, r, path.Join(name, "index.html")) {
		return
	}
	if h.spaFallback && !strings.Contains(path.Base(name), ".") {
		if h.serve(w, r, "index.html") {
			return
		}
	}
	http.NotFound(w, r)
}

// serve writes the file when it exists and reports whether it did.
func (h *staticHandler) serve(w http.ResponseWriter, r *http.Request, name string) bool {
	f, err := h.fsys.Open(name)
	if err != nil {
		return false
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || info.IsDir() {
		return false
	}
	switch {
	case strings.HasPrefix(name, "assets/"):
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	case strings.HasSuffix(name, ".html"):
		w.Header().Set("Cache-Control", "no-cache")
	default:
		w.Header().Set("Cache-Control", "public, max-age=3600")
	}
	if rs, ok := f.(io.ReadSeeker); ok {
		http.ServeContent(w, r, name, h.modTime, rs)
		return true
	}
	data, err := io.ReadAll(f)
	if err != nil {
		return false
	}
	http.ServeContent(w, r, name, h.modTime, bytes.NewReader(data))
	return true
}
