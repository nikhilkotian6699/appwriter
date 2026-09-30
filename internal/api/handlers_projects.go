package api

import (
	"net/http"
	"strings"

	"writersguild/internal/db/sqlcgen"
)

func validateProjectInput(in ProjectInput) (name, desc string, err error) {
	name = strings.TrimSpace(in.Name)
	if name == "" || len(name) > 200 {
		return "", "", errBadRequest("name must be 1 to 200 characters")
	}
	desc = strings.TrimSpace(stringOr(in.Description, ""))
	if len(desc) > 5000 {
		return "", "", errBadRequest("description must be at most 5000 characters")
	}
	return name, desc, nil
}

// ListProjects lists the account's projects, newest activity first.
func (s *Server) ListProjects(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r.Context())
	rows, err := s.q.ListProjects(r.Context(), u.ID)
	if err != nil {
		s.fail(w, err)
		return
	}
	out := make([]ProjectSummary, 0, len(rows))
	for _, p := range rows {
		out = append(out, toProjectSummary(p))
	}
	writeJSON(w, http.StatusOK, out)
}

// CreateProject starts a novel.
func (s *Server) CreateProject(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r.Context())
	var in ProjectInput
	if err := decodeJSON(r, &in); err != nil {
		s.fail(w, err)
		return
	}
	name, desc, err := validateProjectInput(in)
	if err != nil {
		s.fail(w, err)
		return
	}
	p, err := s.q.CreateProject(r.Context(), sqlcgen.CreateProjectParams{UserID: u.ID, Name: name, Description: desc})
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, toProject(p))
}

// GetProject returns one project.
func (s *Server) GetProject(w http.ResponseWriter, r *http.Request, projectId ProjectId) {
	u := currentUser(r.Context())
	p, err := s.q.GetProject(r.Context(), sqlcgen.GetProjectParams{ID: projectId, UserID: u.ID})
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toProject(p))
}

// UpdateProject renames or describes a project.
func (s *Server) UpdateProject(w http.ResponseWriter, r *http.Request, projectId ProjectId) {
	u := currentUser(r.Context())
	var in ProjectInput
	if err := decodeJSON(r, &in); err != nil {
		s.fail(w, err)
		return
	}
	name, desc, err := validateProjectInput(in)
	if err != nil {
		s.fail(w, err)
		return
	}
	p, err := s.q.UpdateProject(r.Context(), sqlcgen.UpdateProjectParams{ID: projectId, UserID: u.ID, Name: name, Description: desc})
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toProject(p))
}

// DeleteProject removes a project and everything in it.
func (s *Server) DeleteProject(w http.ResponseWriter, r *http.Request, projectId ProjectId) {
	u := currentUser(r.Context())
	n, err := s.q.DeleteProject(r.Context(), sqlcgen.DeleteProjectParams{ID: projectId, UserID: u.ID})
	if err != nil {
		s.fail(w, err)
		return
	}
	if n == 0 {
		s.fail(w, errNotFound("project"))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
