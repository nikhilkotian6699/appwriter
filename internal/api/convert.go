package api

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"

	"github.com/google/uuid"

	"writersguild/internal/db/sqlcgen"
)

// hashContent identifies a chapter text; revisions and saves compare it.
func hashContent(md string) string {
	sum := sha256.Sum256([]byte(md))
	return hex.EncodeToString(sum[:])
}

func toUser(u sqlcgen.User) User {
	return User{Id: u.ID, Username: u.Username, DisplayName: u.DisplayName, Role: UserRole(u.Role), CreatedAt: u.CreatedAt}
}

func toProject(p sqlcgen.Project) Project {
	return Project{Id: p.ID, Name: p.Name, Description: p.Description, CreatedAt: p.CreatedAt, UpdatedAt: p.UpdatedAt}
}

func toProjectSummary(p sqlcgen.ListProjectsRow) ProjectSummary {
	return ProjectSummary{Id: p.ID, Name: p.Name, Description: p.Description, CreatedAt: p.CreatedAt, UpdatedAt: p.UpdatedAt, ChapterCount: int(p.ChapterCount)}
}

func toChapter(c sqlcgen.Chapter) Chapter {
	return Chapter{Id: c.ID, ProjectId: c.ProjectID, Title: c.Title, Position: int(c.Position), ContentMd: c.ContentMd, ContentHash: c.ContentHash, CreatedAt: c.CreatedAt, UpdatedAt: c.UpdatedAt}
}

func toChapterSummary(c sqlcgen.ListChaptersRow) ChapterSummary {
	return ChapterSummary{Id: c.ID, ProjectId: c.ProjectID, Title: c.Title, Position: int(c.Position), ContentHash: c.ContentHash, ContentLength: int(c.ContentLength), CreatedAt: c.CreatedAt, UpdatedAt: c.UpdatedAt}
}

func toVersion(v sqlcgen.ChapterVersion) ChapterVersion {
	return ChapterVersion{Id: v.ID, ChapterId: v.ChapterID, Kind: ChapterVersionKind(v.Kind), Label: v.Label, ContentMd: v.ContentMd, ContentHash: v.ContentHash, CreatedAt: v.CreatedAt}
}

func toVersionSummary(v sqlcgen.ListChapterVersionsRow) ChapterVersionSummary {
	return ChapterVersionSummary{Id: v.ID, ChapterId: v.ChapterID, Kind: ChapterVersionSummaryKind(v.Kind), Label: v.Label, ContentHash: v.ContentHash, ContentLength: int(v.ContentLength), CreatedAt: v.CreatedAt}
}

func toBibleEntry(e sqlcgen.BibleEntry) BibleEntry {
	out := BibleEntry{Id: e.ID, ProjectId: e.ProjectID, Section: BibleSection(e.Section), Title: e.Title, Fields: decodeFields(e.Fields), Position: int(e.Position), CreatedAt: e.CreatedAt, UpdatedAt: e.UpdatedAt}
	if e.ChapterID.Valid {
		id := e.ChapterID.UUID
		out.ChapterId = &id
	}
	return out
}

// decodeFields tolerates non-string JSON values by stringifying them.
func decodeFields(raw []byte) map[string]string {
	out := map[string]string{}
	if len(raw) == 0 {
		return out
	}
	var generic map[string]any
	if err := json.Unmarshal(raw, &generic); err != nil {
		return out
	}
	for k, v := range generic {
		switch t := v.(type) {
		case string:
			out[k] = t
		case nil:
			out[k] = ""
		default:
			b, _ := json.Marshal(t)
			out[k] = string(b)
		}
	}
	return out
}

func encodeFields(f map[string]string) ([]byte, error) {
	if f == nil {
		f = map[string]string{}
	}
	return json.Marshal(f)
}

func toWriter(w sqlcgen.Writer) Writer {
	roles := make([]WriterRole, 0, len(w.Roles))
	for _, r := range w.Roles {
		roles = append(roles, WriterRole(r))
	}
	return Writer{Id: w.ID, Name: w.Name, Slug: w.Slug, ModelAlias: w.ModelAlias, SystemPrompt: w.SystemPrompt, Roles: roles, Enabled: w.Enabled, Temperature: w.Temperature, IsSystem: w.IsSystem, CreatedAt: w.CreatedAt, UpdatedAt: w.UpdatedAt}
}

func nullUUID(p *uuid.UUID) uuid.NullUUID {
	if p == nil {
		return uuid.NullUUID{}
	}
	return uuid.NullUUID{UUID: *p, Valid: true}
}

func stringOr(p *string, def string) string {
	if p == nil {
		return def
	}
	return *p
}

func ptr[T any](v T) *T { return &v }

func describe(kind string, n int) string { return fmt.Sprintf("%d %s", n, kind) }
