package remote

import (
	"context"
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
	id       *crypto.Identity
	cancel   context.CancelFunc
	offerURI string
	offerAt  time.Time
	err      string

	// keepAlive is forwarded to pairlink. Zero is the library default.
	// Tests stretch it so a short hub Idle can drop the socket.
	keepAlive time.Duration
	// version is this PC build. The hub stores it; this package does not invent one.
	version string

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
	defer h.mu.Unlock()
	h.stopLocked()
	h.startLocked()
	h.applyWakeLocked()
}

func (h *Host) Start() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.startLocked()
	h.applyWakeLocked()
}

func (h *Host) startLocked() {
	if h.conn != nil {
		return
	}
	cfg := h.cfg
	if cfg == nil || !cfg.Remote.Enabled {
		return
	}
	hub := cfg.Remote.HubURL
	if hub == "" {
		h.err = "remote needs hub_url"
		return
	}
	token, err := cfg.EnsureHostToken()
	if err != nil {
		h.err = err.Error()
		return
	}
	id, err := loadIdentity(cfg)
	if err != nil {
		h.err = err.Error()
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	// The gateway list reads this label and the build string. Neither is the hub URL.
	name := config.SeedRemoteDisplayName(cfg.Remote.DisplayName)
	if err := client.RegisterHostMeta(ctx, nil, hub, token, name, h.version, id); err != nil {
		cancel()
		h.err = err.Error()
		h.log.Warn("remote host register failed", "err", err)
		return
	}
	conn, err := client.Dial(ctx, client.Config{
		HubURL:    hub,
		Identity:  id,
		Token:     token,
		KeepAlive: h.keepAlive,
		Name:      name,
		Version:   h.version,
	})
	if err != nil {
		cancel()
		h.err = err.Error()
		h.log.Warn("remote host dial failed", "err", err)
		return
	}
	conn.OnLink(func(l *client.Link) {
		go h.serveLink(l)
	})
	h.conn = conn
	h.id = id
	h.cancel = cancel
	h.err = ""
}

func (h *Host) Stop() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.stopLocked()
	if h.wake != nil {
		_ = h.wake.Set(false)
	}
}

func (h *Host) applyWakeLocked() {
	if h.wake == nil {
		return
	}
	enabled, keep := false, false
	if h.cfg != nil {
		enabled = h.cfg.Remote.Enabled
		keep = h.cfg.Remote.KeepAwake
	}
	if err := h.wake.Set(wakeup.Wanted(enabled, keep)); err != nil {
		h.log.Warn("keep-awake failed", "err", err)
	}
}

func (h *Host) stopLocked() {
	if h.cancel != nil {
		h.cancel()
		h.cancel = nil
	}
	if h.conn != nil {
		_ = h.conn.Close()
		h.conn = nil
	}
	h.offerURI = ""
}

func (h *Host) serveLink(l *client.Link) {
	h.log.Info("remote device linked", "path", l.Path(), "session", l.SessionID())
	pump := newLinkPump(h.eng, h.cfg.Remote, l)
	defer pump.Close()
	for msg := range l.Recv() {
		pump.Dispatch(msg)
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
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.conn == nil {
		h.startLocked()
		h.applyWakeLocked()
	}
	if h.conn == nil {
		if h.err != "" {
			return nil, fmt.Errorf("%s", h.err)
		}
		return nil, fmt.Errorf("remote is offline")
	}
	h.waitConnectedLocked(time.Now().Add(2 * time.Second))
	if h.conn == nil || !h.conn.Connected() {
		if h.err != "" {
			return nil, fmt.Errorf("%s", h.err)
		}
		return nil, fmt.Errorf("remote is offline")
	}
	uri, pairingID, err := h.conn.CreateOffer(ctx)
	if err != nil {
		return nil, err
	}
	png, err := qr.PNG(uri, 320)
	if err != nil {
		return nil, err
	}
	h.offerURI = uri
	h.offerAt = time.Now()
	exp := h.offerAt.Add(time.Duration(protocol.DefaultPairingTTL) * time.Second)
	return &Offer{URI: uri, PairingID: pairingID, ExpiresAt: exp, PNG: png}, nil
}

type Status struct {
	Enabled     bool   `json:"enabled"`
	HubURL      string `json:"hub_url"`
	HasToken    bool   `json:"has_token"`
	Online      bool   `json:"online"`
	KeepAwake   bool   `json:"keep_awake"`
	Awake       bool   `json:"awake"`
	Fingerprint string `json:"fingerprint,omitempty"`
	Error       string `json:"error,omitempty"`
}

func (h *Host) Status() Status {
	h.mu.Lock()
	defer h.mu.Unlock()
	st := Status{
		Enabled:   h.cfg.Remote.Enabled,
		HubURL:    h.cfg.Remote.HubURL,
		HasToken:  h.cfg.HasHostToken(),
		Online:    h.conn != nil && h.conn.Connected(),
		KeepAwake: h.cfg.Remote.KeepAwake,
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
	token, err := h.cfg.HostToken()
	if err != nil {
		return nil, err
	}
	if token == "" || h.cfg.Remote.HubURL == "" {
		return []Binding{}, nil
	}
	raw, err := client.ListBindings(ctx, h.cfg.Remote.HubURL, token)
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
	token, err := h.cfg.HostToken()
	if err != nil {
		return err
	}
	return client.RevokeBinding(ctx, h.cfg.Remote.HubURL, token, id)
}
