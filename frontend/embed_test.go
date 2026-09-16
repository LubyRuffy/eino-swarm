package frontend

import (
	"io/fs"
	"testing"
)

// Assets must track the embed, not the working tree: a clone with only the
// sentinel compiles, and a built bundle is what the binary serves.
func TestAssetsTracksTheEmbeddedIndex(t *testing.T) {
	_, err := fs.Stat(dist, "dist/index.html")
	got := Assets()
	if err != nil {
		if got != nil {
			t.Fatal("no index.html in the embed but Assets() returned a bundle")
		}
		return
	}
	if got == nil {
		t.Fatal("index.html is embedded but Assets() returned nil")
	}
	if _, err := fs.Stat(got, "index.html"); err != nil {
		t.Fatalf("Assets() did not root at dist/: %v", err)
	}
}
