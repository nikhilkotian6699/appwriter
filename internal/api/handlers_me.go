package api

import (
	"net/http"
	"sort"
	"strings"

	"writersguild/internal/db/sqlcgen"
	"writersguild/internal/guild"
)

// GetMe returns the current account and the alias configuration.
func (s *Server) GetMe(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r.Context())
	writeJSON(w, http.StatusOK, Me{
		User:              toUser(u),
		AppName:           s.cfg.AppName,
		DefaultModelAlias: s.cfg.DefaultModelAlias,
		AliasPrefix:       s.cfg.AliasPrefix(),
	})
}

// GetSettings returns the account's settings, creating defaults on first use.
func (s *Server) GetSettings(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r.Context())
	if err := s.q.EnsureSettings(r.Context(), u.ID); err != nil {
		s.fail(w, err)
		return
	}
	st, err := s.q.GetSettings(r.Context(), u.ID)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toSettings(st))
}

// UpdateSettings validates and stores the settings.
func (s *Server) UpdateSettings(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r.Context())
	var in SettingsInput
	if err := decodeJSON(r, &in); err != nil {
		s.fail(w, err)
		return
	}
	if in.SceneTokenLimit < 500 || in.SceneTokenLimit > 200000 {
		s.fail(w, errBadRequest("scene_token_limit must be between 500 and 200000"))
		return
	}
	if in.AutosaveSnapshotMinutes < 1 || in.AutosaveSnapshotMinutes > 1440 {
		s.fail(w, errBadRequest("autosave_snapshot_minutes must be between 1 and 1440"))
		return
	}
	st, err := s.q.UpsertSettings(r.Context(), sqlcgen.UpsertSettingsParams{
		UserID: u.ID, SceneTokenLimit: int32(in.SceneTokenLimit), AutosaveSnapshotMinutes: int32(in.AutosaveSnapshotMinutes),
	})
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toSettings(st))
}

func toSettings(st sqlcgen.UserSetting) Settings {
	return Settings{SceneTokenLimit: int(st.SceneTokenLimit), AutosaveSnapshotMinutes: int(st.AutosaveSnapshotMinutes)}
}

// ListGatewayModels asks the gateway for its models and filters by prefix.
func (s *Server) ListGatewayModels(w http.ResponseWriter, r *http.Request) {
	models, err := s.llm.ListModels(r.Context())
	if err != nil {
		s.fail(w, errGateway(guild.FriendlyError(err)))
		return
	}
	sort.Strings(models)
	prefix := s.cfg.AliasPrefix()
	aliases := make([]string, 0, len(models))
	for _, m := range models {
		if strings.HasPrefix(m, prefix) {
			aliases = append(aliases, m)
		}
	}
	writeJSON(w, http.StatusOK, GatewayModels{AliasPrefix: prefix, Aliases: aliases, AllModels: models})
}
