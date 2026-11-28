package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/hostrecovery"
	"github.com/LubyRuffy/eino-swarm/internal/lease"
)

// linuxSupervisorEngineCommand is a child-only command. The private launch
// envelope and kernel parent identity establish the recovery owner.
const linuxSupervisorEngineCommand = "_linux-supervised-engine"
const linuxSupervisorActivate = 'P'

// readLinuxSupervisedLaunch admits a process only while its original
// supervising parent is still alive. Merely providing command-line flags or
// setting an environment variable cannot grant supervised update authority.
func readLinuxSupervisedLaunch() (hostrecovery.Launch, error) {
	input, err := inheritedLinuxUpdatePipe(3)
	if err != nil {
		return hostrecovery.Launch{}, err
	}
	defer input.Close()
	var launch hostrecovery.Launch
	decoder := json.NewDecoder(io.LimitReader(input, 64<<10))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&launch); err != nil {
		return hostrecovery.Launch{}, fmt.Errorf("read supervised engine launch: %w", err)
	}
	if launch.ParentPID <= 1 || launch.ParentPID != os.Getppid() ||
		!filepath.IsAbs(launch.DataDir) || filepath.Clean(launch.DataDir) != launch.DataDir || launch.Nonce == "" ||
		(launch.Trial != nil && !launch.Protected) {
		return hostrecovery.Launch{}, errors.New("the supervised engine launch has no valid parent or data directory")
	}
	return launch, nil
}

type linuxSupervisorChild struct {
	process *os.Process
	build   lease.Build
	done    chan struct{}
	err     error // read only after done closes
	permit  *os.File
}

// startLinuxSupervisorChild runs the selected verified executable. Its
// caller holds a dedicated OS thread for the lifetime of the supervisor.
// Linux delivers Pdeathsig when the creating thread exits.
func startLinuxSupervisorChild(image hostrecovery.Image, address string, mock bool, launch hostrecovery.Launch, listener *net.TCPListener, output io.Writer) (*linuxSupervisorChild, error) {
	info, err := os.Lstat(image.Path)
	if err != nil {
		return nil, fmt.Errorf("inspect supervised engine: %w", err)
	}
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok || !ownedLinuxFile(info) || !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 ||
		info.Mode().Perm()&0o022 != 0 || info.Mode()&(os.ModeSetuid|os.ModeSetgid) != 0 {
		return nil, errors.New("the supervised engine executable is not a private runnable image")
	}

	reader, writer, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	defer writer.Close()
	args := []string{linuxSupervisorEngineCommand, "--data-dir", launch.DataDir, "--addr", address}
	if mock {
		args = append(args, "--mock")
	}
	cmd := exec.Command(image.Path, args...)
	cmd.ExtraFiles = []*os.File{reader}
	var permit *os.File
	if launch.Trial != nil {
		trialRead, trialWrite, err := os.Pipe()
		if err != nil {
			return nil, err
		}
		defer trialRead.Close()
		permit = trialWrite
		cmd.ExtraFiles = append(cmd.ExtraFiles, trialRead)
	}
	if listener != nil {
		socket, err := listener.File()
		if err != nil {
			if permit != nil {
				_ = permit.Close()
			}
			return nil, fmt.Errorf("duplicate stable listener: %w", err)
		}
		defer socket.Close()
		launch.ListenerFD = 3 + len(cmd.ExtraFiles)
		cmd.ExtraFiles = append(cmd.ExtraFiles, socket)
	}
	cmd.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL}
	cmd.Stdout, cmd.Stderr = output, output
	if err := cmd.Start(); err != nil {
		if permit != nil {
			_ = permit.Close()
		}
		return nil, fmt.Errorf("start supervised engine: %w", err)
	}
	child := &linuxSupervisorChild{
		process: cmd.Process, done: make(chan struct{}), permit: permit,
		build: lease.Build{Version: image.Version, Dev: uint64(stat.Dev), Ino: stat.Ino},
	}
	go func() {
		child.err = cmd.Wait()
		close(child.done)
	}()
	// Close our read end immediately. A candidate that refuses the command
	// may exit without ever reading; retaining this end would hide EPIPE.
	_ = reader.Close()
	launch.ParentPID = os.Getpid()
	if err := writer.SetWriteDeadline(time.Now().Add(5 * time.Second)); err != nil {
		return nil, errors.Join(err, child.stop())
	}
	if err := json.NewEncoder(writer).Encode(launch); err != nil {
		return nil, errors.Join(err, child.stop())
	}
	return child, nil
}

func (c *linuxSupervisorChild) serving(ctx context.Context, dataDir string, mock bool, phase string) bool {
	rec, err := lease.DiscoverContext(ctx, dataDir, mock)
	return err == nil && rec.PID == c.process.Pid && lease.SameEngine(rec, c.build) && rec.Startup == phase
}

// activate grants write access only after the supervisor has durably
// committed to the exact trial image. Closing without a byte only wakes the
// child so it can exit without running recovery or tools.
func (c *linuxSupervisorChild) activate() error {
	if c.permit == nil {
		return errors.New("the supervised trial has no activation channel")
	}
	permit := c.permit
	c.permit = nil
	defer permit.Close()
	if err := permit.SetWriteDeadline(time.Now().Add(5 * time.Second)); err != nil {
		return err
	}
	_, err := permit.Write([]byte{linuxSupervisorActivate})
	return err
}

// stop terminates only the process created by this supervisor, waits for
// exit and leaves any competing lease holder untouched. Waiting for the
// exact child is also what makes a subsequent data rollback admissible.
func (c *linuxSupervisorChild) stop() error {
	if c.permit != nil {
		_ = c.permit.Close()
		c.permit = nil
	}
	select {
	case <-c.done:
		return nil
	default:
	}
	if err := c.process.Signal(syscall.SIGTERM); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return fmt.Errorf("stop supervised engine: %w", err)
	}
	timer := time.NewTimer(shutdownGrace)
	defer timer.Stop()
	select {
	case <-c.done:
		return nil
	case <-timer.C:
	}
	if err := c.process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return fmt.Errorf("kill supervised engine: %w", err)
	}
	// Filesystems or drivers can temporarily leave SIGKILL pending. Refuse
	// to restore data while the owned writer still exists.
	wait := time.NewTimer(shutdownGrace)
	defer wait.Stop()
	select {
	case <-c.done:
		return nil
	case <-wait.C:
		return errors.New("supervised engine did not exit before rollback")
	}
}
