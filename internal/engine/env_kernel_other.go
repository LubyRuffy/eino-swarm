//go:build !darwin

package engine

import (
	"context"
	"os/exec"
)

func platformUname(ctx context.Context) ([]byte, error) {
	return exec.CommandContext(ctx, "uname", "-s", "-r").Output()
}
