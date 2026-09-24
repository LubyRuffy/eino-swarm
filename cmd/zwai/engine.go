package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/app"
	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/lease"
	"github.com/LubyRuffy/eino-swarm/internal/server"
)

// activeEngineStop cancels the in-process engine tests start. Production
// shells spawn a child and must not call this: closing a window is not
// shutting the engine down.
var activeEngineStop func()

func runEngine(args []string) error {
	fs := flag.NewFlagSet("engine", flag.ExitOnError)
	dataDir := fs.String("data-dir", "", "data directory")
	addr := fs.String("addr", "", "listen address")
	mock := fs.Bool("mock", false, "run on the scripted offline provider")
	if err := fs.Parse(reorderFlags(args, map[string]bool{"data-dir": true, "addr": true})); err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return serveEngine(ctx, engineOpts{
		DataDir: *dataDir,
		Addr:    *addr,
		Mock:    *mock,
	})
}

type engineOpts struct {
	DataDir string
	Addr    string
	Mock    bool
	Watch   lease.Watcher
	Ready   func(url string)
}

// serveEngine is the long-lived process. It holds the data-dir lock, serves
// the same App the window used to own, and returns when the idle rule says
// nobody is left or when ctx is cancelled.
func serveEngine(ctx context.Context, opts engineOpts) error {
	cfg, err := config.Load(opts.DataDir)
	if err != nil {
		return err
	}
	dir := cfg.DataDir()
	hold, err := lease.Acquire(dir)
	if err != nil {
		return err
	}
	defer hold.Release()

	addr := opts.Addr
	if addr == "" {
		addr = "127.0.0.1:0"
	}
	a, err := assembleApp(app.Options{
		DataDir: dir,
		Addr:    addr,
		Mock:    opts.Mock,
		Mode:    server.ModeEngine,
		Version: version,
	})
	if err != nil {
		return err
	}
	url, err := a.Listen()
	if err != nil {
		shut(a)
		return err
	}
	build := lease.ThisBuild(version)
	if err := lease.Publish(dir, lease.Record{
		PID: os.Getpid(), URL: url, Mock: opts.Mock, StartedAt: time.Now().UTC(),
		Version: build.Version, Dev: build.Dev, Ino: build.Ino,
	}); err != nil {
		shut(a)
		return err
	}
	errCh := make(chan error, 1)
	go func() { errCh <- a.Serve() }()

	if opts.Ready != nil {
		opts.Ready(url)
	}
	// Tests stop the engine by cancelling ctx. The real idle grace would
	// race a slow test that has not opened a presence connection yet.
	watch := opts.Watch
	if testing.Testing() && watch.StartGrace == 0 && watch.IdleGrace == 0 && watch.Poll == 0 && watch.Now == nil {
		watch.StartGrace = time.Hour
		watch.IdleGrace = time.Hour
	}
	started := time.Now()
	watch.Run(ctx, started, func() (int, bool, int) {
		phone := a.Remote != nil && a.Remote.Status().Online
		return len(a.Server.Clients()), phone, len(a.Engine.Running())
	})
	shut(a)
	select {
	case err := <-errCh:
		if err != nil {
			return err
		}
	case <-time.After(shutdownGrace):
	}
	return nil
}

func shut(a *app.App) {
	ctx, cancel := context.WithTimeout(context.Background(), shutdownGrace)
	defer cancel()
	a.Shutdown(ctx)
}

// ensureEngine attaches to a live engine or starts one. A mock mismatch or a
// process that will not answer is an error: starting a second engine on the
// same database is the bug this exists to prevent.
func ensureEngine(dataDir, addr string, mock bool) (string, error) {
	cfg, err := config.Load(dataDir)
	if err != nil {
		return "", err
	}
	dir := cfg.DataDir()
	rec, err := lease.Discover(dir, mock)
	switch {
	case err == nil:
		if lease.SameEngine(rec, lease.ThisBuild(version)) {
			return rec.URL, nil
		}
		// A window restart is not a new engine. The binary the user just
		// opened has to be the one holding the database, or the phone keeps
		// talking to the build they thought they replaced. A turn that is
		// actually calling the model finishes first. A parked schedule and
		// a question waiting on the human are not that turn.
		force, err := waitUntilQuiet(rec.URL)
		if err != nil {
			// The process can exit while this shell is waiting. A dead pid
			// is the same as no engine; failing the launch would leave the
			// user with nothing to attach to.
			if _, discErr := lease.Discover(dir, mock); !errors.Is(discErr, lease.ErrNotRunning) {
				return "", err
			}
		} else {
			stop := lease.Stop
			if force {
				stop = lease.Kill
			}
			if err := stop(rec.PID); err != nil {
				return "", err
			}
		}
	case errors.Is(err, lease.ErrMockMismatch), errors.Is(err, lease.ErrUnreachable):
		return "", err
	}
	if testing.Testing() {
		return startInProcessEngine(dir, addr, mock)
	}
	return spawnEngine(dir, addr, mock)
}

// quietPoll is how often a newer shell asks whether the live engine is
// still in a model call. Short enough that a turn ending is not a long
// pause before the window opens. replaceHeartbeat is the status line
// while that wait continues. replaceForceAfter is when the shell asks
// before it kills the old process.
const (
	quietPoll         = 200 * time.Millisecond
	replaceHeartbeat  = 10 * time.Second
	replaceForceAfter = 30 * time.Second
)

// quietNow, quietSleep and confirmReplace are the wait loop's clock and
// the yes/no question. Tests move the clock instead of sleeping 30s.
var (
	quietNow       = time.Now
	quietSleep     = time.Sleep
	confirmReplace = func() bool { return readForceReplace(os.Stdin, stdinIsTTY()) }
)

type replaceWait struct {
	poll       time.Duration
	heartbeat  time.Duration
	forceAfter time.Duration
	now        func() time.Time
	sleep      func(time.Duration)
	logf       func(string, ...any)
	confirm    func() bool
}

// waitUntilQuiet reports whether the caller should kill the live engine.
// False means no conversation is in a model call. True means the user
// confirmed a force stop while one still was.
func waitUntilQuiet(base string) (bool, error) {
	client := &http.Client{Timeout: 2 * time.Second}
	return waitToReplace(func() (bool, error) {
		return engineBusy(client, base)
	}, replaceWait{
		poll: quietPoll, heartbeat: replaceHeartbeat, forceAfter: replaceForceAfter,
		now: quietNow, sleep: quietSleep, logf: log.Printf, confirm: confirmReplace,
	})
}

func waitToReplace(busy func() (bool, error), w replaceWait) (bool, error) {
	if w.now == nil {
		w.now = time.Now
	}
	if w.sleep == nil {
		w.sleep = time.Sleep
	}
	if w.logf == nil {
		w.logf = log.Printf
	}
	if w.confirm == nil {
		w.confirm = confirmReplace
	}
	if w.poll <= 0 {
		w.poll = quietPoll
	}
	started := w.now()
	var lastBeat, lastAsk time.Time
	told := false
	for {
		running, err := busy()
		if err != nil {
			return false, err
		}
		if !running {
			if told {
				w.logf("the running turn finished; replacing the engine")
			}
			return false, nil
		}
		if !told {
			w.logf("waiting for the running turn to finish before replacing the engine")
			told = true
		}
		now := w.now()
		elapsed := now.Sub(started)
		if w.heartbeat > 0 && elapsed >= w.heartbeat && (lastBeat.IsZero() || now.Sub(lastBeat) >= w.heartbeat) {
			w.logf("engine replace: checked at %s; a turn is still running", elapsed.Round(time.Second))
			lastBeat = now
		}
		if w.forceAfter > 0 && elapsed >= w.forceAfter && (lastAsk.IsZero() || now.Sub(lastAsk) >= w.forceAfter) {
			if w.confirm() {
				running, err = busy()
				if err != nil {
					return false, err
				}
				if !running {
					w.logf("the running turn finished; replacing the engine")
					return false, nil
				}
				w.logf("force-stopping the running turn and replacing the engine")
				return true, nil
			}
			// Count the interval from the answer. A slow "no" must not
			// be followed by another question on the next poll.
			lastAsk = w.now()
			w.logf("still waiting for the running turn to finish")
		}
		w.sleep(w.poll)
	}
}

var toldNoTerminal sync.Once

func readForceReplace(in io.Reader, tty bool) bool {
	if !tty {
		toldNoTerminal.Do(func() {
			log.Print("engine replace: no terminal to confirm a force stop; still waiting")
		})
		return false
	}
	fmt.Fprint(os.Stderr, "Force stop the running turn and replace the engine? [y/N] ")
	line, err := bufio.NewReader(in).ReadString('\n')
	if err != nil && line == "" {
		return false
	}
	return replaceConfirmed(line)
}

func replaceConfirmed(line string) bool {
	switch strings.ToLower(strings.TrimSpace(line)) {
	case "y", "yes", "是":
		return true
	default:
		return false
	}
}

func engineBusy(client *http.Client, base string) (bool, error) {
	resp, err := client.Get(base + "/api/threads")
	if err != nil {
		return false, fmt.Errorf("engine: list conversations: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return false, err
	}
	// An engine that does not list conversations cannot be shown to be
	// mid-call. Holding the replacement forever would leave the old binary
	// in place, which is the failure this wait exists to avoid.
	if resp.StatusCode != http.StatusOK {
		return false, nil
	}
	var page struct {
		Threads []struct {
			Running        bool `json:"running"`
			AwaitingAnswer bool `json:"awaiting_answer"`
		} `json:"threads"`
	}
	if err := json.Unmarshal(body, &page); err != nil {
		return false, fmt.Errorf("engine: list conversations: %w", err)
	}
	for _, th := range page.Threads {
		if th.Running && !th.AwaitingAnswer {
			return true, nil
		}
	}
	return false, nil
}

func startInProcessEngine(dir, addr string, mock bool) (string, error) {
	ctx, cancel := context.WithCancel(context.Background())
	ready := make(chan string, 1)
	errCh := make(chan error, 1)
	go func() {
		errCh <- serveEngine(ctx, engineOpts{
			DataDir: dir, Addr: addr, Mock: mock,
			Ready: func(url string) { ready <- url },
		})
	}()
	select {
	case url := <-ready:
		activeEngineStop = cancel
		return url, nil
	case err := <-errCh:
		cancel()
		if err == nil {
			err = errors.New("engine stopped before it was listening")
		}
		return "", err
	case <-time.After(30 * time.Second):
		cancel()
		return "", errors.New("engine did not start")
	}
}

func spawnEngine(dir, addr string, mock bool) (string, error) {
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
	logPath := dir + "/engine.log"
	f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return "", err
	}
	cmd.Stdout = f
	cmd.Stderr = f
	if err := cmd.Start(); err != nil {
		_ = f.Close()
		return "", err
	}
	_ = f.Close()
	exited := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(exited)
	}()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		select {
		case <-exited:
			tail, _ := os.ReadFile(logPath)
			if len(tail) > 2000 {
				tail = tail[len(tail)-2000:]
			}
			return "", fmt.Errorf("engine exited before it was listening: %s", tail)
		default:
		}
		rec, err := lease.Discover(dir, mock)
		if err == nil {
			return rec.URL, nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	tail, _ := os.ReadFile(logPath)
	if len(tail) > 2000 {
		tail = tail[len(tail)-2000:]
	}
	return "", fmt.Errorf("engine did not come up: %s", tail)
}
