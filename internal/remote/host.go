package remote

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/engine"
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
	online   bool
	err      string
}

func New(eng *engine.Engine, cfg *config.Config, log *slog.Logger) *Host {
	if log == nil {
		log = slog.Default()
	}
	return &Host{eng: eng, cfg: cfg, log: log}
}

func (h *Host) Reload() {
	h.Stop()
	h.Start()
}

func (h *Host) Start() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.startLocked()
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
	token, err := cfg.HostToken()
	if err != nil || hub == "" || token == "" {
		h.err = "remote needs hub_url and a Host Token"
		return
	}
	id, err := loadIdentity(cfg)
	if err != nil {
		h.err = err.Error()
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	if err := client.RegisterHost(ctx, hub, token, id); err != nil {
		cancel()
		h.err = err.Error()
		h.log.Warn("remote host register failed", "err", err)
		return
	}
	conn, err := client.Dial(ctx, client.Config{
		HubURL:   hub,
		Identity: id,
		Token:    token,
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
	h.online = true
	h.err = ""
}

func (h *Host) Stop() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.stopLocked()
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
	h.online = false
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

func (h *Host) Offer(ctx context.Context) (*Offer, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.conn == nil {
		h.startLocked()
	}
	if h.conn == nil {
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
	Fingerprint string `json:"fingerprint,omitempty"`
	Error       string `json:"error,omitempty"`
}

func (h *Host) Status() Status {
	h.mu.Lock()
	defer h.mu.Unlock()
	st := Status{
		Enabled:  h.cfg.Remote.Enabled,
		HubURL:   h.cfg.Remote.HubURL,
		HasToken: h.cfg.HasHostToken(),
		Online:   h.online,
		Error:    h.err,
	}
	if h.id != nil {
		st.Fingerprint = h.id.Fingerprint()
	}
	return st
}

func (h *Host) ListBindings(ctx context.Context) ([]client.BindingView, error) {
	token, err := h.cfg.HostToken()
	if err != nil {
		return nil, err
	}
	if token == "" || h.cfg.Remote.HubURL == "" {
		return []client.BindingView{}, nil
	}
	return client.ListBindings(ctx, h.cfg.Remote.HubURL, token)
}

func (h *Host) RevokeBinding(ctx context.Context, id string) error {
	token, err := h.cfg.HostToken()
	if err != nil {
		return err
	}
	return client.RevokeBinding(ctx, h.cfg.Remote.HubURL, token, id)
}
