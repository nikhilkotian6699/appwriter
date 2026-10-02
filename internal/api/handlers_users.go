package api

import (
	"errors"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"

	"writersguild/internal/auth"
	"writersguild/internal/db/sqlcgen"
	"writersguild/internal/seed"
)

var userRoles = map[string]bool{"admin": true, "author": true}

// errForbidden is the answer for authors on admin-only routes.
func errForbidden(msg string) error {
	return &httpError{status: http.StatusForbidden, code: "forbidden", msg: msg}
}

// requireAdmin refuses non-admins.
func requireAdmin(u sqlcgen.User) error {
	if u.Role != "admin" {
		return errForbidden("only an admin can do that")
	}
	return nil
}

// ListUsers lists every account; admins only.
func (s *Server) ListUsers(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r.Context())
	if err := requireAdmin(u); err != nil {
		s.fail(w, err)
		return
	}
	rows, err := s.q.ListUsers(r.Context())
	if err != nil {
		s.fail(w, err)
		return
	}
	out := make([]User, 0, len(rows))
	for _, row := range rows {
		out = append(out, toUser(row))
	}
	writeJSON(w, http.StatusOK, out)
}

// CreateUser adds an account with its first password; admins only.
func (s *Server) CreateUser(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r.Context())
	if err := requireAdmin(u); err != nil {
		s.fail(w, err)
		return
	}
	var in UserCreateInput
	if err := decodeJSON(r, &in); err != nil {
		s.fail(w, err)
		return
	}
	username := strings.ToLower(strings.TrimSpace(in.Username))
	if err := auth.ValidateUsername(username); err != nil {
		s.fail(w, errBadRequest("%s", err.Error()))
		return
	}
	if err := auth.ValidatePassword(in.Password); err != nil {
		s.fail(w, errBadRequest("%s", err.Error()))
		return
	}
	role := string(in.Role)
	if !userRoles[role] {
		s.fail(w, errBadRequest("role must be admin or author"))
		return
	}
	display := strings.TrimSpace(stringOr(in.DisplayName, ""))
	if len(display) > 120 {
		s.fail(w, errBadRequest("display_name must be at most 120 characters"))
		return
	}
	hash, err := auth.HashPassword(in.Password)
	if err != nil {
		s.fail(w, err)
		return
	}
	created, err := s.q.CreateUser(r.Context(), sqlcgen.CreateUserParams{Username: username, DisplayName: display, PasswordHash: hash, Role: role})
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			s.fail(w, errConflict("that username is taken"))
			return
		}
		s.fail(w, err)
		return
	}
	if err := seed.ForUser(r.Context(), s.q, created.ID, s.cfg.AppName, s.cfg.DefaultModelAlias); err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, toUser(created))
}

// UpdateAccount changes the signed-in account's display name.
func (s *Server) UpdateAccount(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r.Context())
	var in AccountInput
	if err := decodeJSON(r, &in); err != nil {
		s.fail(w, err)
		return
	}
	display := strings.TrimSpace(in.DisplayName)
	if len(display) > 120 {
		s.fail(w, errBadRequest("display_name must be at most 120 characters"))
		return
	}
	row, err := s.q.UpdateUserDisplayName(r.Context(), sqlcgen.UpdateUserDisplayNameParams{ID: u.ID, DisplayName: display})
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toUser(row))
}

// ChangePassword sets a new password after checking the current one. The
// account's auth version moves on, which ends every other session; this
// browser gets a fresh cookie.
func (s *Server) ChangePassword(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r.Context())
	var in PasswordChangeInput
	if err := decodeJSON(r, &in); err != nil {
		s.fail(w, err)
		return
	}
	if !auth.CheckPassword(u.PasswordHash, in.CurrentPassword) {
		s.fail(w, &httpError{status: http.StatusBadRequest, code: "wrong_password", msg: "the current password is wrong"})
		return
	}
	if err := auth.ValidatePassword(in.NewPassword); err != nil {
		s.fail(w, errBadRequest("%s", err.Error()))
		return
	}
	if in.NewPassword == in.CurrentPassword {
		s.fail(w, errBadRequest("the new password must differ from the current one"))
		return
	}
	hash, err := auth.HashPassword(in.NewPassword)
	if err != nil {
		s.fail(w, err)
		return
	}
	if err := s.q.UpdateUserPassword(r.Context(), sqlcgen.UpdateUserPasswordParams{ID: u.ID, PasswordHash: hash}); err != nil {
		s.fail(w, err)
		return
	}
	fresh, err := s.q.GetUserByID(r.Context(), u.ID)
	if err != nil {
		s.fail(w, err)
		return
	}
	if s.auth.Signer != nil {
		s.setSessionCookie(w, r, fresh)
	}
	w.WriteHeader(http.StatusNoContent)
}
