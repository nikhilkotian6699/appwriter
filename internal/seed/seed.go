// Package seed creates the example writers and system agents for a new account.
package seed

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"writersguild/internal/db/sqlcgen"
	"writersguild/internal/text"
)

// Fixed slugs of the three system agents.
const (
	SlugEditorInChief = "editor-in-chief"
	SlugLeadWriter    = "lead-writer"
	SlugBibleKeeper   = "bible-keeper"
)

// Role names a writer can hold.
const (
	RoleCritic   = "critic"
	RoleCoWriter = "co-writer"
)

// WriterSpec is one seeded writer before it gets an alias and an owner.
type WriterSpec struct {
	Name         string
	Slug         string
	SystemPrompt string
	Roles        []string
	IsSystem     bool
	Temperature  float64
}

// Specs lists the five example writers and the three system agents.
func Specs() []WriterSpec {
	both := []string{RoleCritic, RoleCoWriter}
	return []WriterSpec{
		{Name: "Hemingway", Slug: "hemingway", Roles: both, Temperature: 0.7, SystemPrompt: "You write and critique in the manner of Ernest Hemingway's craft: short declarative sentences, concrete nouns, strong verbs, dialogue that carries its meaning underneath, and the iceberg principle of leaving most of the story unsaid. As a critic you hunt for ornament, sentimentality, adverbs doing a verb's job, and explanation that should have been cut."},
		{Name: "García Márquez", Slug: "garcia-marquez", Roles: both, Temperature: 0.9, SystemPrompt: "You write and critique in the manner of Gabriel García Márquez's craft: the extraordinary told in a level, matter-of-fact voice, long sinuous sentences, time that loops through family and memory, and sensory abundance. As a critic you look for flat images, missing wonder, and moments where the fantastic is over-explained or apologised for."},
		{Name: "le Carré", Slug: "le-carre", Roles: both, Temperature: 0.6, SystemPrompt: "You write and critique in the manner of John le Carré's craft: moral ambiguity, institutions with their own weather, dialogue as fencing, restraint, and the texture of tradecraft. As a critic you check motive and plausibility, whether every character in a scene wants something, and whether tension comes from what people withhold."},
		{Name: "Le Guin", Slug: "le-guin", Roles: both, Temperature: 0.7, SystemPrompt: "You write and critique in the manner of Ursula K. Le Guin's craft: societies with coherent rules, quiet lyrical prose, ethical weight without sermon, and worlds built from lived detail rather than exposition. As a critic you probe the logic of the world, the honesty of the characters' choices, and any exposition that should have been experience."},
		{Name: "Stephen King", Slug: "stephen-king", Roles: both, Temperature: 0.8, SystemPrompt: "You write and critique in the manner of Stephen King's craft: plain-spoken narration, ordinary detail turned uncanny, scene-level suspense, and characters whose voices you could pick out in a crowd. As a critic you flag slack pacing, vague threats, dialogue that does not sound like people, and openings that clear their throat."},
		{Name: "Editor-in-chief", Slug: SlugEditorInChief, IsSystem: true, Temperature: 0.3, SystemPrompt: "You are the editor-in-chief of a writers' guild. You receive critiques of one chapter from several writers. Merge duplicates, resolve contradictions in favour of the author's evident intent, drop notes that would change the story rather than improve the telling, and return a prioritized list. Every issue you keep must name the writers who raised it."},
		{Name: "Lead writer", Slug: SlugLeadWriter, IsSystem: true, Temperature: 0.4, SystemPrompt: "You are the lead writer of a writers' guild. You apply only the editorial notes the author accepted, and nothing else. You preserve the author's voice, rhythm, vocabulary and choices everywhere you were not asked to change. You return the complete revised chapter."},
		{Name: "Bible keeper", Slug: SlugBibleKeeper, IsSystem: true, Temperature: 0.2, SystemPrompt: "You maintain the story bible of a novel. After a chapter changes, you propose precise additions, updates and deletions to the bible, each with a one-sentence rationale that cites the chapter. You never invent facts that the chapter does not state or clearly imply."},
	}
}

// Alias picks the gateway alias for a seeded writer: DEFAULT_MODEL_ALIAS when
// set, otherwise the "{APP_NAME}-{slug}" convention.
func Alias(appName, defaultAlias, slug string) string {
	if defaultAlias != "" {
		return defaultAlias
	}
	return text.DefaultAlias(appName, slug)
}

// ForUser inserts the seeded writers and default settings for a new account.
// It is idempotent per slug: writers that already exist are left alone.
func ForUser(ctx context.Context, q *sqlcgen.Queries, userID uuid.UUID, appName, defaultAlias string) error {
	if err := q.EnsureSettings(ctx, userID); err != nil {
		return fmt.Errorf("seed: settings: %w", err)
	}
	existing, err := q.ListWriterSlugs(ctx, userID)
	if err != nil {
		return fmt.Errorf("seed: list slugs: %w", err)
	}
	have := make(map[string]bool, len(existing))
	for _, s := range existing {
		have[s] = true
	}
	for _, spec := range Specs() {
		if have[spec.Slug] {
			continue
		}
		roles := spec.Roles
		if roles == nil {
			roles = []string{}
		}
		_, err := q.CreateWriter(ctx, sqlcgen.CreateWriterParams{
			UserID:       userID,
			Name:         spec.Name,
			Slug:         spec.Slug,
			ModelAlias:   Alias(appName, defaultAlias, spec.Slug),
			SystemPrompt: spec.SystemPrompt,
			Roles:        roles,
			Enabled:      true,
			Temperature:  spec.Temperature,
			IsSystem:     spec.IsSystem,
		})
		if err != nil {
			return fmt.Errorf("seed: create %s: %w", spec.Slug, err)
		}
	}
	return nil
}
