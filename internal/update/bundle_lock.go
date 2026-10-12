package update

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/LubyRuffy/eino-swarm/internal/lease"
)

// The bundle is shared by engines using different data directories. Its lock
// must live beside it, so replacing the bundle does not replace the lock inode.
// Resolve parent and bundle aliases before selecting that stable lock location.
func lockBundle(app string) (string, *lease.Hold, error) {
	app, err := bundlePath(app)
	if err != nil {
		return "", nil, err
	}
	hold, err := lease.Acquire(app + ".update-lock")
	if errors.Is(err, lease.ErrHeld) {
		return "", nil, ErrBusy
	}
	return app, hold, err
}

func bundlePath(app string) (string, error) {
	app, err := filepath.Abs(app)
	if err != nil {
		return "", err
	}
	// Resolve bundle symlinks even while their final target is temporarily
	// absent during swap. EvalSymlinks on the whole path cannot do that.
	for links := 0; links < 40; links++ {
		parent, err := filepath.EvalSymlinks(filepath.Dir(app))
		if err != nil {
			return "", err
		}
		app = filepath.Join(parent, filepath.Base(app))
		info, err := os.Lstat(app)
		if errors.Is(err, os.ErrNotExist) || (err == nil && info.Mode()&os.ModeSymlink == 0) {
			return app, nil
		}
		if err != nil {
			return "", err
		}
		target, err := os.Readlink(app)
		if err != nil {
			return "", err
		}
		if !filepath.IsAbs(target) {
			target = filepath.Join(parent, target)
		}
		app = target
	}
	return "", fmt.Errorf("desktop update: too many bundle symlinks: %w", os.ErrInvalid)
}
