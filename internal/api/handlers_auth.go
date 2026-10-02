package api

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"writersguild/internal/auth"
	"writersguild/internal/db/sqlcgen"
)

// sessionCookie is the name of the session cookie.
const sessionCookie = "wg_session"

// Login checks the credentials, applies the limiter and sets the cookie.
func (s *Server) Login(w http.ResponseWriter, r *http.Request) {
	var in LoginInput
	if err := decodeJSON(r, &in); err != nil {
		s.fail(w, err)
		return
	}
	username := strings.ToLower(strings.TrimSpace(in.Username))
	if username == "" || in.Password == "" {
		writeError(w, http.StatusUnauthorized, "invalid_credentials", "wrong username or password")
		return
	}
	if s.auth.Signer == nil {
		s.fail(w, errors.New("sessions are not configured"))
		return
	}
	key := auth.Key(username, clientAddr(r))
	if wait := s.auth.Limiter.Check(key); wait > 0 {
		s.tooManyAttempts(w, wait)
		return
	}
	u, err := s.q.GetUserByUsername(r.Context(), username)
	hash := auth.DummyHash
	known := err == nil
	if known {
		hash = u.PasswordHash
	} else if !errors.Is(err, pgx.ErrNoRows) {
		s.fail(w, err)
		return
	}
	// The comparison runs whether or not the user exists, so a wrong
	// username costs the same time as a wrong password.
	if !auth.CheckPassword(hash, in.Password) || !known {
		if wait := s.auth.Limiter.Fail(key); wait > 0 {
			s.tooManyAttempts(w, wait)
			return
		}
		writeError(w, http.StatusUnauthorized, "invalid_credentials", "wrong username or password")
		return
	}
	if u.DisabledAt != nil {
		writeError(w, http.StatusUnauthorized, "account_disabled", "this account is disabled")
		return
	}
	s.auth.Limiter.Reset(key)
	s.setSessionCookie(w, r, u)
	writeJSON(w, http.StatusOK, s.me(u))
}

func (s *Server) tooManyAttempts(w http.ResponseWriter, wait time.Duration) {
	secs := int(wait.Round(time.Second).Seconds())
	if secs < 1 {
		secs = 1
	}
	w.Header().Set("Retry-After", strconv.Itoa(secs))
	writeError(w, http.StatusTooManyRequests, "too_many_attempts", fmt.Sprintf("too many wrong passwords; try again in %s", humanWait(wait)))
}

func humanWait(d time.Duration) string {
	d = d.Round(time.Second)
	if d < time.Minute {
		return fmt.Sprintf("%d seconds", int(d.Seconds()))
	}
	m := int(d.Minutes())
	if m == 1 {
		return "a minute"
	}
	return fmt.Sprintf("%d minutes", m)
}

// Logout clears the cookie. It works without a valid session.
func (s *Server) Logout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: s.secureCookie(r)})
	w.WriteHeader(http.StatusNoContent)
}

// setSessionCookie issues a fresh session for the account as it stands now.
func (s *Server) setSessionCookie(w http.ResponseWriter, r *http.Request, u sqlcgen.User) {
	tok := s.auth.Signer.Encode(auth.Session{UserID: u.ID, AuthVersion: u.AuthVersion, IssuedAt: time.Now()})
	http.SetCookie(w, &http.Cookie{
		Name: sessionCookie, Value: tok, Path: "/", MaxAge: int(s.auth.MaxAge.Seconds()),
		HttpOnly: true, SameSite: http.SameSiteLaxMode, Secure: s.secureCookie(r),
	})
}

// secureCookie marks the cookie Secure when configured or when the request
// arrived over HTTPS (directly or through a proxy).
func (s *Server) secureCookie(r *http.Request) bool {
	if s.auth.CookieSecure || r.TLS != nil {
		return true
	}
	return strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

// clientAddr is the caller's address without the port; chi's RealIP has
// already honoured X-Forwarded-For.
func clientAddr(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func (s *Server) me(u sqlcgen.User) Me {
	return Me{User: toUser(u), AppName: s.cfg.AppName, DefaultModelAlias: s.cfg.DefaultModelAlias, AliasPrefix: s.cfg.AliasPrefix()}
}
