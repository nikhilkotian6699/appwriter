package api

import (
	"net/http"

	"writersguild/internal/db/sqlcgen"
)

// SetUserResolver replaces how the current account is found. Milestone 7
// installs cookie authentication through it; tests pin a fixed account.
func (s *Server) SetUserResolver(fn func(r *http.Request) (sqlcgen.User, error)) {
	s.resolveUser = fn
}
