//go:build darwin || linux

package tools

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"syscall"
	"time"
)

const (
	managedExecProcessPoll = 20 * time.Millisecond
	managedExecGroupWait   = 2 * time.Second
	managedExecPipeWait    = 250 * time.Millisecond
	managedExecStopProbe   = time.Second
	managedExecStopGrace   = 30 * time.Second
)

func managedExecSupported() bool { return true }

func startManagedExecProcess(ctx context.Context, cmd *exec.Cmd) (func() (int, error), error) {
	return startManagedExecProcessWithStopGrace(ctx, cmd, managedExecStopGrace)
}

func startManagedExecProcessWithStopGrace(ctx context.Context, cmd *exec.Cmd, stopGrace time.Duration) (func() (int, error), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.WaitDelay = managedExecPipeWait
	if err := cmd.Start(); err != nil {
		return nil, err
	}

	done := make(chan struct{})
	var exitCode int
	var resultErr error
	go func() {
		defer close(done)
		pid := cmd.Process.Pid
		// Observe exit without reaping. The leader's PID pins the owned
		// process group identity until the last group signal is finished.
		observeErr := waitManagedExecLeader(ctx, pid, stopGrace)
		groupErr := stopManagedExecGroup(pid, managedExecGroupLive)
		if groupErr != nil {
			groupErr = fmt.Errorf("%w: %w", errManagedExecCleanup, groupErr)
			// Still reclaim the directly owned child if group cleanup
			// could not be proved.
			if err := cmd.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
				groupErr = errors.Join(groupErr, err)
			}
		}
		waitErr := cmd.Wait()
		exitCode = -1
		if state := cmd.ProcessState; state != nil {
			exitCode = state.ExitCode()
		} else {
			waitErr = errors.Join(errManagedExecCleanup, waitErr)
		}
		if errors.Is(waitErr, exec.ErrWaitDelay) {
			waitErr = errors.Join(errManagedExecCleanup, waitErr)
		}
		resultErr = errors.Join(observeErr, groupErr, waitErr)
	}()
	return func() (int, error) {
		<-done
		return exitCode, resultErr
	}, nil
}

type managedExecProcessBirth struct {
	secondsOrTicks int64
	microseconds   int64
}

type managedExecStopEvidence struct {
	group       map[int]managedExecProcessBirth
	first, last time.Time
}

func (e *managedExecStopEvidence) observe(group map[int]managedExecProcessBirth, now time.Time, probe, grace time.Duration) bool {
	if len(group) == 0 {
		*e = managedExecStopEvidence{}
		return false
	}
	if now.Sub(e.last) > 2*probe || !maps.Equal(e.group, group) {
		*e = managedExecStopEvidence{group: group, first: now, last: now}
		return false
	}
	e.last = now
	return now.Sub(e.first) >= grace
}

func waitManagedExecLeader(ctx context.Context, pid int, stopGrace time.Duration) error {
	ticker := time.NewTicker(managedExecProcessPoll)
	defer ticker.Stop()
	probe := min(managedExecStopProbe, stopGrace/4)
	watchdog := time.NewTicker(probe)
	defer watchdog.Stop()
	var stopped managedExecStopEvidence
	for {
		exited, err := managedExecLeaderExited(pid)
		if err != nil {
			return fmt.Errorf("observe command exit: %w", err)
		}
		if exited {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		case <-watchdog.C:
			group, err := managedExecStoppedGroup(pid)
			if err != nil {
				stopped = managedExecStopEvidence{}
				continue
			}
			now := time.Now()
			if !stopped.observe(group, now, probe, stopGrace) {
				continue
			}
			// This is an OS-confirmed stop, not lack of output or CPU.
			// Revalidate ownership and every member before the group kill.
			confirmed, err := managedExecStoppedGroup(pid)
			if err != nil || !maps.Equal(group, confirmed) {
				stopped = managedExecStopEvidence{}
				continue
			}
			if err := ctx.Err(); err != nil {
				return err
			}
			return fmt.Errorf("%w: %d live members observed stopped for %s",
				errManagedExecStalled, len(group), now.Sub(stopped.first).Round(time.Millisecond))
		}
	}
}

func stopManagedExecGroup(pgid int, groupLive func(int) (bool, error)) error {
	deadline := time.NewTimer(managedExecGroupWait)
	defer deadline.Stop()
	ticker := time.NewTicker(managedExecProcessPoll)
	defer ticker.Stop()
	for {
		// Signal the owned kernel group before trusting an inventory. A
		// descendant can fork and exit while a process scan is in flight.
		signalErr := syscall.Kill(-pgid, syscall.SIGKILL)
		if errors.Is(signalErr, syscall.ESRCH) {
			return nil
		}
		live, probeErr := groupLive(pgid)
		if probeErr != nil {
			return fmt.Errorf("verify command process group exit: %w", errors.Join(probeErr, signalErr))
		}
		// Darwin can return EPERM for a zombie-only group. A successful
		// inventory can confirm that this group has no live processes.
		if !live {
			return nil
		}
		if signalErr != nil && !errors.Is(signalErr, syscall.EPERM) {
			return fmt.Errorf("stop command process group: %w", signalErr)
		}
		select {
		case <-deadline.C:
			return errors.Join(errors.New("command process group exit could not be confirmed"), signalErr)
		case <-ticker.C:
		}
	}
}
