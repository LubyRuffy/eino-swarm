//go:build unix

package frontend

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"time"
)

var signalBuildGroup = syscall.Kill

func runBuildCommand(ctx context.Context, cmd *exec.Cmd) error {
	// npm starts a shell and Node descendants. Killing just npm would release
	// the checkout lock while those children still write into node_modules/dist.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	// Bound inherited stdout/stderr pipes after npm exits; these are not
	// evidence of live processes. Group state is checked separately below.
	cmd.WaitDelay = 100 * time.Millisecond
	cmd.Cancel = func() error {
		err := signalBuildGroup(-cmd.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
	if err := cmd.Start(); err != nil {
		return errors.Join(err, ctx.Err())
	}
	err := cmd.Wait()
	if errors.Is(err, exec.ErrWaitDelay) {
		err = nil // npm exited successfully; remaining writers are stopped below.
	}
	// A failed script may also leave background children after npm exits.
	// Retain the build lock until every member of this owned group has exited.
	killErr := signalBuildGroup(-cmd.Process.Pid, syscall.SIGKILL)
	if killErr != nil && !errors.Is(killErr, syscall.ESRCH) {
		return errors.Join(err, ctx.Err(), fmt.Errorf("stop frontend build processes: %w", killErr))
	}
	for {
		active, probeErr := buildGroupActive(cmd.Process.Pid)
		if probeErr != nil {
			return errors.Join(err, ctx.Err(), fmt.Errorf("wait for frontend build processes: %w", probeErr))
		}
		if !active {
			return errors.Join(err, ctx.Err())
		}
		time.Sleep(10 * time.Millisecond)
	}
}
