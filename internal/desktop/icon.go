package desktop

import (
	"sync"

	_ "embed"
)

// appIcon is the square source mark. A naked `go run` binary has no
// .app bundle, so macOS would otherwise show the generic Unix-exec glyph
// unless Wails is given PNG bytes for NSApp setApplicationIconImage.
//
//go:embed appicon.png
var appIcon []byte

var (
	dockIconOnce  sync.Once
	dockIconBytes []byte
)

// dockIcon is the mark Wails actually receives: the source PNG inset to
// Apple's 824/1024 icon grid and given rounded corners.
// setApplicationIconImage does not apply the Dock squircle or that
// margin, so a square full-bleed PNG shows as a large square.
func dockIcon() []byte {
	dockIconOnce.Do(func() {
		dockIconBytes = loadDockIcon(appIcon)
	})
	return dockIconBytes
}

func loadDockIcon(src []byte) []byte {
	rounded, err := roundPNG(src)
	if err != nil {
		return src
	}
	return rounded
}
