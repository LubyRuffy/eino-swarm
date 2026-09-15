// Package frontend embeds the built web UI so a single binary serves it, and
// so `go run ./cmd/zwai desktop` works on a fresh clone without a Node
// toolchain. That is why dist/ is committed: run `make frontend` after
// changing anything under src/.
package frontend

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var dist embed.FS

// Assets returns the built bundle rooted at dist/, or nil when this build has
// no bundle. A nil result leaves the API working and logs a warning, which is
// what keeps API-only tests and `zwai trace` usable.
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
