// Package text holds pure text utilities: slugs, token estimates, scene
// splitting, quote anchoring and diffs. Nothing here touches the database.
package text

import (
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// Slugify turns a display name into a lower-case ASCII slug such as
// "garcia-marquez". Diacritics are stripped; anything that is not a letter or
// digit becomes a single hyphen. An empty result falls back to "writer".
func Slugify(name string) string {
	decomposed := norm.NFD.String(name)
	var b strings.Builder
	lastHyphen := true // suppress a leading hyphen
	for _, r := range decomposed {
		switch {
		case unicode.Is(unicode.Mn, r):
			continue // combining mark: drop it
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastHyphen = false
		case r >= 'A' && r <= 'Z':
			b.WriteRune(unicode.ToLower(r))
			lastHyphen = false
		default:
			if !lastHyphen {
				b.WriteByte('-')
				lastHyphen = true
			}
		}
	}
	s := strings.Trim(b.String(), "-")
	if s == "" {
		return "writer"
	}
	return s
}

// UniqueSlug returns base, or base-2, base-3, ... until exists reports false.
func UniqueSlug(base string, exists func(string) bool) string {
	if !exists(base) {
		return base
	}
	for i := 2; ; i++ {
		candidate := base + "-" + itoa(i)
		if !exists(candidate) {
			return candidate
		}
	}
}

// DefaultAlias applies the gateway alias convention "{APP_NAME}-{slug}".
func DefaultAlias(appName, slug string) string {
	return appName + "-" + slug
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var buf [20]byte
	pos := len(buf)
	for i > 0 {
		pos--
		buf[pos] = byte('0' + i%10)
		i /= 10
	}
	return string(buf[pos:])
}
