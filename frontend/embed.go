// Package frontend embeds the built web UI so a single binary serves it.
//
// dist/ is a Vite artefact and is not committed. A sentinel file keeps the
// directory in git so `go:embed all:dist` still compiles on a clone that has
// not built the UI yet. `go run ./cmd/zwai desktop` (and `web`) call Load,
// which builds dist/ from the checkout when the sources changed, then serves
// that directory — go:embed is a compile-time snapshot and would otherwise
// stay empty until the next `go build`. An installed binary with no checkout
// serves the embed; Assets() is nil until index.html exists, which leaves
// the API working for `zwai trace` and the handler tests.
package frontend

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var dist embed.FS

//go:generate go run generate.go

// Assets returns the bundle compiled into this binary, rooted at dist/, or
// nil when this build has no index.html.
func Assets() fs.FS {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		return nil
	}
	if _, err := fs.Stat(sub, "index.html"); err != nil {
		return nil
	}
	return sub
}
