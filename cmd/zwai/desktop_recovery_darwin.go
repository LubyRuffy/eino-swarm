//go:build darwin

package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/desktop"
	"github.com/LubyRuffy/eino-swarm/internal/lease"
)

const desktopRecoveryJournalLimit = 64 << 10
const desktopSuccessorDeadline = 30 * time.Second

var focusRecoverySuccessor = desktop.FocusRecoverySuccessor
var waitRecoverySuccessor = (*exec.Cmd).Wait
var readRecoveryReady = (*json.Decoder).Decode

type desktopRecoverySpec struct {
	SessionID        string       `json:"session_id"`
	Token            string       `json:"token"`
	DataDir          string       `json:"data_dir"`
	Mock             bool         `json:"mock"`
	Engine           lease.Record `json:"engine"`
	PrivateDir       string       `json:"private_dir"`
	Executable       string       `json:"executable"`
	ExecutableSHA256 string       `json:"executable_sha256"`
	Journal          string       `json:"journal"`
	Generation       int          `json:"generation"`
	Attempts         int          `json:"attempts"`
	ThreadID         string       `json:"thread_id,omitempty"`
	ProjectID        string       `json:"project_id,omitempty"`
	DraftKey         string       `json:"draft_key,omitempty"`
}

type desktopRecoveryReady struct {
	SessionID  string `json:"session_id"`
	Generation int    `json:"generation"`
	Token      string `json:"token"`
	PID        int    `json:"pid"`
	Committed  bool   `json:"committed,omitempty"`
}

type desktopRecoveryCommit struct {
	Token  string `json:"token"`
	Commit bool   `json:"commit"`
}

func configureDesktopRecovery(opts desktop.Options, args []string) (desktop.Options, func(), error) {
	return configureDesktopRecoveryWithSystemPaths(opts, args, os.UserCacheDir, os.Executable)
}

func configureDesktopRecoveryWithSystemPaths(opts desktop.Options, args []string, userCacheDir, currentExecutable func() (string, error)) (desktop.Options, func(), error) {
	cache, err := userCacheDir()
	if err != nil {
		return opts, nil, err
	}
	executable, err := currentExecutable()
	if err != nil {
		return opts, nil, err
	}
	return configureDesktopRecoveryAt(opts, args, cache, executable)
}

func configureDesktopRecoveryAt(opts desktop.Options, args []string, cache, executable string) (desktop.Options, func(), error) {
	fs := flag.NewFlagSet("desktop recovery", flag.ContinueOnError)
	dataDir := fs.String("data-dir", "", "")
	mock := fs.Bool("mock", false, "")
	if err := fs.Parse(reorderFlags(args, map[string]bool{"data-dir": true})); err != nil {
		return opts, nil, err
	}
	cfg, err := config.Load(*dataDir)
	if err != nil {
		return opts, nil, err
	}
	record, err := lease.Discover(cfg.DataDir(), *mock)
	if err != nil {
		return opts, nil, err
	}
	if !sameRecoveryOrigin(record.URL, opts.URL) {
		return opts, nil, errors.New("desktop recovery engine origin changed")
	}
	var random [24]byte
	if _, err := rand.Read(random[:]); err != nil {
		return opts, nil, err
	}
	session := hex.EncodeToString(random[:8])
	parent := filepath.Join(cache, "zwai")
	if err := os.MkdirAll(parent, 0700); err != nil {
		return opts, nil, err
	}
	private, err := os.MkdirTemp(parent, "desktop-recovery-")
	if err != nil {
		return opts, nil, err
	}
	cleanupFailure := func(err error) (desktop.Options, func(), error) { _ = os.RemoveAll(private); return opts, nil, err }
	if err := os.WriteFile(filepath.Join(private, "session"), []byte(session), 0600); err != nil {
		return cleanupFailure(err)
	}
	appDir := filepath.Join(private, "zwai.app")
	bundleVersion := version
	if strings.Trim(bundleVersion, "0123456789.") != "" {
		bundleVersion = ""
	}
	if err := desktop.WriteMacApp(executable, appDir, bundleVersion); err != nil {
		return cleanupFailure(err)
	}
	recoveryExecutable := filepath.Join(appDir, "Contents", "MacOS", "zwai")
	executableHash, err := desktopRecoveryExecutableHash(recoveryExecutable)
	if err != nil {
		return cleanupFailure(err)
	}
	diagnosticDir := filepath.Join(cfg.DataDir(), "diagnostics")
	if err := os.MkdirAll(diagnosticDir, 0700); err != nil {
		return cleanupFailure(err)
	}
	spec := desktopRecoverySpec{SessionID: session, Token: hex.EncodeToString(random[8:]), DataDir: cfg.DataDir(), Mock: *mock, Engine: record, PrivateDir: private, Executable: recoveryExecutable, ExecutableSHA256: executableHash, Journal: filepath.Join(diagnosticDir, "desktop-"+session+".jsonl")}
	configured, cleanup, err := attachDesktopRecovery(opts, spec, nil)
	if err != nil {
		return cleanupFailure(err)
	}
	return configured, cleanup, nil
}

func attachDesktopRecovery(opts desktop.Options, spec desktopRecoverySpec, ready func(context.Context) error) (desktop.Options, func(), error) {
	return attachDesktopRecoveryWithLauncher(opts, spec, ready, runRecoverySuccessor)
}

func attachDesktopRecoveryWithLauncher(opts desktop.Options, spec desktopRecoverySpec, ready func(context.Context) error, launch func(context.Context, desktopRecoverySpec, *exec.Cmd) error) (desktop.Options, func(), error) {
	release := func() {}
	if opts.Fatal == nil {
		held, err := holdDesktopPresence(spec.Engine.URL)
		if err != nil {
			return opts, nil, err
		}
		release = held.release
		opts.Fatal = held.fatal
		u, _ := url.Parse(opts.URL)
		q := u.Query()
		q.Set("presence", held.id)
		u.RawQuery = q.Encode()
		opts.URL = u.String()
	}
	var transferred atomic.Bool
	var ownsCopy atomic.Bool
	var controllerStopped atomic.Bool
	var retainCopy atomic.Bool
	var copyCleanup sync.Once
	ownsCopy.Store(ready == nil)
	report, closeReporter := desktopRecoveryReporter(spec)
	opts.Recovery = &desktop.RecoveryOptions{SessionID: spec.SessionID, Generation: spec.Generation, Attempts: spec.Attempts, Diagnostic: report, Relaunch: func(parent context.Context, attempt desktop.RecoveryAttempt) error {
		next := spec
		next.Generation, next.Attempts = attempt.Generation, attempt.Attempts
		next.ThreadID, next.ProjectID = attempt.ThreadID, attempt.ProjectID
		next.DraftKey = attempt.DraftKey
		launchCtx, stop := context.WithTimeout(parent, desktopSuccessorDeadline)
		defer stop()
		if err := verifyDesktopRecoveryTarget(launchCtx, next); err != nil {
			return fmt.Errorf("desktop recovery parent prelaunch: %w", err)
		}
		actual, err := desktopRecoveryExecutableHash(next.Executable)
		if err != nil {
			return err
		}
		if actual != next.ExecutableSHA256 {
			return errors.New("desktop recovery executable identity changed")
		}
		command := exec.Command(next.Executable, "desktop-attach")
		command.Stdout, command.Stderr = os.Stdout, os.Stderr
		command.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
		if err := launch(launchCtx, next, command); err != nil {
			if errors.Is(err, desktop.ErrRecoveryCleanup) {
				retainCopy.Store(true)
				slog.Warn("desktop recovery child did not exit; retaining its private copy", "session_id", spec.SessionID)
			}
			return err
		}
		transferred.Store(true)
		return nil
	}}
	if ready != nil {
		opts.Recovery.Ready = func(ctx context.Context) error {
			if err := ready(ctx); err != nil {
				return err
			}
			ownsCopy.Store(true)
			return nil
		}
	}
	previousShutdown := opts.OnShutdown
	opts.OnShutdown = func() {
		release()
		if previousShutdown != nil {
			previousShutdown()
		}
	}
	cleanup := func() {
		release()
		closeReporter()
		if controllerStopped.Load() && ownsCopy.Load() && !transferred.Load() && !retainCopy.Load() {
			copyCleanup.Do(func() { removeDesktopRecoveryCopy(spec) })
		}
	}
	opts.Recovery.OnStopped = func() {
		controllerStopped.Store(true)
		cleanup()
	}
	return opts, cleanup, nil
}

func runDesktopAttach(args []string) error {
	if len(args) != 0 {
		return errors.New("desktop-attach accepts only an inherited recovery channel")
	}
	readyPipe, control := os.NewFile(3, "desktop recovery ready"), os.NewFile(4, "desktop recovery control")
	defer readyPipe.Close()
	defer control.Close()
	for _, file := range []*os.File{readyPipe, control} {
		info, err := file.Stat()
		if err != nil || info.Mode()&os.ModeNamedPipe == 0 {
			return errors.New("desktop-attach requires private inherited pipes")
		}
	}
	return runDesktopAttachment(control, readyPipe)
}

func runDesktopAttachment(control io.Reader, readyPipe io.Writer) error {
	decoder := json.NewDecoder(io.LimitReader(control, 16<<10))
	var spec desktopRecoverySpec
	if err := decoder.Decode(&spec); err != nil {
		return fmt.Errorf("read desktop recovery handoff: %w", err)
	}
	if spec.SessionID == "" || spec.Token == "" || spec.Generation < 1 || spec.Attempts < 1 || spec.Attempts > 2 {
		return errors.New("invalid desktop recovery lineage")
	}
	if err := verifyDesktopRecoveryTarget(context.Background(), spec); err != nil {
		return fmt.Errorf("desktop recovery successor attach: %w", err)
	}
	aborted := make(chan struct{})
	type commitResult struct {
		commit desktopRecoveryCommit
		err    error
	}
	commitRead := make(chan commitResult, 1)
	go func() {
		var commit desktopRecoveryCommit
		err := decoder.Decode(&commit)
		commitRead <- commitResult{commit: commit, err: err}
		if err != nil || commit.Token != spec.Token || !commit.Commit {
			close(aborted)
		}
	}()
	windowURL := recoveryWindowURL(spec.Engine.URL, spec.ThreadID, spec.ProjectID)
	if spec.DraftKey != "" {
		u, _ := url.Parse(windowURL)
		q := u.Query()
		q.Set("recovery_draft", spec.DraftKey)
		u.RawQuery = q.Encode()
		windowURL = u.String()
	}
	opts := desktop.Options{URL: windowURL, Title: "zwai", Version: version}
	opts, cleanup, err := attachDesktopRecovery(opts, spec, func(ctx context.Context) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := json.NewEncoder(readyPipe).Encode(desktopRecoveryReady{SessionID: spec.SessionID, Generation: spec.Generation, Token: spec.Token, PID: os.Getpid()}); err != nil {
			return err
		}
		var commit desktopRecoveryCommit
		select {
		case <-ctx.Done():
			return ctx.Err()
		case result := <-commitRead:
			if result.err != nil {
				return result.err
			}
			commit = result.commit
		}
		if commit.Token != spec.Token || !commit.Commit {
			return errors.New("desktop recovery was not committed")
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		return json.NewEncoder(readyPipe).Encode(desktopRecoveryReady{SessionID: spec.SessionID, Generation: spec.Generation, Token: spec.Token, PID: os.Getpid(), Committed: true})
	})
	if err != nil {
		return err
	}
	opts.Recovery.ParentDone = aborted
	defer cleanup()
	return openDesktopWindow(opts)
}

func verifyDesktopRecoveryTarget(ctx context.Context, spec desktopRecoverySpec) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	record, err := lease.Discover(spec.DataDir, spec.Mock)
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if err != nil {
		return fmt.Errorf("desktop recovery original engine: %w", err)
	}
	if record.PID != spec.Engine.PID || record.URL != spec.Engine.URL || record.Dev != spec.Engine.Dev || record.Ino != spec.Engine.Ino || record.Version != spec.Engine.Version {
		return errors.New("desktop recovery original engine identity changed")
	}
	return nil
}

func recoveryWindowURL(base, threadID, projectID string) string {
	u, err := url.Parse(shellURL(base, "desktop"))
	if err != nil {
		return base
	}
	q := u.Query()
	if threadID != "" {
		q.Set("recovery_thread", threadID)
	} else if projectID != "" {
		q.Set("recovery_project", projectID)
	}
	u.RawQuery = q.Encode()
	return u.String()
}

func sameRecoveryOrigin(first, second string) bool {
	a, err := url.Parse(first)
	if err != nil {
		return false
	}
	b, err := url.Parse(second)
	return err == nil && a.Scheme == "http" && a.Host != "" && a.Scheme == b.Scheme && a.Host == b.Host
}

func runRecoverySuccessor(ctx context.Context, spec desktopRecoverySpec, command *exec.Cmd) (result error) {
	readyReader, readyWriter, err := os.Pipe()
	if err != nil {
		return err
	}
	defer readyReader.Close()
	defer readyWriter.Close()
	controlReader, controlWriter, err := os.Pipe()
	if err != nil {
		return err
	}
	defer controlReader.Close()
	defer controlWriter.Close()
	command.ExtraFiles = []*os.File{readyWriter, controlReader}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := command.Start(); err != nil {
		return fmt.Errorf("start recovery shell: %w", err)
	}
	exit := make(chan error, 1)
	waitChild := waitRecoverySuccessor
	go func() { exit <- waitChild(command) }()
	committed := false
	exited := false
	defer func() {
		if !committed && !exited {
			if err := stopRecoveryChild(command, exit); err != nil {
				result = errors.Join(result, err)
			}
		}
	}()
	_ = readyWriter.Close()
	_ = controlReader.Close()
	if err := json.NewEncoder(controlWriter).Encode(spec); err != nil {
		return fmt.Errorf("send recovery handoff: %w", err)
	}
	focusResult, stopFocus := startOwnedRecoveryFocus(ctx, command)
	defer stopFocus()
	var ready desktopRecoveryReady
	receive := make(chan error, 1)
	decoder := json.NewDecoder(io.LimitReader(readyReader, 4096))
	readReady := readRecoveryReady
	go func() { receive <- readReady(decoder, &ready) }()

readyRead:
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-focusResult:
			focusResult = nil
			if err != nil {
				// Activation is assistance, not the handoff result. A departed
				// child's kernel identity can disappear before Wait or pipe EOF.
				slog.Warn("desktop recovery focus assist unavailable", "session_id", spec.SessionID,
					"generation", spec.Generation, "child_pid", command.Process.Pid, "error", err)
			}
		case err := <-receive:
			// A real ready frame never waits for native registration observation.
			// Join it before commit; a focus request itself cannot produce ready.
			stopFocus()
			if err != nil {
				if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
					var observed bool
					result, observed = observeRecoveryChildExit(ctx, exit)
					exited = observed
					return result
				}
				return fmt.Errorf("recovery renderer ready: %w", err)
			}
			break readyRead
		case err := <-exit:
			exited = true
			if err == nil {
				return desktop.ErrRecoveryCancelled
			}
			return fmt.Errorf("recovery shell exited before ready: %w", err)
		}
	}
	if ready.SessionID != spec.SessionID || ready.Generation != spec.Generation || ready.Token != spec.Token || ready.PID != command.Process.Pid {
		return errors.New("recovery renderer ready identity mismatch")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if spec.Engine.PID > 0 {
		if err := verifyDesktopRecoveryTarget(ctx, spec); err != nil {
			return fmt.Errorf("desktop recovery post-ready precommit: %w", err)
		}
	}
	if err := json.NewEncoder(controlWriter).Encode(desktopRecoveryCommit{Token: spec.Token, Commit: true}); err != nil {
		return fmt.Errorf("commit recovery renderer: %w", err)
	}
	var acknowledgement desktopRecoveryReady
	go func() { receive <- readReady(decoder, &acknowledgement) }()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-receive:
		if err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
				var observed bool
				result, observed = observeRecoveryChildExit(ctx, exit)
				exited = observed
				return result
			}
			return fmt.Errorf("recovery commit receipt: %w", err)
		}
	}
	if acknowledgement.SessionID != spec.SessionID || acknowledgement.Generation != spec.Generation || acknowledgement.Token != spec.Token || acknowledgement.PID != command.Process.Pid || !acknowledgement.Committed {
		return errors.New("recovery commit receipt identity mismatch")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	committed = true
	return nil
}

func startOwnedRecoveryFocus(ctx context.Context, command *exec.Cmd) (<-chan error, func()) {
	focusContext, cancelFocus := context.WithCancel(ctx)
	result, done := make(chan error, 1), make(chan struct{})
	go func() {
		defer close(done)
		result <- focusRecoverySuccessor(focusContext, command.Process.Pid, command.Path)
	}()
	return result, func() { cancelFocus(); <-done }
}

func observeRecoveryChildExit(ctx context.Context, exit <-chan error) (error, bool) {
	select {
	case err := <-exit:
		if err == nil {
			return desktop.ErrRecoveryCancelled, true
		}
		return fmt.Errorf("recovery shell exited: %w", err), true
	case <-ctx.Done():
		return ctx.Err(), false
	}
}

func stopRecoveryChild(command *exec.Cmd, exit <-chan error) error {
	select {
	case <-exit:
		return nil
	default:
	}
	_ = command.Process.Signal(syscall.SIGTERM)
	timer := time.NewTimer(2 * time.Second)
	defer timer.Stop()
	select {
	case <-exit:
		return nil
	case <-timer.C:
	}
	_ = command.Process.Kill()
	timer.Reset(2 * time.Second)
	select {
	case <-exit:
		return nil
	case <-timer.C:
		return desktop.ErrRecoveryCleanup
	}
}

func desktopRecoveryReporter(spec desktopRecoverySpec) (func(desktop.RecoveryDiagnostic), func()) {
	queue := make(chan desktop.RecoveryDiagnostic, 32)
	stop := make(chan struct{})
	var queueMu sync.Mutex
	stopped := false
	var stopOnce sync.Once
	done := make(chan struct{})
	requestContext, cancelRequests := context.WithCancel(context.Background())
	go func() {
		defer close(done)
		defer cancelRequests()
		client := &http.Client{Timeout: 750 * time.Millisecond}
		defer client.CloseIdleConnections()
		for event := range queue {
			event = normalizeRecoveryDiagnostic(event, spec.Engine.PID)
			if err := appendRecoveryDiagnostic(spec.Journal, event); err != nil {
				slog.Warn("desktop recovery diagnostic write failed", "session_id", spec.SessionID, "error", err)
			}
			select {
			case <-stop:
				continue
			default:
			}
			body, _ := json.Marshal(event)
			request, err := http.NewRequestWithContext(requestContext, http.MethodPost, spec.Engine.URL+"/api/desktop/recovery", bytes.NewReader(body))
			var response *http.Response
			if err == nil {
				request.Header.Set("Content-Type", "application/json")
				response, err = client.Do(request)
			}
			if err != nil {
				slog.Warn("desktop recovery Trace unavailable", "session_id", spec.SessionID, "phase", event.Phase, "error", err)
				continue
			}
			_ = response.Body.Close()
			if response.StatusCode < 200 || response.StatusCode >= 300 {
				slog.Warn("desktop recovery Trace rejected", "session_id", spec.SessionID, "phase", event.Phase, "status", response.StatusCode)
			}
		}
	}()
	return func(event desktop.RecoveryDiagnostic) {
			queueMu.Lock()
			if stopped {
				queueMu.Unlock()
				return
			}
			select {
			case queue <- event:
				queueMu.Unlock()
			default:
				queueMu.Unlock()
				slog.Warn("desktop recovery diagnostic queue full", "session_id", spec.SessionID)
			}
		}, func() {
			stopOnce.Do(func() {
				queueMu.Lock()
				stopped = true
				close(stop)
				close(queue)
				queueMu.Unlock()
				cancelRequests()
			})
			select {
			case <-done:
			case <-time.After(3 * time.Second):
			}
		}
}

func normalizeRecoveryDiagnostic(event desktop.RecoveryDiagnostic, enginePID int) desktop.RecoveryDiagnostic {
	event.EnginePID = enginePID
	if len(event.Reason) > 256 {
		event.Reason = event.Reason[:256]
	}
	event.Reason = strings.ToValidUTF8(event.Reason, "")
	return event
}

func desktopRecoveryExecutableHash(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", err
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func appendRecoveryDiagnostic(path string, event desktop.RecoveryDiagnostic) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer file.Close()
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	defer syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
	body, err := io.ReadAll(io.LimitReader(file, desktopRecoveryJournalLimit))
	if err != nil {
		return err
	}
	row, err := json.Marshal(event)
	if err != nil {
		return err
	}
	row = append(row, '\n')
	if len(row) > desktopRecoveryJournalLimit {
		return errors.New("desktop recovery diagnostic exceeds journal limit")
	}
	if len(body)+len(row) > desktopRecoveryJournalLimit {
		body = body[len(body)/2:]
		if end := bytes.IndexByte(body, '\n'); end >= 0 {
			body = body[end+1:]
		} else {
			body = nil
		}
	}
	body = append(body, row...)
	if err := file.Truncate(0); err != nil {
		return err
	}
	if _, err := file.WriteAt(body, 0); err != nil {
		return err
	}
	return file.Sync()
}

func removeDesktopRecoveryCopy(spec desktopRecoverySpec) {
	if !filepath.IsAbs(spec.PrivateDir) || !strings.HasPrefix(filepath.Base(spec.PrivateDir), "desktop-recovery-") {
		return
	}
	info, err := os.Lstat(spec.PrivateDir)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return
	}
	marker, err := os.ReadFile(filepath.Join(spec.PrivateDir, "session"))
	if err != nil || string(marker) != spec.SessionID {
		return
	}
	if err := os.RemoveAll(spec.PrivateDir); err != nil {
		slog.Warn("desktop recovery private copy cleanup failed", "session_id", spec.SessionID, "error", err)
	}
}
