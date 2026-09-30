package api

import (
	"context"
	"net/http"
	"sort"
	"strings"

	"github.com/google/uuid"

	"writersguild/internal/db/sqlcgen"
	"writersguild/internal/guild"
	"writersguild/internal/text"
)

var writerRoles = map[WriterRole]bool{"critic": true, "co-writer": true}

type writerFields struct {
	name   string
	prompt string
	roles  []string
	temp   float64
	alias  string
}

func validateWriterInput(in WriterInput) (writerFields, error) {
	f := writerFields{name: strings.TrimSpace(in.Name), prompt: strings.TrimSpace(in.SystemPrompt), temp: in.Temperature}
	if f.name == "" || len(f.name) > 120 {
		return f, errBadRequest("name must be 1 to 120 characters")
	}
	if len(f.prompt) > 20000 {
		return f, errBadRequest("system_prompt must be at most 20000 characters")
	}
	if f.temp < 0 || f.temp > 2 {
		return f, errBadRequest("temperature must be between 0 and 2")
	}
	seen := map[WriterRole]bool{}
	f.roles = []string{}
	for _, r := range in.Roles {
		if !writerRoles[r] {
			return f, errBadRequest("unknown role %q", r)
		}
		if !seen[r] {
			seen[r] = true
			f.roles = append(f.roles, string(r))
		}
	}
	sort.Strings(f.roles)
	f.alias = strings.TrimSpace(stringOr(in.ModelAlias, ""))
	if len(f.alias) > 200 {
		return f, errBadRequest("model_alias must be at most 200 characters")
	}
	return f, nil
}

// uniqueSlug picks a slug no other writer of the account uses.
func (s *Server) uniqueSlug(ctx context.Context, userID uuid.UUID, name string) (string, error) {
	slugs, err := s.q.ListWriterSlugs(ctx, userID)
	if err != nil {
		return "", err
	}
	taken := make(map[string]bool, len(slugs))
	for _, sl := range slugs {
		taken[sl] = true
	}
	return text.UniqueSlug(text.Slugify(name), func(c string) bool { return taken[c] }), nil
}

// ListWriters lists the account's writers, system agents last.
func (s *Server) ListWriters(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r.Context())
	rows, err := s.q.ListWriters(r.Context(), u.ID)
	if err != nil {
		s.fail(w, err)
		return
	}
	out := make([]Writer, 0, len(rows))
	for _, wr := range rows {
		out = append(out, toWriter(wr))
	}
	writeJSON(w, http.StatusOK, out)
}

// CreateWriter adds a writer; the slug is generated and the alias defaults
// to the convention.
func (s *Server) CreateWriter(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r.Context())
	var in WriterInput
	if err := decodeJSON(r, &in); err != nil {
		s.fail(w, err)
		return
	}
	f, err := validateWriterInput(in)
	if err != nil {
		s.fail(w, err)
		return
	}
	slug, err := s.uniqueSlug(r.Context(), u.ID, f.name)
	if err != nil {
		s.fail(w, err)
		return
	}
	if f.alias == "" {
		f.alias = text.DefaultAlias(s.cfg.AppName, slug)
	}
	wr, err := s.q.CreateWriter(r.Context(), sqlcgen.CreateWriterParams{
		UserID: u.ID, Name: f.name, Slug: slug, ModelAlias: f.alias, SystemPrompt: f.prompt, Roles: f.roles, Enabled: in.Enabled, Temperature: f.temp, IsSystem: false,
	})
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, toWriter(wr))
}

// GetWriter returns one writer.
func (s *Server) GetWriter(w http.ResponseWriter, r *http.Request, writerId WriterId) {
	u := currentUser(r.Context())
	wr, err := s.q.GetWriter(r.Context(), sqlcgen.GetWriterParams{ID: writerId, UserID: u.ID})
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toWriter(wr))
}

// UpdateWriter edits a writer. System agents keep their slug and have no
// critic or co-writer role.
func (s *Server) UpdateWriter(w http.ResponseWriter, r *http.Request, writerId WriterId) {
	u := currentUser(r.Context())
	var in WriterInput
	if err := decodeJSON(r, &in); err != nil {
		s.fail(w, err)
		return
	}
	f, err := validateWriterInput(in)
	if err != nil {
		s.fail(w, err)
		return
	}
	cur, err := s.q.GetWriter(r.Context(), sqlcgen.GetWriterParams{ID: writerId, UserID: u.ID})
	if err != nil {
		s.fail(w, err)
		return
	}
	if f.alias == "" {
		s.fail(w, errBadRequest("model_alias must not be empty"))
		return
	}
	if cur.IsSystem {
		f.roles = []string{}
	}
	wr, err := s.q.UpdateWriter(r.Context(), sqlcgen.UpdateWriterParams{
		ID: writerId, UserID: u.ID, Name: f.name, ModelAlias: f.alias, SystemPrompt: f.prompt, Roles: f.roles, Enabled: in.Enabled, Temperature: f.temp,
	})
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toWriter(wr))
}

// DeleteWriter removes a non-system writer.
func (s *Server) DeleteWriter(w http.ResponseWriter, r *http.Request, writerId WriterId) {
	u := currentUser(r.Context())
	cur, err := s.q.GetWriter(r.Context(), sqlcgen.GetWriterParams{ID: writerId, UserID: u.ID})
	if err != nil {
		s.fail(w, err)
		return
	}
	if cur.IsSystem {
		s.fail(w, errBadRequest("system agents cannot be deleted; disable or edit them instead"))
		return
	}
	n, err := s.q.DeleteWriter(r.Context(), sqlcgen.DeleteWriterParams{ID: writerId, UserID: u.ID})
	if err != nil {
		s.fail(w, err)
		return
	}
	if n == 0 {
		s.fail(w, errNotFound("writer"))
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// DuplicateWriter copies a writer under a new name and slug.
func (s *Server) DuplicateWriter(w http.ResponseWriter, r *http.Request, writerId WriterId) {
	u := currentUser(r.Context())
	cur, err := s.q.GetWriter(r.Context(), sqlcgen.GetWriterParams{ID: writerId, UserID: u.ID})
	if err != nil {
		s.fail(w, err)
		return
	}
	if cur.IsSystem {
		s.fail(w, errBadRequest("system agents cannot be duplicated"))
		return
	}
	name := cur.Name + " (copy)"
	slug, err := s.uniqueSlug(r.Context(), u.ID, name)
	if err != nil {
		s.fail(w, err)
		return
	}
	wr, err := s.q.CreateWriter(r.Context(), sqlcgen.CreateWriterParams{
		UserID: u.ID, Name: name, Slug: slug, ModelAlias: cur.ModelAlias, SystemPrompt: cur.SystemPrompt, Roles: cur.Roles, Enabled: cur.Enabled, Temperature: cur.Temperature, IsSystem: false,
	})
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, toWriter(wr))
}

// TestWriter sends a sample request through the gateway with the writer as
// it stands in the form, saved or not.
func (s *Server) TestWriter(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r.Context())
	var in WriterTestInput
	if err := decodeJSON(r, &in); err != nil {
		s.fail(w, err)
		return
	}
	alias := strings.TrimSpace(in.ModelAlias)
	if alias == "" {
		s.fail(w, errBadRequest("model_alias must not be empty"))
		return
	}
	if in.Temperature < 0 || in.Temperature > 2 {
		s.fail(w, errBadRequest("temperature must be between 0 and 2"))
		return
	}
	input := guild.TestWriterInput{Name: strings.TrimSpace(in.Name), ModelAlias: alias, SystemPrompt: in.SystemPrompt, Temperature: in.Temperature}
	if in.WriterId != nil {
		wr, err := s.q.GetWriter(r.Context(), sqlcgen.GetWriterParams{ID: *in.WriterId, UserID: u.ID})
		if err != nil {
			s.fail(w, errNotFound("writer"))
			return
		}
		input.WriterID = uuid.NullUUID{UUID: wr.ID, Valid: true}
		input.Slug = wr.Slug
	}
	ctx, cancel := context.WithTimeout(r.Context(), s.cfg.LLMTimeout)
	defer cancel()
	res, err := s.guild.TestWriter(ctx, u, input)
	if err != nil {
		s.fail(w, err)
		return
	}
	out := WriterTestResult{
		Ok:               res.Error == "",
		RunId:            res.RunID,
		ModelAlias:       res.ModelAlias,
		PromptTokens:     res.Usage.PromptTokens,
		CompletionTokens: res.Usage.CompletionTokens,
		CostKnown:        res.CostKnown,
		CostEstimated:    res.CostEstimated,
		LatencyMs:        int(res.Latency.Milliseconds()),
	}
	if res.Error != "" {
		out.Error = ptr(res.Error)
	} else {
		out.Reply = ptr(res.Reply)
	}
	if res.CostKnown {
		out.CostUsd = ptr(res.CostUSD)
	}
	writeJSON(w, http.StatusOK, out)
}
