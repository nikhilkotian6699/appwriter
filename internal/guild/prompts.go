// Package guild implements the workflows: critique, synthesis, revision,
// bible upkeep, co-writing, comparison and the writer test.
package guild

import "strings"

// FixedSuffix is appended to every writer's system prompt and cannot be edited.
const FixedSuffix = `

## Guild rules (fixed, always apply)
- Produce original text only. Never reproduce, quote at length or closely paraphrase passages from any published work, including the works of the author whose craft you emulate. Evoke a sensibility; do not copy sentences.
- Always follow the required output format exactly. When JSON is requested, return only valid JSON with no commentary before or after it.`

// SystemPrompt combines the user's prompt with the fixed suffix.
func SystemPrompt(userPrompt string) string {
	return strings.TrimSpace(userPrompt) + FixedSuffix
}
