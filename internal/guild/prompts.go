// Package guild implements the workflows: critique, synthesis, revision,
// bible upkeep, co-writing, comparison and the writer test.
package guild

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"writersguild/internal/db/sqlcgen"
)

// FixedSuffix is appended to every writer's system prompt and cannot be edited.
const FixedSuffix = `

## Guild rules (fixed, always apply)
- Produce original text only. Never reproduce, quote at length or closely paraphrase passages from any published work, including the works of the author whose craft you emulate. Evoke a sensibility; do not copy sentences.
- Always follow the required output format exactly. When JSON is requested, return only valid JSON with no commentary before or after it.`

// SystemPrompt combines the user's prompt with the fixed suffix.
func SystemPrompt(userPrompt string) string {
	return strings.TrimSpace(userPrompt) + FixedSuffix
}

// Section titles of the story bible, in reading order.
var bibleSections = []struct{ key, title string }{
	{"premise", "Premise"},
	{"character", "Characters"},
	{"setting", "Setting and world rules"},
	{"timeline", "Timeline"},
	{"style", "Tone and style rules"},
	{"chapter_summary", "Chapter summaries"},
}

// Field order inside a bible entry; anything else follows alphabetically.
var bibleFieldOrder = []string{"text", "role", "voice", "arc", "facts", "key_facts", "notes"}

// BibleContext renders the story bible as Markdown for a prompt. Empty
// sections are skipped; an empty bible yields a short note instead.
func BibleContext(entries []sqlcgen.BibleEntry) string {
	bySection := map[string][]sqlcgen.BibleEntry{}
	for _, e := range entries {
		bySection[e.Section] = append(bySection[e.Section], e)
	}
	var b strings.Builder
	for _, sec := range bibleSections {
		list := bySection[sec.key]
		if len(list) == 0 {
			continue
		}
		fmt.Fprintf(&b, "## %s\n", sec.title)
		for _, e := range list {
			title := strings.TrimSpace(e.Title)
			fields := orderedFields(e.Fields)
			if title == "" && len(fields) == 0 {
				continue
			}
			if title != "" {
				fmt.Fprintf(&b, "- **%s**", title)
			} else {
				b.WriteString("-")
			}
			for i, f := range fields {
				if i == 0 && title != "" {
					b.WriteString(": ")
				} else if i > 0 || title == "" {
					b.WriteString(" ")
				}
				if f.key == "text" && len(fields) == 1 {
					b.WriteString(f.value)
				} else {
					fmt.Fprintf(&b, "%s: %s.", humanKey(f.key), strings.TrimSuffix(f.value, "."))
				}
			}
			b.WriteString("\n")
		}
		b.WriteString("\n")
	}
	if b.Len() == 0 {
		return "(The story bible is still empty.)\n"
	}
	return b.String()
}

type bibleField struct{ key, value string }

func orderedFields(raw []byte) []bibleField {
	var generic map[string]any
	if len(raw) == 0 || json.Unmarshal(raw, &generic) != nil {
		return nil
	}
	values := map[string]string{}
	for k, v := range generic {
		var s string
		switch t := v.(type) {
		case string:
			s = strings.TrimSpace(t)
		case nil:
			s = ""
		default:
			bs, _ := json.Marshal(t)
			s = string(bs)
		}
		if s != "" {
			values[k] = s
		}
	}
	var out []bibleField
	for _, k := range bibleFieldOrder {
		if v, ok := values[k]; ok {
			out = append(out, bibleField{k, v})
			delete(values, k)
		}
	}
	rest := make([]string, 0, len(values))
	for k := range values {
		rest = append(rest, k)
	}
	sort.Strings(rest)
	for _, k := range rest {
		out = append(out, bibleField{k, values[k]})
	}
	return out
}

func humanKey(k string) string {
	k = strings.ReplaceAll(k, "_", " ")
	if k == "" {
		return k
	}
	return strings.ToUpper(k[:1]) + k[1:]
}

// Chapter text markers. The fake gateway uses them to find the chapter and
// they make the boundary unmistakable for a real model.
const (
	ChapterBegin = "--- CHAPTER TEXT BEGIN ---"
	ChapterEnd   = "--- CHAPTER TEXT END ---"
)

const criticRole = `

## Your task in this session
You attend the Writers' Guild as a critic. You will receive the story bible of the novel and the text of one chapter (or one scene of it). Read as the author described you would. Find the few things that matter most, quote the exact passage each one concerns, say what the problem is and propose a concrete fix in the author's own voice. Flag any passage that contradicts the story bible. Be specific and brief.

## Required output
` + CritiqueFormat

// CriticSystemPrompt is the writer's own prompt plus the fixed suffix and the
// critic's task and output contract.
func CriticSystemPrompt(writerPrompt string) string {
	return SystemPrompt(writerPrompt) + criticRole
}

// CriticUserPrompt assembles the bible, the chapter or scene and the request.
func CriticUserPrompt(projectName, chapterTitle string, bible string, sceneIndex, sceneCount int, sceneTitle, sceneText string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Story bible of \"%s\"\n\n%s\n", projectName, bible)
	fmt.Fprintf(&b, "# Chapter: %s", chapterTitle)
	if sceneCount > 1 {
		fmt.Fprintf(&b, " — scene %d of %d", sceneIndex+1, sceneCount)
		if sceneTitle != "" {
			fmt.Fprintf(&b, " (%s)", sceneTitle)
		}
		b.WriteString("\nThe chapter was split into scenes because of its length; critique only the scene below, quoting from it alone.")
	}
	b.WriteString("\n\n")
	b.WriteString(ChapterBegin)
	b.WriteString("\n")
	b.WriteString(sceneText)
	if !strings.HasSuffix(sceneText, "\n") {
		b.WriteString("\n")
	}
	b.WriteString(ChapterEnd)
	b.WriteString("\n\nCritique this text now. Return the JSON object only.")
	return b.String()
}
