// Package api wires the HTTP layer: router, middleware, generated handlers
// and static files.
package api

import (
	"context"
	"io/fs"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgxpool"

	"writersguild/internal/config"
	"writersguild/internal/db/sqlcgen"
	"writersguild/internal/guild"
	"writersguild/internal/llm"
)

// Server implements the generated ServerInterface.
type Server struct {
	cfg   config.Config
	pool  *pgxpool.Pool
	q     *sqlcgen.Queries
	llm   llm.Client
	guild *guild.Guild
	log   *slog.Logger
	// resolveUser returns the account making the request. Until milestone 7
	// this is the single bootstrap account.
	resolveUser func(r *http.Request) (sqlcgen.User, error)
}

var _ ServerInterface = (*Server)(nil)

// NewServer wires a Server.
func NewServer(cfg config.Config, pool *pgxpool.Pool, q *sqlcgen.Queries, client llm.Client, g *guild.Guild, log *slog.Logger) *Server {
	s := &Server{cfg: cfg, pool: pool, q: q, llm: client, guild: g, log: log}
	s.resolveUser = s.singleUser
	return s
}

// singleUser resolves the first (only) account.
func (s *Server) singleUser(r *http.Request) (sqlcgen.User, error) {
	return s.q.GetFirstUser(r.Context())
}

type ctxKey int

const userKey ctxKey = 1

// withUser puts the current account into the request context.
func (s *Server) withUser(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u, err := s.resolveUser(r)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized", "no account is signed in")
			return
		}
		if u.DisabledAt != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized", "this account is disabled")
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), userKey, u)))
	})
}

// currentUser reads the account placed by withUser.
func currentUser(ctx context.Context) sqlcgen.User {
	u, _ := ctx.Value(userKey).(sqlcgen.User)
	return u
}

// NewRouter builds the full HTTP handler: API under /api, the guide under
// /guide/ and the single-page app everywhere else.
func NewRouter(s *Server, webFS, guideFS fs.FS) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(requestLogger(s.log))
	r.Use(middleware.Recoverer)

	r.Get("/api/healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	r.Group(func(r chi.Router) {
		r.Use(s.withUser)
		HandlerWithOptions(s, ChiServerOptions{
			BaseRouter: r,
			ErrorHandlerFunc: func(w http.ResponseWriter, _ *http.Request, err error) {
				writeError(w, http.StatusBadRequest, "bad_request", err.Error())
			},
		})
	})

	spa := Static(webFS, true)
	guide := http.StripPrefix("/guide", Static(guideFS, false))
	r.Handle("/guide", http.RedirectHandler("/guide/", http.StatusMovedPermanently))
	r.Handle("/guide/*", guide)
	r.NotFound(func(w http.ResponseWriter, req *http.Request) {
		if strings.HasPrefix(req.URL.Path, "/api/") {
			writeError(w, http.StatusNotFound, "not_found", "no such route")
			return
		}
		spa.ServeHTTP(w, req)
	})
	r.MethodNotAllowed(func(w http.ResponseWriter, req *http.Request) {
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
	})
	return r
}

func requestLogger(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			next.ServeHTTP(ww, r)
			if strings.HasPrefix(r.URL.Path, "/api/") {
				log.Info("http", "method", r.Method, "path", r.URL.Path, "status", ww.Status(), "ms", time.Since(start).Milliseconds())
			}
		})
	}
}
