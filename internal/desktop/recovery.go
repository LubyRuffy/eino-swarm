package desktop

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"image/color"
	"log/slog"
	"net/url"
	"os"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

// ErrRecoveryCleanup prevents another successor while an owned child may still exist.
var ErrRecoveryCleanup = errors.New("desktop recovery child did not exit")

// ErrRecoveryCancelled means a successor was closed normally before takeover.
var ErrRecoveryCancelled = errors.New("desktop recovery successor was closed")

// RecoveryDiagnostic contains lifecycle facts, never page content or snapshot pixels.
type RecoveryDiagnostic struct {
	SessionID  string    `json:"session_id"`
	Generation int       `json:"generation"`
	Attempt    int       `json:"attempt"`
	ShellPID   int       `json:"shell_pid"`
	EnginePID  int       `json:"engine_pid,omitempty"`
	ThreadID   string    `json:"thread_id,omitempty"`
	TurnID     string    `json:"turn_id,omitempty"`
	Phase      string    `json:"phase"`
	Reason     string    `json:"reason,omitempty"`
	At         time.Time `json:"at"`
	DurationMS int64     `json:"duration_ms,omitempty"`
}

// RecoveryAttempt carries the cumulative lineage budget into a successor shell.
type RecoveryAttempt struct {
	Generation int
	Attempts   int
	ThreadID   string
	ProjectID  string
	DraftKey   string
}

// RecoveryOptions connects native renderer recovery to the launcher's attach-only path.
type RecoveryOptions struct {
	SessionID  string
	Generation int
	Attempts   int
	Relaunch   func(context.Context, RecoveryAttempt) error
	Ready      func(context.Context) error
	Diagnostic func(RecoveryDiagnostic)
	// OnStopped releases shell resources only after its recovery owner has exited.
	OnStopped func()
	// ParentDone aborts an uncommitted successor if its private launch channel closes.
	ParentDone <-chan struct{}
}

type recoveryInput struct {
	message  string
	origin   *application.OriginInfo
	activity recoveryActivity
	active   *bool
	probe    string
	pixels   []byte
	err      error
}

type recoveryActivity uint8

const (
	recoveryFocused recoveryActivity = iota
	recoveryMinimized
	recoverySleeping
)

type recoveryController struct {
	window          application.Window
	opts            RecoveryOptions
	origin          string
	windowURL       string
	in              chan recoveryInput
	cancel          context.CancelFunc
	done            <-chan struct{}
	stopOnce        sync.Once
	initialActivity recoveryWindowActivity
	probe           func(string)
	capture         func(context.Context) ([]byte, error)
	reload          func(string)
	quit            func()
	showError       func()
	now             func() time.Time
	ticks           <-chan time.Time
}

func startRendererRecovery(app *application.App, window application.Window, opts Options) *recoveryController {
	return startRecoveryForHost(wailsRecoveryHost{app: app, window: window}, opts)
}

func startRecoveryForHost(host recoveryHost, opts Options) *recoveryController {
	if opts.Recovery == nil || !rendererRecoveryAvailable() {
		return nil
	}
	u, err := url.Parse(opts.URL)
	if err != nil || u.Scheme != "http" || u.Host == "" {
		return nil
	}
	ctx, cancel := context.WithCancel(host.Context())
	c := &recoveryController{window: host.Window(), opts: *opts.Recovery, origin: u.Scheme + "://" + u.Host, windowURL: opts.URL, in: make(chan recoveryInput, 32), cancel: cancel}
	c.probe = host.Probe
	c.capture = host.Snapshot
	c.reload = host.Reload
	c.quit = host.Quit
	c.now = time.Now
	c.showError = host.ShowError
	done := make(chan struct{})
	c.done = done
	host.OnShutdown(c.shutdown)
	bindRecoveryActivity(host, c)
	started := make(chan recoveryWindowActivity, 1)
	var startedOnce sync.Once
	host.OnApplicationEvent(events.Common.ApplicationStarted, func() {
		if ctx.Err() == nil {
			startedOnce.Do(func() { started <- host.Activity() })
		}
	})
	go func() {
		defer close(done)
		select {
		case <-ctx.Done():
			return
		case activity := <-started:
			c.initialActivity = activity
			c.run(ctx)
		}
	}()
	return c
}

func bindRecoveryActivity(host recoveryHost, c *recoveryController) {
	host.OnWindowEvent(events.Common.WindowFocus, func() { c.setActivity(recoveryFocused, true) })
	host.OnWindowEvent(events.Common.WindowLostFocus, func() { c.setActivity(recoveryFocused, false) })
	host.OnWindowEvent(events.Mac.WindowDidMiniaturize, func() { c.setActivity(recoveryMinimized, true) })
	host.OnWindowEvent(events.Mac.WindowDidDeminiaturize, func() { c.setActivity(recoveryMinimized, false) })
	host.OnApplicationEvent(events.Common.SystemWillSleep, func() { c.setActivity(recoverySleeping, true) })
	host.OnApplicationEvent(events.Common.SystemDidWake, func() { c.setActivity(recoverySleeping, false) })
}

func (c *recoveryController) shutdown() {
	c.stopOnce.Do(func() {
		c.cancel()
		select {
		case <-c.done:
			if c.opts.OnStopped != nil {
				c.opts.OnStopped()
			}
		case <-time.After(recoveryShutdownWait):
			slog.Warn("desktop recovery owner did not stop; retaining its private copy", "session_id", c.opts.SessionID)
		}
	})
}

func (c *recoveryController) setActivity(activity recoveryActivity, active bool) {
	select {
	case c.in <- recoveryInput{activity: activity, active: &active}:
	default:
	}
}

func (c *recoveryController) raw(window application.Window, message string, origin *application.OriginInfo) {
	if window != c.window || len(message) > recoveryMessageLimit {
		return
	}
	if origin != nil {
		copied := *origin
		origin = &copied
	}
	select {
	case c.in <- recoveryInput{message: message, origin: origin}:
	default:
	}
}

func (c *recoveryController) run(ctx context.Context) {
	now := c.now()
	state := newRecoveryState(now, c.opts.Attempts)
	ticks := c.ticks
	if ticks == nil {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		ticks = ticker.C
	}
	lastTick := now
	var challenge string
	probeStage := "probe_not_started"
	var probeStarted time.Time
	var first, second color.NRGBA
	location := recoveryLocationFromURL(c.windowURL)
	expected := location
	awaitingLocation := expected != (recoveryLocation{})
	independentLocation := false
	document := 0
	var snapshotCancel context.CancelFunc
	defer func() {
		if snapshotCancel != nil {
			snapshotCancel()
		}
	}()
	ready := false
	state.probeOnly = c.opts.Ready != nil
	foreground, minimized, sleeping := c.initialActivity.focused, c.initialActivity.minimized, false
	state.active = foreground && !minimized
	snapshotPending := false
	probeReported := false
	report := func(phase, reason string) {
		if c.opts.Diagnostic != nil {
			observed := c.now()
			var elapsed int64
			if !probeStarted.IsZero() && !observed.Before(probeStarted) {
				elapsed = observed.Sub(probeStarted).Milliseconds()
			}
			c.opts.Diagnostic(RecoveryDiagnostic{SessionID: c.opts.SessionID, Generation: c.opts.Generation, Attempt: state.attempts, ShellPID: os.Getpid(), ThreadID: location.ThreadID, TurnID: location.TurnID, Phase: phase, Reason: reason, At: observed.UTC(), DurationMS: elapsed})
		}
	}
	clearProbe := func() {
		challenge = ""
		if snapshotCancel != nil {
			snapshotCancel()
			snapshotCancel = nil
		}
		snapshotPending = false
	}
	showExhausted := func() {
		clearProbe()
		report("exhausted", "renderer_recovery_budget_exhausted")
		c.showError()
	}
	for {
		if ctx.Err() != nil {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-c.opts.ParentDone:
			c.quit()
			return
		case input := <-c.in:
			now = c.now()
			if input.active != nil {
				switch input.activity {
				case recoveryMinimized:
					minimized = *input.active
				case recoverySleeping:
					sleeping = *input.active
				default:
					foreground = *input.active
				}
				active := foreground && !minimized && !sleeping
				if active == state.active {
					continue
				}
				state.setActive(active, now)
				clearProbe()
				probeStage, probeStarted = "probe_not_started", time.Time{}
				if active {
					report("resumed", "")
				} else {
					report("suspended", "")
				}
				continue
			}
			if next, valid := validRecoveryLocation(c.origin, c.opts.SessionID, c.opts.Generation, document, input.message, input.origin); valid {
				if awaitingLocation && !expected.matchesRestored(next) {
					continue
				}
				location, independentLocation = next, true
				if awaitingLocation {
					// A pre-restore capture cannot acknowledge the restored document.
					// Only the per-probe work is discarded; outer deadlines stay fixed.
					awaitingLocation = false
					clearProbe()
					probeStage, probeStarted = "probe_not_started", time.Time{}
					state.pendingUntil, state.nextProbe = time.Time{}, now
				}
				continue
			}
			if input.probe != "" {
				if input.probe != challenge || !state.active {
					continue
				}
				clearProbe()
				if input.err != nil || !snapshotMatchesProbe(input.pixels, first, second, input.probe) {
					probeStage = "snapshot_mismatch"
					if input.err != nil {
						probeStage = "snapshot_error"
					}
					state.failed(now)
					report("probe_failed", probeStage)
					continue
				}
				state.healthy(now)
				probeStage = "probe_not_pending"
				if !ready {
					ready = true
					report("ready", "")
					if c.opts.Ready != nil {
						if err := c.opts.Ready(ctx); err != nil {
							report("replacement_failed", "handoff_not_committed")
							c.quit()
							return
						}
					}
					state.probeOnly = false
				}
				continue
			}
			if awaitingLocation || snapshotPending || !state.active || !validRecoveryMessage(c.origin, c.opts.SessionID, c.opts.Generation, challenge, input.message, input.origin) {
				continue
			}
			if attribution, valid := readRecoveryLocation(input.message); valid && !independentLocation {
				location = attribution
			}
			snapshotPending = true
			probeStage = "snapshot_pending"
			snapshotCtx, cancelSnapshot := context.WithDeadline(ctx, state.pendingUntil)
			snapshotCancel = cancelSnapshot
			go func(token string, probeCtx context.Context, cancelCapture context.CancelFunc) {
				defer cancelCapture()
				pixels, err := c.capture(probeCtx)
				select {
				case c.in <- recoveryInput{probe: token, pixels: pixels, err: err}:
				case <-ctx.Done():
				}
			}(challenge, snapshotCtx, cancelSnapshot)
		case now = <-ticks:
			// Relaunch can block for its full deadline. Ignore any tick queued
			// before it returned, including when it was about to enter sleep.
			if now.Before(lastTick) {
				continue
			}
			if now.Sub(lastTick) > recoveryHealthTimeout {
				state.setActive(state.active, now)
				clearProbe()
				probeStage, probeStarted = "probe_not_started", time.Time{}
			}
			lastTick = now
			// A document can mount after evaluation. Retry an unanswered startup probe
			// without replacing a real snapshot or extending either recovery deadline.
			retrying := !state.retryAt.IsZero()
			if state.active && !retrying && (!ready || state.stage == recoveryReload) && !state.pendingUntil.IsZero() && !now.Before(state.nextProbe) {
				if state.probeOnly && !probeReported {
					// Report before the parent deadline, even though the successor
					// cannot reload or replace itself before commit.
					report("probe_failed", probeStage)
					probeReported = true
				}
				if !snapshotPending {
					state.pendingUntil = time.Time{}
				} else if state.probeOnly && !now.Before(state.pendingUntil) {
					clearProbe()
				}
			}
			switch state.tick(now) {
			case recoveryProbe:
				clearProbe()
				probeReported = false
				var random [8]byte
				if _, err := rand.Read(random[:]); err != nil {
					state.pendingUntil = time.Time{}
					continue
				}
				challenge = hex.EncodeToString(random[:])
				probeStarted, probeStage = c.now(), "callback_missing"
				if retrying {
					// A queued tick predates dispatch. Give this new retry probe
					// its full health interval without moving outer deadlines.
					state.pendingUntil = probeStarted.Add(recoveryHealthTimeout)
					state.graceUntil = state.pendingUntil
				}
				if awaitingLocation {
					probeStage = "location_pending"
				}
				first = color.NRGBA{R: 40 + random[0]%64, G: 40 + random[1]%64, B: 40 + random[2]%64, A: 255}
				second = color.NRGBA{R: 255 - first.R, G: 255 - first.G, B: 255 - first.B, A: 255}
				c.probe(recoveryProbeScript(c.origin, c.opts.SessionID, c.opts.Generation, challenge, first, second, document))
			case recoveryReload:
				// Report the last probe's missing boundary before reload removes
				// that document. This adds no per-probe journal traffic or retries.
				report("probe_failed", probeStage)
				report("reloading", "renderer_unresponsive")
				clearProbe()
				document++
				expected = location
				awaitingLocation = expected != (recoveryLocation{})
				c.reload(location.windowURL(c.windowURL))
			case recoveryReplace:
				report("probe_failed", probeStage)
				reason := "reload_did_not_restore_renderer"
				if retrying {
					reason = probeStage
				}
				report("replacing", reason)
				clearProbe()
				if c.opts.Relaunch != nil {
					err := c.opts.Relaunch(ctx, RecoveryAttempt{Generation: c.opts.Generation + 1, Attempts: state.attempts, ThreadID: location.ThreadID, ProjectID: location.ProjectID, DraftKey: location.DraftKey})
					lastTick = c.now()
					if err == nil {
						report("recovered", "")
						c.quit()
						return
					}
					report("replacement_failed", err.Error())
					if errors.Is(err, ErrRecoveryCancelled) {
						c.quit()
						return
					}
					if errors.Is(err, ErrRecoveryCleanup) {
						state.attempts = recoveryMaxAttempts
					}
					if ctx.Err() != nil {
						return
					}
				}
				if state.replacementFailed(lastTick) == recoveryExhausted {
					showExhausted()
				} else {
					probeStage, probeStarted = "probe_not_started", time.Time{}
				}
			case recoveryExhausted:
				showExhausted()
			}
		}
	}
}
