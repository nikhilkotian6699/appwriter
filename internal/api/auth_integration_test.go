package api

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"writersguild/internal/auth"
	"writersguild/internal/config"
	"writersguild/internal/db/sqlcgen"
	"writersguild/internal/guild"
	"writersguild/internal/llm"
	"writersguild/internal/runs"
	"writersguild/internal/testutil"
)

// authEnv is a server with the real cookie resolver and a limiter whose
// clock the test controls.
type authEnv struct {
	t       *testing.T
	pool    *pgxpool.Pool
	q       *sqlcgen.Queries
	handler http.Handler
	limiter *auth.Limiter
	now     time.Time
	user    sqlcgen.User
}

func newAuthEnv(t *testing.T) *authEnv {
	t.Helper()
	pool := testutil.DB(t)
	q := sqlcgen.New(pool)
	user := testutil.NewUser(t, q, "author")
	hash, _ := auth.HashPassword("correct horse battery")
	if err := q.UpdateUserPassword(context.Background(), sqlcgen.UpdateUserPasswordParams{ID: user.ID, PasswordHash: hash}); err != nil {
		t.Fatal(err)
	}
	user, _ = q.GetUserByID(context.Background(), user.ID)
	mock := llm.NewMock()
	cfg := config.Config{AppName: "writersguild", DefaultModelAlias: "lumos-chat", LLMTimeout: 5 * time.Second}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	tracker := runs.NewTracker(q, mock, cfg.AppName)
	engine := runs.NewEngine(tracker, q, 30*time.Second, logger)
	g := guild.New(engine, q, cfg.AppName, cfg.LLMTimeout)
	signer, _ := auth.NewSigner("integration-test-secret-that-is-long-enough-0123456789")
	e := &authEnv{t: t, pool: pool, q: q, now: time.Now(), user: user}
	e.limiter = auth.NewLimiter()
	e.limiter.Now = func() time.Time { return e.now }
	srv := NewServer(cfg, pool, q, mock, g, engine, Auth{Signer: signer, Limiter: e.limiter, MaxAge: time.Hour}, logger)
	static := fstest.MapFS{"index.html": {Data: []byte("app")}}
	e.handler = NewRouter(srv, static, static)
	return e
}

// do sends a request, optionally with a session cookie and a client address.
func (e *authEnv) do(method, path, body, cookie, addr string) *httptest.ResponseRecorder {
	e.t.Helper()
	var rd io.Reader
	if body != "" {
		rd = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, rd)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if cookie != "" {
		req.AddCookie(&http.Cookie{Name: sessionCookie, Value: cookie})
	}
	if addr != "" {
		req.RemoteAddr = addr + ":51234"
	}
	rec := httptest.NewRecorder()
	e.handler.ServeHTTP(rec, req)
	return rec
}

func sessionFrom(rec *httptest.ResponseRecorder) *http.Cookie {
	for _, c := range rec.Result().Cookies() {
		if c.Name == sessionCookie {
			return c
		}
	}
	return nil
}

func TestIntegrationLoginAndSessions(t *testing.T) {
	e := newAuthEnv(t)
	login := func(username, password, addr string) *httptest.ResponseRecorder {
		return e.do("POST", "/api/auth/login", `{"username":"`+username+`","password":"`+password+`"}`, "", addr)
	}

	// Nothing works without a session, except health and the auth routes.
	if rec := e.do("GET", "/api/me", "", "", ""); rec.Code != 401 || !strings.Contains(rec.Body.String(), "unauthorized") {
		t.Fatalf("anonymous /api/me: %d %s", rec.Code, rec.Body.String())
	}
	if rec := e.do("GET", "/api/healthz", "", "", ""); rec.Code != 200 {
		t.Fatalf("health should be public: %d", rec.Code)
	}
	if rec := e.do("GET", "/api/projects", "", "garbage.token.value", ""); rec.Code != 401 {
		t.Fatalf("garbage cookie: %d", rec.Code)
	}

	// A wrong username and a wrong password are refused the same way.
	wrongUser := login("nobody-here", "correct horse battery", "10.0.0.1")
	wrongPass := login(e.user.Username, "wrong password", "10.0.0.1")
	if wrongUser.Code != 401 || wrongPass.Code != 401 || wrongUser.Body.String() != wrongPass.Body.String() || !strings.Contains(wrongPass.Body.String(), "invalid_credentials") {
		t.Fatalf("wrong user %d %s / wrong pass %d %s", wrongUser.Code, wrongUser.Body.String(), wrongPass.Code, wrongPass.Body.String())
	}
	if sessionFrom(wrongPass) != nil {
		t.Fatal("a failed login must not set a cookie")
	}

	// Sign in: cookie set, /api/me works, logout clears it.
	ok := login(strings.ToUpper(e.user.Username), "correct horse battery", "10.0.0.1")
	if ok.Code != 200 || !strings.Contains(ok.Body.String(), e.user.Username) {
		t.Fatalf("login: %d %s", ok.Code, ok.Body.String())
	}
	c := sessionFrom(ok)
	if c == nil || !c.HttpOnly || c.SameSite != http.SameSiteLaxMode || c.Path != "/" || c.MaxAge != 3600 || c.Secure {
		t.Fatalf("cookie %+v", c)
	}
	if rec := e.do("GET", "/api/me", "", c.Value, ""); rec.Code != 200 {
		t.Fatalf("/api/me with cookie: %d %s", rec.Code, rec.Body.String())
	}
	// Over HTTPS (behind a proxy) the cookie is Secure.
	req := httptest.NewRequest("POST", "/api/auth/login", strings.NewReader(`{"username":"`+e.user.Username+`","password":"correct horse battery"}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Forwarded-Proto", "https")
	rec := httptest.NewRecorder()
	e.handler.ServeHTTP(rec, req)
	if sc := sessionFrom(rec); sc == nil || !sc.Secure {
		t.Fatalf("https login cookie %+v", sc)
	}
	out := e.do("POST", "/api/auth/logout", "", c.Value, "")
	if out.Code != 204 || sessionFrom(out) == nil || sessionFrom(out).MaxAge >= 0 {
		t.Fatalf("logout %d cookie %+v", out.Code, sessionFrom(out))
	}

	// A password change ends every existing session.
	newHash, _ := auth.HashPassword("a brand new password")
	if err := e.q.UpdateUserPassword(context.Background(), sqlcgen.UpdateUserPasswordParams{ID: e.user.ID, PasswordHash: newHash}); err != nil {
		t.Fatal(err)
	}
	if rec := e.do("GET", "/api/me", "", c.Value, ""); rec.Code != 401 {
		t.Fatalf("old session after password change: %d", rec.Code)
	}
	if rec := login(e.user.Username, "correct horse battery", "10.0.0.1"); rec.Code != 401 {
		t.Fatalf("old password after change: %d", rec.Code)
	}
	fresh := login(e.user.Username, "a brand new password", "10.0.0.1")
	if fresh.Code != 200 {
		t.Fatalf("new password: %d %s", fresh.Code, fresh.Body.String())
	}
	fc := sessionFrom(fresh)

	// A disabled account can neither sign in nor keep its session.
	if _, err := e.pool.Exec(context.Background(), "UPDATE users SET disabled_at = now(), auth_version = auth_version + 1 WHERE id = $1", e.user.ID); err != nil {
		t.Fatal(err)
	}
	if rec := e.do("GET", "/api/me", "", fc.Value, ""); rec.Code != 401 {
		t.Fatalf("disabled session: %d", rec.Code)
	}
	if rec := login(e.user.Username, "a brand new password", "10.0.0.1"); rec.Code != 401 || !strings.Contains(rec.Body.String(), "account_disabled") {
		t.Fatalf("disabled login: %d %s", rec.Code, rec.Body.String())
	}
}

func TestIntegrationLoginLockout(t *testing.T) {
	e := newAuthEnv(t)
	login := func(password, addr string) *httptest.ResponseRecorder {
		return e.do("POST", "/api/auth/login", `{"username":"`+e.user.Username+`","password":"`+password+`"}`, "", addr)
	}
	for i := 1; i <= 4; i++ {
		if rec := login("wrong", "10.0.0.5"); rec.Code != 401 {
			t.Fatalf("attempt %d: %d", i, rec.Code)
		}
	}
	// The fifth wrong password locks the username for this address.
	fifth := login("wrong", "10.0.0.5")
	if fifth.Code != 429 || fifth.Header().Get("Retry-After") != "60" || !strings.Contains(fifth.Body.String(), "too_many_attempts") || !strings.Contains(fifth.Body.String(), "a minute") {
		t.Fatalf("fifth attempt: %d %s retry-after=%q", fifth.Code, fifth.Body.String(), fifth.Header().Get("Retry-After"))
	}
	// Even the right password waits while locked; another address does not.
	if rec := login("correct horse battery", "10.0.0.5"); rec.Code != 429 {
		t.Fatalf("right password during lock: %d", rec.Code)
	}
	if rec := login("correct horse battery", "10.0.0.6"); rec.Code != 200 {
		t.Fatalf("other address: %d %s", rec.Code, rec.Body.String())
	}
	e.now = e.now.Add(61 * time.Second)
	// The next wrong password doubles the wait.
	if rec := login("wrong", "10.0.0.5"); rec.Code != 429 || rec.Header().Get("Retry-After") != "120" {
		t.Fatalf("sixth attempt: %d retry-after=%q", rec.Code, rec.Header().Get("Retry-After"))
	}
	e.now = e.now.Add(2*time.Minute + time.Second)
	// A right password resets the count.
	if rec := login("correct horse battery", "10.0.0.5"); rec.Code != 200 {
		t.Fatalf("after waiting: %d %s", rec.Code, rec.Body.String())
	}
	if rec := login("wrong", "10.0.0.5"); rec.Code != 401 {
		t.Fatalf("after reset the first wrong password is free again: %d", rec.Code)
	}
	// Unknown usernames are limited too, so guessing names costs the same.
	for i := 0; i < 5; i++ {
		e.do("POST", "/api/auth/login", `{"username":"ghost","password":"x"}`, "", "10.0.0.9")
	}
	if rec := e.do("POST", "/api/auth/login", `{"username":"ghost","password":"x"}`, "", "10.0.0.9"); rec.Code != 429 {
		t.Fatalf("unknown username lockout: %d", rec.Code)
	}
}

// TestIntegrationUsersAndAccount covers adding accounts (admins only), the
// account page's changes, and that a password change ends other sessions
// while keeping this one.
func TestIntegrationUsersAndAccount(t *testing.T) {
	e := newAuthEnv(t)
	ctx := context.Background()
	if err := e.q.SetUserRole(ctx, sqlcgen.SetUserRoleParams{ID: e.user.ID, Role: "admin"}); err != nil {
		t.Fatal(err)
	}
	login := func(username, password string) *httptest.ResponseRecorder {
		return e.do("POST", "/api/auth/login", `{"username":"`+username+`","password":"`+password+`"}`, "", "10.0.0.2")
	}
	admin := sessionFrom(login(e.user.Username, "correct horse battery"))
	if admin == nil {
		t.Fatal("admin login failed")
	}

	// Add an author.
	newName := "t" + strings.ToLower(e.user.Username[1:7]) + "x"
	rec := e.do("POST", "/api/users", `{"username":"`+strings.ToUpper(newName)+`","display_name":"  Paige Turner ","password":"first password 1","role":"author"}`, admin.Value, "")
	if rec.Code != 201 || !strings.Contains(rec.Body.String(), `"username":"`+newName+`"`) || !strings.Contains(rec.Body.String(), `"display_name":"Paige Turner"`) {
		t.Fatalf("create user: %d %s", rec.Code, rec.Body.String())
	}
	author, err := e.q.GetUserByUsername(ctx, newName)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = e.q.DeleteUser(context.Background(), author.ID) })
	if writers, _ := e.q.ListWriters(ctx, author.ID); len(writers) != 8 {
		t.Fatalf("new account should be seeded with 8 writers, got %d", len(writers))
	}
	if rec := e.do("GET", "/api/users", "", admin.Value, ""); rec.Code != 200 || !strings.Contains(rec.Body.String(), newName) || strings.Contains(rec.Body.String(), "password") {
		t.Fatalf("list users: %d %s", rec.Code, rec.Body.String()[:min(200, len(rec.Body.String()))])
	}
	// Validation and uniqueness.
	for body, want := range map[string]int{
		`{"username":"Ab","password":"first password 1","role":"author"}`:              400,
		`{"username":"newperson","password":"short","role":"author"}`:                  400,
		`{"username":"newperson","password":"first password 1","role":"editor"}`:       400,
		`{"username":"` + newName + `","password":"first password 1","role":"author"}`: 409,
	} {
		if rec := e.do("POST", "/api/users", body, admin.Value, ""); rec.Code != want {
			t.Fatalf("create %s: %d, want %d: %s", body, rec.Code, want, rec.Body.String())
		}
	}

	// The author can sign in, but cannot see or add accounts.
	authorCookie := sessionFrom(login(newName, "first password 1"))
	if authorCookie == nil {
		t.Fatal("author login failed")
	}
	if rec := e.do("GET", "/api/users", "", authorCookie.Value, ""); rec.Code != 403 || !strings.Contains(rec.Body.String(), "forbidden") {
		t.Fatalf("author listing users: %d %s", rec.Code, rec.Body.String())
	}
	if rec := e.do("POST", "/api/users", `{"username":"sneaky","password":"first password 1","role":"admin"}`, authorCookie.Value, ""); rec.Code != 403 {
		t.Fatalf("author adding users: %d", rec.Code)
	}

	// Account page: display name, then password.
	if rec := e.do("PUT", "/api/account", `{"display_name":" P. Turner "}`, authorCookie.Value, ""); rec.Code != 200 || !strings.Contains(rec.Body.String(), `"display_name":"P. Turner"`) {
		t.Fatalf("display name: %d %s", rec.Code, rec.Body.String())
	}
	second := sessionFrom(login(newName, "first password 1")) // another browser
	if rec := e.do("PUT", "/api/account/password", `{"current_password":"wrong","new_password":"second password 2"}`, authorCookie.Value, ""); rec.Code != 400 || !strings.Contains(rec.Body.String(), "wrong_password") {
		t.Fatalf("wrong current password: %d %s", rec.Code, rec.Body.String())
	}
	if rec := e.do("PUT", "/api/account/password", `{"current_password":"first password 1","new_password":"short"}`, authorCookie.Value, ""); rec.Code != 400 {
		t.Fatalf("short new password: %d", rec.Code)
	}
	changed := e.do("PUT", "/api/account/password", `{"current_password":"first password 1","new_password":"second password 2"}`, authorCookie.Value, "")
	if changed.Code != 204 {
		t.Fatalf("change password: %d %s", changed.Code, changed.Body.String())
	}
	fresh := sessionFrom(changed)
	if fresh == nil || fresh.Value == authorCookie.Value {
		t.Fatal("a password change should issue a fresh cookie for this browser")
	}
	if rec := e.do("GET", "/api/me", "", fresh.Value, ""); rec.Code != 200 {
		t.Fatalf("this browser should stay signed in: %d", rec.Code)
	}
	if rec := e.do("GET", "/api/me", "", second.Value, ""); rec.Code != 401 {
		t.Fatalf("the other browser should be signed out: %d", rec.Code)
	}
	if rec := login(newName, "first password 1"); rec.Code != 401 {
		t.Fatalf("old password still works: %d", rec.Code)
	}
	if rec := login(newName, "second password 2"); rec.Code != 200 {
		t.Fatalf("new password: %d %s", rec.Code, rec.Body.String())
	}
}
