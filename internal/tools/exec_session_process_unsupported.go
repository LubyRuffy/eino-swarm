//go:build !darwin && !linux

package tools

import (
	"context"
	"errors"
	"os/exec"
)

func managedExecSupported() bool { return false }

func startManagedExecProcess(context.Context, *exec.Cmd) (func() (int, error), error) {
	return nil, errors.New("managed command sessions are unavailable on this platform")
}
