package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/lease"
)

// Cold startup can include a frontend rebuild and persisted-state recovery.
// Replacement passes its existing 30-second context into the same wait, so
// the longer cold-start allowance never extends a restart's shared budget.
var engineBootWait = 2 * time.Minute
var engineBootHeartbeat = 10 * time.Second

// discoverIfStarting waits when another process already holds the lock but
// has not published a URL yet. The bool is true when this shell must inspect
// that holder's identity or report its startup failure instead of starting.
func discoverIfStartingContext(parent context.Context, dir string, mock bool) (lease.Record, bool, error) {
	if !lease.Busy(dir) {
		return lease.Record{}, false, nil
	}
	ctx, cancel := context.WithTimeout(parent, engineBootWait)
	defer cancel()
	started := time.Now()
	poll := time.NewTicker(50 * time.Millisecond)
	defer poll.Stop()
	progress := time.NewTicker(engineBootHeartbeat)
	defer progress.Stop()
	for {
		if err := ctx.Err(); err != nil {
			return lease.Record{}, true, fmt.Errorf("engine holds the data directory but is not listening: %w", err)
		}
		rec, err := lease.Discover(dir, mock)
		if err == nil && ctx.Err() == nil {
			return rec, true, nil
		}
		if errors.Is(err, lease.ErrMockMismatch) || errors.Is(err, lease.ErrUnreachable) {
			return lease.Record{}, true, err
		}
		if !lease.Busy(dir) {
			return lease.Record{}, false, nil
		}
		select {
		case <-ctx.Done():
		case <-poll.C:
		case <-progress.C:
			log.Printf("engine startup: still initializing after %s", time.Since(started).Round(time.Second))
		}
	}
}

func spawnEngine(dir, addr string, mock bool) (string, error) {
	return spawnEngineContext(context.Background(), dir, addr, mock)
}

func spawnEngineContext(parent context.Context, dir, addr string, mock bool) (string, error) {
	if err := parent.Err(); err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(parent, engineBootWait)
	defer cancel()
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	args := []string{"engine", "--data-dir", dir}
	if addr != "" {
		args = append(args, "--addr", addr)
	}
	if mock {
		args = append(args, "--mock")
	}
	cmd := exec.Command(exe, args...)
	detach(cmd)
	logPath := filepath.Join(dir, "engine.log")
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return "", err
	}
	offset, err := f.Seek(0, io.SeekEnd)
	if err != nil {
		_ = f.Close()
		return "", err
	}
	cmd.Stdout, cmd.Stderr = f, f
	if err := cmd.Start(); err != nil {
		_ = f.Close()
		return "", err
	}
	_ = f.Close()
	exited := make(chan struct{})
	var waitErr error
	go func() {
		waitErr = cmd.Wait()
		close(exited)
	}()
	keepChild := false
	defer func() {
		if !keepChild {
			// The PID belongs to the child this call launched. A competing
			// lease holder is never a cancellation or timeout cleanup target.
			_ = cmd.Process.Kill()
			<-exited
		}
	}()
	started := time.Now()
	poll := time.NewTicker(50 * time.Millisecond)
	defer poll.Stop()
	progress := time.NewTicker(engineBootHeartbeat)
	defer progress.Stop()
	childExited := false
	for {
		if err := ctx.Err(); err != nil {
			select {
			case <-exited:
				err = errors.Join(err, waitErr)
			default:
			}
			return "", fmt.Errorf("engine startup did not finish: %w\n%s", err, engineLogTail(logPath, offset))
		}
		if !childExited {
			select {
			case <-exited:
				childExited = true
			default:
			}
		}
		rec, err := lease.Discover(dir, mock)
		if err == nil && ctx.Err() == nil {
			keepChild = rec.PID == cmd.Process.Pid
			return rec.URL, nil
		}
		if errors.Is(err, lease.ErrMockMismatch) || errors.Is(err, lease.ErrUnreachable) {
			return "", err
		}
		if childExited && !lease.Busy(dir) {
			if waitErr == nil {
				waitErr = errors.New("child exited successfully before readiness")
			}
			return "", fmt.Errorf("engine exited before it was listening: %w\n%s", waitErr, engineLogTail(logPath, offset))
		}
		select {
		case <-ctx.Done():
		case <-poll.C:
		case <-progress.C:
			log.Printf("engine startup: still initializing after %s", time.Since(started).Round(time.Second))
		}
	}
}

// Read only this launch's output, bounded even when the shared log is large.
func engineLogTail(path string, offset int64) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return ""
	}
	if start := info.Size() - 2000; start > offset {
		offset = start
	}
	tail, _ := io.ReadAll(io.NewSectionReader(f, offset, 2000))
	return string(tail)
}
