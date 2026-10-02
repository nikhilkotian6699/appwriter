// Package api wires the HTTP layer: router, middleware, generated handlers
// and static files.
package api

import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgxpool"

	"writersguild/internal/auth"
	"writersguild/internal/config"
	"writersguild/internal/db/sqlcgen"
	"writersguild/internal/guild"
	"writersguild/internal/llm"
	"writersguild/internal/runs"
)

// Auth bundles what signing in needs.
type Auth struct {
	Signer       *auth.Signer
	Limiter      *auth.Limiter
	MaxAge       time.Duration
	CookieSecure bool
}

// Server implements the generated ServerInterface.
type Server struct {
	cfg    config.Config
	pool   *pgxpool.Pool
	q      *sqlcgen.Queries
	llm    llm.Client
	guild  *guild.Guild
	engine *runs.Engine
	auth   Auth
	log    *slog.Logger
	// resolveUser returns the account making the request: the session
	// cookie's account, or whatever a test pinned through SetUserResolver.
	resolveUser func(r *http.Request) (sqlcgen.User, error)
}

var _ ServerInterface = (*Server)(nil)

// NewServer wires a Server.
func NewServer(cfg config.Config, pool *pgxpool.Pool, q *sqlcgen.Queries, client llm.Client, g *guild.Guild, engine *runs.Engine, a Auth, log *slog.Logger) *Server {
	s := &Server{cfg: cfg, pool: pool, q: q, llm: client, guild: g, engine: engine, auth: a, log: log}
	if s.auth.Limiter == nil {
		s.auth.Limiter = auth.NewLimiter()
	}
	if s.auth.MaxAge <= 0 {
		s.auth.MaxAge = 30 * 24 * time.Hour
	}
	s.resolveUser = s.sessionUser
	return s
}

// errNoSession means the request carries no usable session cookie.
var errNoSession = errors.New("not signed in")

// sessionUser resolves the account from the session cookie. The cookie
// must verify, be young enough, and name an account whose auth version has
// not moved on (a password change or a disable bumps it).
func (s *Server) sessionUser(r *http.Request) (sqlcgen.User, error) {
	if s.auth.Signer == nil {
		return sqlcgen.User{}, errNoSession
	}
	c, err := r.Cookie(sessionCookie)
	if err != nil || c.Value == "" {
		return sqlcgen.User{}, errNoSession
	}
	sess, err := s.auth.Signer.Decode(c.Value, time.Now(), s.auth.MaxAge)
	if err != nil {
		return sqlcgen.User{}, errNoSession
	}
	u, err := s.q.GetUserByID(r.Context(), sess.UserID)
	if err != nil {
		return sqlcgen.User{}, errNoSession
	}
	if u.AuthVersion != sess.AuthVersion {
		return sqlcgen.User{}, errNoSession
	}
	return u, nil
}

// publicPaths need no account.
var publicPaths = map[string]bool{"/api/auth/login": true, "/api/auth/logout": true}

type ctxKey int

const userKey ctxKey = 1

// withUser puts the current account into the request context.
func (s *Server) withUser(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if publicPaths[r.URL.Path] {
			next.ServeHTTP(w, r)
			return
		}
		u, err := s.resolveUser(r)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized", "sign in to continue")
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
