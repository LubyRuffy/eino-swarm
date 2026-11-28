package main

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/app"
	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/hostrecovery"
	"github.com/LubyRuffy/eino-swarm/internal/lease"
	"github.com/LubyRuffy/eino-swarm/internal/update"
	"golang.org/x/sys/unix"
)

func runLinuxSupervisedEngine(args []string) error {
	launch, err := readLinuxSupervisedLaunch()
	if err != nil {
		return err
	}
	listener, err := supervisedLinuxListener(launch)
	if err != nil {
		return err
	}
	defer listener.Close()
	options := &engineSupervisor{
		trial:    launch.Trial != nil,
		listener: listener,
		configure: func(ctx context.Context, a *app.App, _ lease.Record, stop context.CancelFunc) error {
			if launch.DataDir != a.Config.DataDir() {
				return errors.New("the supervised engine opened another data directory")
			}
			configureSupervisedUpdate(ctx, a, launch, stop)
			return nil
		},
	}
	if launch.Trial != nil {
		gate, err := inheritedLinuxUpdatePipe(4)
		if err != nil {
			return err
		}
		defer gate.Close()
		options.waitPromotion = func(ctx context.Context) error {
			stop := context.AfterFunc(ctx, func() { _ = gate.Close() })
			defer stop()
			var token [1]byte
			if _, err := io.ReadFull(gate, token[:]); err != nil || token[0] != linuxSupervisorActivate {
				return errors.Join(errors.New("candidate never received durable promotion"), err)
			}
			return ctx.Err()
		}
	}
	return runEngineWithSupervisor(args, options)
}

func supervisedLinuxListener(launch hostrecovery.Launch) (net.Listener, error) {
	fd := 4
	if launch.Trial != nil {
		fd = 5
	}
	if fd != launch.ListenerFD {
		return nil, errors.New("the supervisor did not pass a listening socket")
	}
	unix.CloseOnExec(fd)
	file := os.NewFile(uintptr(fd), "host listening socket")
	defer file.Close()
	listener, err := net.FileListener(file)
	if err != nil {
		return nil, fmt.Errorf("read inherited host socket: %w", err)
	}
	if _, ok := listener.(*net.TCPListener); !ok {
		_ = listener.Close()
		return nil, errors.New("the inherited host socket is not TCP")
	}
	return listener, nil
}

func configureSupervisedUpdate(ctx context.Context, a *app.App, launch hostrecovery.Launch, stop context.CancelFunc) {
	dir := hostrecovery.StateDir(launch.DataDir)
	feed := newLinuxUpdateFeed()
	supported := launch.Protected && linuxArchiveBuild == "1"
	if supported {
		if err := hostrecovery.CheckDataSupport(launch.DataDir); err != nil {
			supported = false
			a.Logger.Warn("Linux host update needs a supported data filesystem", "err", err)
		}
	}
	a.ConfigureHostUpdate(ctx, version, update.HostUpdateOptions{
		Protected: true,
		Eligible: func() bool {
			if !supported || a.Engine.RequireReady() != nil {
				return false
			}
			journal, err := hostrecovery.LoadJournal(dir)
			return err == nil && journal.Preparing == nil && journal.Completed == ""
		},
		Check: feed.Check,
		Install: func(ctx context.Context, job *update.HostJob) error {
			return queueSupervisedUpdate(ctx, a, stop, dir, launch, feed, job)
		},
	})
	if launch.Trial != nil {
		a.BeginHostUpdateTrial(verifySupervisedPhone(dir, launch))
	}
}

func verifySupervisedPhone(dir string, launch hostrecovery.Launch) func(string, string, string) (string, error) {
	challenge := hostrecovery.Nonce()
	return func(operation, echo, peer string) (string, error) {
		if operation != launch.Trial.OperationID || peer != launch.Trial.Peer {
			return "", update.ErrHostUpdateUnauthorized
		}
		if subtle.ConstantTimeCompare([]byte(echo), []byte(challenge)) != 1 {
			return challenge, nil
		}
		return "", hostrecovery.SaveProof(dir, hostrecovery.Proof{OperationID: operation, Nonce: launch.Nonce, Peer: peer})
	}
}

func queueSupervisedUpdate(ctx context.Context, a *app.App, stop context.CancelFunc, dir string, launch hostrecovery.Launch, feed *update.LinuxFeed, job *update.HostJob) error {
	operation := job.Snapshot()
	stage, err := os.MkdirTemp(dir, "image-")
	if err != nil {
		return err
	}
	queued := false
	defer func() {
		if !queued {
			_ = os.RemoveAll(stage)
		}
	}()
	if err := job.SetStage("downloading"); err != nil {
		return err
	}
	file, err := feed.Stage(ctx, operation.Version, stage, job.ReportDownload)
	if err != nil {
		return err
	}
	image, err := hostrecovery.ImageAt(file, operation.Version)
	if err != nil {
		return err
	}
	if err := syncLinuxDir(stage); err != nil {
		return err
	}
	if err := syncLinuxDir(dir); err != nil {
		return err
	}
	if err := verifyBootOwner(ctx, launch.Unit, launch.ParentPID); err != nil {
		return err
	}
	if err := hostrecovery.CheckDataSupport(launch.DataDir); err != nil {
		return err
	}
	if err := job.SetStage("saving"); err != nil {
		return err
	}
	handoff, cancel := context.WithTimeout(ctx, handoffBudget-time.Second)
	defer cancel()
	status, err := a.Engine.PrepareRestart(handoff)
	if err != nil || !status.Ready {
		return errors.Join(errors.New("host update handoff could not be confirmed"), err)
	}
	// Even if saving submission fails, the old engine is durably frozen.
	// Its stable owner can start the same image without losing its checkpoint.
	defer stop()
	if err := job.SetStage("restarting"); err != nil {
		return err
	}
	if err := hostrecovery.SaveRequest(dir, hostrecovery.Request{
		OperationID: operation.OperationID, Peer: job.Peer(), Nonce: launch.Nonce, Candidate: image,
	}); err != nil {
		return err
	}
	queued = true
	return nil
}

func verifiedBootUnit(parent context.Context, unit string) error {
	return verifyBootOwner(parent, unit, os.Getpid())
}

func verifyBootOwner(parent context.Context, unit string, pid int) error {
	if unit == "" || unit[0] == '-' || filepath.Base(unit) != unit || !strings.HasSuffix(unit, ".service") {
		return errors.New("the protected host needs an enabled systemd system service")
	}
	invocation := os.Getenv("INVOCATION_ID")
	if len(invocation) != 32 {
		return errors.New("the supervisor was not started by its systemd service")
	}
	ctx, cancel := context.WithTimeout(parent, 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "systemctl", "--system", "show",
		"--property=MainPID,InvocationID,Restart,UnitFileState,KillMode,StartLimitIntervalUSec", unit).Output()
	if err != nil {
		return fmt.Errorf("read boot service definition: %w", err)
	}
	fields := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if key, value, ok := strings.Cut(line, "="); ok {
			fields[key] = value
		}
	}
	if fields["MainPID"] != strconv.Itoa(pid) || fields["InvocationID"] != invocation ||
		fields["UnitFileState"] != "enabled" || fields["Restart"] != "always" ||
		fields["KillMode"] != "control-group" || fields["StartLimitIntervalUSec"] != "0" {
		return errors.New("the active service must be enabled with Restart=always, KillMode=control-group and StartLimitIntervalSec=0")
	}
	return nil
}

func dataPathForHostRecovery(dataDir string) (string, error) {
	if strings.TrimSpace(dataDir) == "" {
		dataDir = config.DefaultDataDir()
	}
	path, err := filepath.Abs(dataDir)
	if err != nil {
		return "", err
	}
	if real, err := filepath.EvalSymlinks(path); err == nil {
		path = real
	}
	return path, nil
}

func holdStandaloneEngineAdmission(dataDir string) (func(), error) {
	path, err := dataPathForHostRecovery(dataDir)
	if err != nil {
		return nil, err
	}
	lock, err := hostrecovery.Guard(path, false)
	if err != nil {
		return nil, err
	}
	release := func() { _ = lock.Close() }
	if hostrecovery.Managed(path) {
		release()
		return nil, errors.New("this data directory belongs to the host supervisor")
	}
	return release, nil
}

func discoverSupervisedEngine(dataDir string, mock bool) (lease.Record, bool, error) {
	path, err := dataPathForHostRecovery(dataDir)
	if err != nil {
		return lease.Record{}, false, err
	}
	if !hostrecovery.Managed(path) {
		return lease.Record{}, false, nil
	}
	rec, err := lease.Discover(path, mock)
	if err == nil && rec.Startup != "ready" {
		err = errors.New("the supervised host is restoring or verifying an update")
	}
	return rec, true, err
}
