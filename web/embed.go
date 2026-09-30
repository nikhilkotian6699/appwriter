// Package web embeds the built single-page app (web/dist).
package web

import "embed"

// Dist holds the Vite build output. Run `npm run build` in web/ first; the
// Docker build does this automatically.
//
//go:embed all:dist
var Dist embed.FS
