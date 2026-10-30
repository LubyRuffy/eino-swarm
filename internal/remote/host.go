package remote

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/engine"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/LubyRuffy/eino-swarm/internal/wakeup"
	"github.com/LubyRuffy/pairlink/client"
	"github.com/LubyRuffy/pairlink/crypto"
	"github.com/LubyRuffy/pairlink/protocol"
	"github.com/LubyRuffy/pairlink/qr"
)

// Host is the PC side of pairlink: QR offers, sealed RPC, path metadata.
type Host struct {
	eng *engine.Engine
	cfg *config.Config
	log *slog.Logger

	mu       sync.Mutex
	conn     *client.Conn
	links    *hostLinkGroup
	id       *crypto.Identity
	starting *hostStart
	offerURI string
	offerAt  time.Time
	err      string

	// keepAlive is forwarded to pairlink. Zero is the library default.
	// Tests stretch it so a short hub Idle can drop the socket.
	keepAlive time.Duration
	// version is this PC build. The hub stores it; this package does not invent one.
	version     string
	loginActive *loginAttempt
	login       LoginStatus
	loginWait   func(context.Context, time.Duration) error
	// afterLoginActivation synchronizes the private-file handoff regression.
	afterLoginActivation func()

	// wake is the sleep assertion. It follows config, not the hub
	// socket: Reload must not release and re-acquire or idle sleep
	// wins the gap.
	wake wakeup.Holder
}

// SetVersion is the PC build string published on register and TypeLabel.
// Call it before Start. An empty string leaves a version the hub already has.
func (h *Host) SetVersion(version string) {
	if h == nil {
		return
	}
	h.mu.Lock()
	h.version = strings.TrimSpace(version)
	h.mu.Unlock()
}

func New(eng *engine.Engine, cfg *config.Config, log *slog.Logger) *Host {
	if log == nil {
		log = slog.Default()
	}
	return &Host{eng: eng, cfg: cfg, log: log, wake: wakeup.ForProcess()}
}

func (h *Host) Reload() {
	h.mu.Lock()
	login := h.cancelLoginLocked()
	h.login = LoginStatus{}
	previous := h.stopLocked()
	attempt, launch := h.prepareStartLocked(context.Background())
	h.applyWakeLocked()
	h.mu.Unlock()
	finishLogin(login)
	previous.finish()
	h.finishStart(attempt, launch)
}

// Start connects before returning. Concurrent callers share the pending attempt.
func (h *Host) Start() {
	h.mu.Lock()
	attempt, launch := h.prepareStartLocked(context.Background())
	h.applyWakeLocked()
	h.mu.Unlock()
	h.finishStart(attempt, launch)
}

func (h *Host) Stop() {
	h.mu.Lock()
	login := h.cancelLoginLocked()
	h.login = LoginStatus{State: "cancelled"}
	previous := h.stopLocked()
	if h.wake != nil {
		_ = h.wake.Set(false)
	}
	h.mu.Unlock()
	finishLogin(login)
	previous.finish()
}

func (h *Host) applyWakeLocked() {
	if h.wake == nil {
		return
	}
	enabled, keep := false, false
	if h.cfg != nil {
		cfg := h.cfg.Snapshot()
		enabled = cfg.Remote.Enabled
		keep = cfg.Remote.KeepAwake
	}
	if err := h.wake.Set(wakeup.Wanted(enabled, keep)); err != nil {
		h.log.Warn("keep-awake failed", "err", err)
	}
}

type Offer struct {
	URI       string    `json:"uri"`
	PairingID string    `json:"pairing_id,omitempty"`
	ExpiresAt time.Time `json:"expires_at"`
	PNG       []byte    `json:"-"`
}

func (h *Host) waitConnectedLocked(deadline time.Time) {
	for time.Now().Before(deadline) {
		if h.conn != nil && h.conn.Connected() {
			return
		}
		h.mu.Unlock()
		time.Sleep(40 * time.Millisecond)
		h.mu.Lock()
	}
}

func (h *Host) Offer(ctx context.Context) (*Offer, error) {
	if err := h.startForOffer(ctx); err != nil {
		return nil, err
	}
	h.mu.Lock()
	h.waitConnectedLocked(time.Now().Add(2 * time.Second))
	if h.conn == nil || !h.conn.Connected() {
		message := h.err
		if message == "" {
			message = "remote is offline"
		}
		h.mu.Unlock()
		return nil, fmt.Errorf("%s", message)
	}
	conn := h.conn
	remoteConfig := h.cfg.Snapshot().Remote
	h.mu.Unlock()
	// The HTTP request may outlive a hub or mode change. Stop and login
	// cancellation must not wait for its network deadline while holding mu.
	uri, pairingID, err := conn.CreateOffer(ctx)
	if err != nil {
		if remoteConfig.AuthMode == config.RemoteAuthNOSEC {
			return nil, errors.New("remote: pairing offer unavailable")
		}
		return nil, err
	}
	png, err := qr.PNG(uri, 320)
	if err != nil {
		return nil, err
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	current := h.cfg.Snapshot().Remote
	if h.conn != conn || !conn.Connected() || current.Enabled != remoteConfig.Enabled || current.HubURL != remoteConfig.HubURL || current.AuthMode != remoteConfig.AuthMode {
		return nil, errors.New("remote: connection changed during pairing")
	}
	h.offerURI = uri
	h.offerAt = time.Now()
	exp := h.offerAt.Add(time.Duration(protocol.DefaultPairingTTL) * time.Second)
	return &Offer{URI: uri, PairingID: pairingID, ExpiresAt: exp, PNG: png}, nil
}

type Status struct {
	AuthMode       string       `json:"auth_mode"`
	Login          *LoginStatus `json:"login,omitempty"`
	LoginAvailable bool         `json:"login_available"`
	Enabled        bool         `json:"enabled"`
	HubURL         string       `json:"hub_url"`
	HasToken       bool         `json:"has_token"`
	Online         bool         `json:"online"`
	KeepAwake      bool         `json:"keep_awake"`
	Awake          bool         `json:"awake"`
	Fingerprint    string       `json:"fingerprint,omitempty"`
	Error          string       `json:"error,omitempty"`
}

func (h *Host) Status() Status {
	h.mu.Lock()
	defer h.mu.Unlock()
	cfg := h.cfg.Snapshot()
	st := Status{
		AuthMode: cfg.Remote.AuthMode, Login: h.loginStatusLocked(cfg),
		Enabled:   cfg.Remote.Enabled,
		HubURL:    cfg.Remote.HubURL,
		HasToken:  cfg.HasHostToken(),
		Online:    h.conn != nil && h.conn.Connected(),
		KeepAwake: cfg.Remote.KeepAwake,
		Error:     h.err,
	}
	if h.wake != nil {
		st.Awake = h.wake.On()
	}
	if h.id != nil {
		st.Fingerprint = h.id.Fingerprint()
	}
	return st
}

func (h *Host) ListBindings(ctx context.Context) ([]Binding, error) {
	cfg := h.cfg.Snapshot()
	token, err := cfg.HostToken()
	if errors.Is(err, config.ErrRemoteApprovalRequired) {
		return []Binding{}, nil
	}
	if err != nil {
		return nil, err
	}
	if token == "" || cfg.Remote.HubURL == "" {
		return []Binding{}, nil
	}
	var raw []client.BindingView
	if cfg.Remote.AuthMode == config.RemoteAuthNOSEC {
		raw, err = privateBindings(ctx, cfg.Remote.HubURL, token)
	} else {
		raw, err = client.ListBindings(ctx, cfg.Remote.HubURL, token)
	}
	if err != nil {
		return nil, err
	}
	var devices []store.RemoteDevice
	if h.eng != nil {
		devices, _ = h.eng.Store().ListRemoteDevices()
	}
	return decorateBindings(raw, devices), nil
}

func (h *Host) RevokeBinding(ctx context.Context, id string) error {
	cfg := h.cfg.Snapshot()
	token, err := cfg.HostToken()
	if err != nil {
		return err
	}
	if cfg.Remote.AuthMode == config.RemoteAuthNOSEC {
		return privateRevokeBinding(ctx, cfg.Remote.HubURL, token, id)
	}
	return client.RevokeBinding(ctx, cfg.Remote.HubURL, token, id)
}
