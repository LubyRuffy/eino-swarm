package main

import (
	"errors"
	"io"
	"net/url"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/lease"
	"github.com/LubyRuffy/eino-swarm/internal/server"
)

// ensureDesktopEngine returns only after a live presence hold is acknowledged.
// The native process owns it until window shutdown. Reloading the WebView
// cannot drop it; the frontend skips its own hold when that id is in the URL.
func ensureDesktopEngine(dataDir string, mock bool) (string, *desktopPresence, error) {
	cfg, err := config.Load(dataDir)
	if err != nil {
		return "", nil, err
	}
	var deadline time.Time
	var previous error
	for {
		if !deadline.IsZero() && !time.Now().Before(deadline) {
			return "", nil, previous
		}
		holder, err := ensureEngineRecord(cfg.DataDir(), "127.0.0.1:0", mock)
		if err != nil {
			return "", nil, err
		}
		held, err := holdDesktopPresence(holder.URL)
		if err == nil {
			return holder.URL, held, nil
		}
		var transport *url.Error
		if !errors.Is(err, server.ErrEngineStopping) && !errors.As(err, &transport) &&
			!errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
			return "", nil, err
		}
		if deadline.IsZero() {
			deadline = time.Now().Add(engineBootWait)
		}
		previous = err
		if !waitDesktopEngineDeparture(cfg.DataDir(), holder, mock, deadline) {
			return "", nil, err
		}
	}
}

// Waiting never stops the recorded holder. Only observed disappearance or a
// different holder permits another full identity/lease check by ensureEngine.
func waitDesktopEngineDeparture(dir string, holder lease.Record, mock bool, deadline time.Time) bool {
	for {
		rec, err := lease.Discover(dir, mock)
		// A replacement may reuse the same port. Keep the complete lease
		// identity that ensureEngine validated before the failed handshake.
		if errors.Is(err, lease.ErrNotRunning) || (rec.PID > 0 && rec != holder) {
			return true
		}
		if errors.Is(err, lease.ErrMockMismatch) || !time.Now().Before(deadline) {
			return false
		}
		time.Sleep(50 * time.Millisecond)
	}
}
