package api

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"writersguild/internal/auth"
	"writersguild/internal/db/sqlcgen"
)

// accountTx runs fn inside a transaction that holds the accounts lock, so
// role, access and deletion changes are serialized across admins. Errors
// returned by fn roll everything back.
func (s *Server) accountTx(ctx context.Context, fn func(q *sqlcgen.Queries) error) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	q := s.q.WithTx(tx)
	if err := q.LockAccounts(ctx); err != nil {
		return err
	}
	if err := fn(q); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// loadTarget fetches and locks the account an admin wants to change.
func loadTarget(ctx context.Context, q *sqlcgen.Queries, id uuid.UUID) (sqlcgen.User, error) {
	u, err := q.GetUserForUpdate(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return u, errNotFound("account")
	}
	return u, err
}

// lastActiveAdmin reports whether target is the only admin who can still
// sign in, in which case it must keep its role and access.
func lastActiveAdmin(ctx context.Context, q *sqlcgen.Queries, target sqlcgen.User) (bool, error) {
	if target.Role != "admin" || target.DisabledAt != nil {
		return false, nil
	}
	n, err := q.CountActiveAdmins(ctx)
	if err != nil {
		return false, err
	}
	return n <= 1, nil
}

var errLastAdmin = errConflict("one active admin must remain")

// UpdateUser changes another account's display name or role.
func (s *Server) UpdateUser(w http.ResponseWriter, r *http.Request, userId UserId) {
	u := currentUser(r.Context())
	if err := requireAdmin(u); err != nil {
		s.fail(w, err)
		return
	}
	var in UserUpdateInput
	if err := decodeJSON(r, &in); err != nil {
		s.fail(w, err)
		return
	}
	if in.Role != nil && !userRoles[string(*in.Role)] {
		s.fail(w, errBadRequest("role must be admin or author"))
		return
	}
	if in.DisplayName != nil && len(strings.TrimSpace(*in.DisplayName)) > 120 {
		s.fail(w, errBadRequest("display_name must be at most 120 characters"))
		return
	}
	var out sqlcgen.User
	err := s.accountTx(r.Context(), func(q *sqlcgen.Queries) error {
		target, err := loadTarget(r.Context(), q, userId)
		if err != nil {
			return err
		}
		display := target.DisplayName
		if in.DisplayName != nil {
			display = strings.TrimSpace(*in.DisplayName)
		}
		role := target.Role
		if in.Role != nil && string(*in.Role) != target.Role {
			if target.ID == u.ID {
				return errForbidden("you cannot change your own role")
			}
			if string(*in.Role) == "author" {
				last, err := lastActiveAdmin(r.Context(), q, target)
				if err != nil {
					return err
				}
				if last {
					return errLastAdmin
				}
			}
			role = string(*in.Role)
		}
		out, err = q.UpdateUserByAdmin(r.Context(), sqlcgen.UpdateUserByAdminParams{ID: target.ID, DisplayName: display, Role: role})
		return err
	})
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toUser(out))
}

// SetUserPassword gives another account a new password and signs it out
// everywhere. Admins change their own password on the Account page, where
// the current one is required.
func (s *Server) SetUserPassword(w http.ResponseWriter, r *http.Request, userId UserId) {
	u := currentUser(r.Context())
	if err := requireAdmin(u); err != nil {
		s.fail(w, err)
		return
	}
	var in UserPasswordInput
	if err := decodeJSON(r, &in); err != nil {
		s.fail(w, err)
		return
	}
	if userId == u.ID {
		s.fail(w, errBadRequest("change your own password on the Account page"))
		return
	}
	if err := auth.ValidatePassword(in.NewPassword); err != nil {
		s.fail(w, errBadRequest("%s", err.Error()))
		return
	}
	hash, err := auth.HashPassword(in.NewPassword)
	if err != nil {
		s.fail(w, err)
		return
	}
	err = s.accountTx(r.Context(), func(q *sqlcgen.Queries) error {
		target, err := loadTarget(r.Context(), q, userId)
		if err != nil {
			return err
		}
		return q.UpdateUserPassword(r.Context(), sqlcgen.UpdateUserPasswordParams{ID: target.ID, PasswordHash: hash})
	})
	if err != nil {
		s.fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// DisableUser signs an account out, cancels its runs and keeps its work.
func (s *Server) DisableUser(w http.ResponseWriter, r *http.Request, userId UserId) {
	u := currentUser(r.Context())
	if err := requireAdmin(u); err != nil {
		s.fail(w, err)
		return
	}
	if userId == u.ID {
		s.fail(w, errForbidden("you cannot disable your own account"))
		return
	}
	var out sqlcgen.User
	var runIDs []uuid.UUID
	err := s.accountTx(r.Context(), func(q *sqlcgen.Queries) error {
		target, err := loadTarget(r.Context(), q, userId)
		if err != nil {
			return err
		}
		if target.DisabledAt != nil {
			out = target
			return nil
		}
		last, err := lastActiveAdmin(r.Context(), q, target)
		if err != nil {
			return err
		}
		if last {
			return errLastAdmin
		}
		out, err = q.DisableUser(r.Context(), target.ID)
		if err != nil {
			return err
		}
		runIDs, err = q.ListActiveRunIDsByUser(r.Context(), target.ID)
		return err
	})
	if err != nil {
		s.fail(w, err)
		return
	}
	// Runs executing here stop through the engine and record themselves;
	// whatever is left (another process, a crash) is marked cancelled.
	for _, id := range runIDs {
		s.engine.Cancel(id)
	}
	for _, id := range runIDs {
		waitCtx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		_ = s.engine.Wait(waitCtx, id)
		cancel()
	}
	if _, err := s.q.CancelRunsByUser(r.Context(), out.ID); err != nil {
		s.log.Error("cancel runs of disabled account", "user", out.ID, "err", err)
	}
	writeJSON(w, http.StatusOK, toUser(out))
}

// EnableUser lets a disabled account sign in again.
func (s *Server) EnableUser(w http.ResponseWriter, r *http.Request, userId UserId) {
	u := currentUser(r.Context())
	if err := requireAdmin(u); err != nil {
		s.fail(w, err)
		return
	}
	var out sqlcgen.User
	err := s.accountTx(r.Context(), func(q *sqlcgen.Queries) error {
		target, err := loadTarget(r.Context(), q, userId)
		if err != nil {
			return err
		}
		if target.DisabledAt == nil {
			out = target
			return nil
		}
		out, err = q.EnableUser(r.Context(), target.ID)
		return err
	})
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toUser(out))
}

// DeleteUser removes a disabled account and everything it owns, after the
// admin typed its username.
func (s *Server) DeleteUser(w http.ResponseWriter, r *http.Request, userId UserId) {
	u := currentUser(r.Context())
	if err := requireAdmin(u); err != nil {
		s.fail(w, err)
		return
	}
	var in UserDeleteInput
	if err := decodeJSON(r, &in); err != nil {
		s.fail(w, err)
		return
	}
	if userId == u.ID {
		s.fail(w, errForbidden("you cannot delete your own account"))
		return
	}
	err := s.accountTx(r.Context(), func(q *sqlcgen.Queries) error {
		target, err := loadTarget(r.Context(), q, userId)
		if err != nil {
			return err
		}
		if strings.ToLower(strings.TrimSpace(in.Username)) != target.Username {
			return errBadRequest("type the account's username to confirm the deletion")
		}
		if target.DisabledAt == nil {
			return errConflict("disable the account before deleting it")
		}
		return q.DeleteUser(r.Context(), target.ID)
	})
	if err != nil {
		s.fail(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
