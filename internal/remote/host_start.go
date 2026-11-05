package remote

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/pairlink/client"
)

// Registration and the initial socket share the same bounded attempt. The
// deadline does not govern the connected client's later reconnects, or the
// backoff before a socket exists.
const hostConnectTimeout = 15 * time.Second

// A laptop can lose its source address for longer than one attempt. Retry
// until the hub accepts the PC; Stop, sign-out and Reload cancel the wait.
const (
	hostReconnectStart = time.Second
	hostReconnectMax   = 15 * time.Second
)

type hostStart struct {
	ctx     context.Context
	cancel  context.CancelFunc
	done    chan struct{}
	config  client.Config
	private bool
}

// hostReconnect is one pending backoff. done closes after the wait returns,
// including when that wait goes on to dial.
type hostReconnect struct {
	ctx    context.Context
	cancel context.CancelFunc
	done   chan struct{}
}

// StartBackground starts one cancellable connection attempt. Local readiness
// does not depend on the hub; Stop and Reload own cancellation and cleanup.
// A failed attempt schedules its own replacement and does not block startup.
func (h *Host) StartBackground() {
	h.accounts.Start()
	h.mu.Lock()
	attempt, launch := h.prepareStartLocked(context.Background())
	h.applyWakeLocked()
	h.mu.Unlock()
	if launch {
		go h.connect(attempt)
	}
}

func (h *Host) prepareStartLocked(parent context.Context) (*hostStart, bool) {
	if h.logoutActive != nil {
		return nil, false
	}
	if h.conn != nil {
		return nil, false
	}
	if h.starting != nil {
		return h.starting, false
	}
	cfg := h.cfg
	if cfg != nil {
		cfg = cfg.Snapshot()
	}
	if cfg == nil || !cfg.Remote.Enabled {
		return nil, false
	}
	if cfg.Remote.HubURL == "" {
		h.err = "remote needs hub_url"
		return nil, false
	}
	if cfg.Remote.AuthMode == config.RemoteAuthNOSEC && (h.loginActive != nil || h.login.State != "" && h.login.State != "authorized") {
		h.err = "remote: NOSEC login and account approval are required"
		return nil, false
	}
	token, err := cfg.EnsureHostToken()
	if err != nil {
		h.err = err.Error()
		return nil, false
	}
	id, err := loadIdentity(cfg)
	if err != nil {
		h.err = err.Error()
		return nil, false
	}
	ctx, cancel := context.WithTimeout(parent, hostConnectTimeout)
	attempt := &hostStart{
		ctx: ctx, cancel: cancel, done: make(chan struct{}), private: cfg.Remote.AuthMode == config.RemoteAuthNOSEC,
		config: client.Config{
			HubURL: cfg.Remote.HubURL, Identity: id, Token: token,
			KeepAlive: h.keepAlive,
			Name:      config.SeedRemoteDisplayName(cfg.Remote.DisplayName), Version: h.version,
		},
	}
	if attempt.private {
		attempt.config.HTTPClient = newLoginClient()
	}
	h.starting = attempt
	h.err = ""
	return attempt, true
}

func (h *Host) finishStart(attempt *hostStart, launch bool) {
	if attempt == nil {
		return
	}
	if launch {
		h.connect(attempt)
	} else {
		<-attempt.done
	}
}

func (h *Host) startForOffer(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	h.mu.Lock()
	attempt, launch := h.prepareStartLocked(ctx)
	h.applyWakeLocked()
	h.mu.Unlock()
	if launch {
		h.connect(attempt)
	} else if attempt != nil {
		select {
		case <-attempt.done:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return ctx.Err()
}

func (h *Host) connect(attempt *hostStart) {
	defer close(attempt.done)
	defer attempt.cancel()
	conn, err := attempt.connect()
	h.mu.Lock()
	// Stop/Reload can replace the attempt while its HTTP response is arriving.
	// Only the currently owned attempt may publish a connection or an error.
	if h.starting != attempt {
		h.mu.Unlock()
		if conn != nil {
			_ = conn.Close()
		}
		return
	}
	h.starting = nil
	if err == nil {
		err = attempt.ctx.Err()
	}
	if err != nil {
		h.err = err.Error()
		retry, delay := h.armReconnectLocked()
		h.mu.Unlock()
		if conn != nil {
			_ = conn.Close()
		}
		if retry != nil {
			go h.reconnectAfter(retry, delay)
			h.log.Warn("remote host connect failed", "err", err, "retry_in", delay.String())
		} else {
			h.log.Warn("remote host connect failed", "err", err)
		}
		return
	}
	if origin, originErr := config.RemoteOrigin(attempt.config.HubURL); originErr == nil {
		if err := h.cfg.BindRemoteOrigin(attempt.config.Token, origin); err != nil {
			// The dial succeeded, but the saved credential belongs to another
			// gateway. Retrying would repeat that refusal.
			h.err = "remote: connection credential origin changed"
			h.mu.Unlock()
			_ = conn.Close()
			return
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	links := &hostLinkGroup{ctx: ctx, cancel: cancel, current: make(map[string]*hostLink)}
	h.conn = conn
	h.links = links
	h.reconnectNext = 0
	conn.OnLink(func(l *client.Link) { h.acceptLink(links, l) })
	h.id = attempt.config.Identity
	h.err = ""
	h.mu.Unlock()
}

// armReconnectLocked records one backoff. The caller starts it after releasing
// the host lock so the waiter cannot deadlock on that lock.
func (h *Host) armReconnectLocked() (*hostReconnect, time.Duration) {
	if h.reconnect != nil {
		return nil, 0
	}
	delay := h.reconnectNext
	gap := h.reconnectGap
	if gap <= 0 {
		gap = hostReconnectStart
	}
	if delay <= 0 {
		delay = gap
	}
	limit := hostReconnectMax
	if h.reconnectGap > 0 {
		limit = h.reconnectGap
	}
	next := delay * 2
	if next > limit {
		next = limit
	}
	h.reconnectNext = next
	ctx, cancel := context.WithCancel(context.Background())
	retry := &hostReconnect{ctx: ctx, cancel: cancel, done: make(chan struct{})}
	h.reconnect = retry
	return retry, delay
}

func (h *Host) reconnectAfter(retry *hostReconnect, delay time.Duration) {
	defer close(retry.done)
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-retry.ctx.Done():
		return
	case <-timer.C:
	}
	h.mu.Lock()
	if h.reconnect != retry {
		h.mu.Unlock()
		return
	}
	h.reconnect = nil
	attempt, launch := h.prepareStartLocked(context.Background())
	h.mu.Unlock()
	if launch {
		h.connect(attempt)
	}
}

func (s *hostStart) connect() (*client.Conn, error) {
	cfg := s.config
	if err := client.RegisterHostMeta(s.ctx, cfg.HTTPClient, cfg.HubURL, cfg.Token, cfg.Name, cfg.Version, cfg.Identity); err != nil {
		if s.private {
			return nil, errors.New("remote: authorized host registration failed")
		}
		return nil, fmt.Errorf("register remote host: %w", err)
	}
	conn, err := dialHost(s.ctx, cfg)
	if err != nil {
		if s.private {
			return nil, errors.New("remote: authorized host connection failed")
		}
		return nil, fmt.Errorf("connect remote host: %w", err)
	}
	return conn, nil
}

func dialHost(ctx context.Context, cfg client.Config) (*client.Conn, error) {
	type result struct {
		conn *client.Conn
		err  error
	}
	finished := make(chan result)
	go func() {
		conn, err := client.Dial(ctx, cfg)
		// The dependency's initial WebSocket response read is deadline-bounded
		// but does not wake on cancellation. Its late connection is still ours.
		select {
		case finished <- result{conn, err}:
		case <-ctx.Done():
			if conn != nil {
				_ = conn.Close()
			}
		}
	}()
	select {
	case got := <-finished:
		return got.conn, got.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
