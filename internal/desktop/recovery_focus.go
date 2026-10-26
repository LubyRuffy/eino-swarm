//go:build darwin

package desktop

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"
)

// ErrRecoveryFocusIdentity refuses activation after the owned child identity changes.
var ErrRecoveryFocusIdentity = errors.New("desktop recovery child identity changed")

type recoverySuccessorBirth struct{ seconds, microseconds uint64 }

const (
	recoveryFocusPending = iota
	recoveryFocusRefused
	recoveryFocusRequested
	recoveryFocusIdentityChanged
)

// FocusRecoverySuccessor coordinates one activation of the caller's own child.
// Registration is observed within ctx; activation never acknowledges renderer health.
func FocusRecoverySuccessor(ctx context.Context, pid int, executable string) error {
	return focusRecoverySuccessor(ctx, pid, executable, captureRecoverySuccessor, requestRecoverySuccessorActivation)
}

func focusRecoverySuccessor(ctx context.Context, pid int, executable string,
	capture func(int, string) (recoverySuccessorBirth, error),
	request func(context.Context, int, string, recoverySuccessorBirth) (int, error),
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if pid <= 1 || !filepath.IsAbs(executable) {
		return ErrRecoveryFocusIdentity
	}
	birth, err := capture(pid, executable)
	if err != nil {
		return err
	}
	slog.Info("desktop recovery focus", "shell_pid", os.Getpid(), "child_pid", pid, "stage", "parent_waiting")
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		status, err := request(ctx, pid, executable, birth)
		if err != nil {
			return err
		}
		switch status {
		case recoveryFocusRefused, recoveryFocusRequested:
			return nil
		case recoveryFocusIdentityChanged:
			return ErrRecoveryFocusIdentity
		case recoveryFocusPending:
			select {
			case <-ctx.Done():
				slog.Info("desktop recovery focus", "shell_pid", os.Getpid(), "child_pid", pid, "stage", "parent_cancelled")
				return ctx.Err()
			case <-ticker.C:
			}
		default:
			return fmt.Errorf("%w: native observation status %d", ErrRecoveryFocusIdentity, status)
		}
	}
}
