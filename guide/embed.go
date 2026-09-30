// Package guide embeds the built user guide (guide/dist), served under /guide/.
package guide

import "embed"

// Dist holds the guide's static files.
//
//go:embed all:dist
var Dist embed.FS
