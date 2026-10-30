package api

import (
	"embed"
	"io/fs"
)

//go:embed web
var webEmbed embed.FS

// WebFS returns the embedded dashboard (no build step, no CDN —
// the UI works fully offline, which matters on a home server).
func WebFS() fs.FS {
	sub, err := fs.Sub(webEmbed, "web")
	if err != nil {
		panic(err) // impossible: path is compiled in
	}
	return sub
}
