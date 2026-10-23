//go:build !darwin

package desktop

import (
	"context"
	"errors"

	"github.com/wailsapp/wails/v3/pkg/application"
)

func rendererRecoveryAvailable() bool                               { return false }
func executeRecoveryProbe(window application.Window, script string) { window.ExecJS(script) }
func captureRecoveryPixels(context.Context, application.Window) ([]byte, error) {
	return nil, errors.New("native renderer snapshot is unavailable on this platform")
}
