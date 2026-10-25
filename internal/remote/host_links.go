package remote

import (
	"context"
	"sync"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/pairlink/client"
)

// The dependency replaces its peer→Link entry without closing the old inbox.
// Host owns that missing lifecycle, including superseded links still cleaning up.
// Admission and current are protected by Host.mu; waiting never holds that lock.
type hostLinkGroup struct {
	ctx     context.Context
	cancel  context.CancelFunc
	current map[string]*hostLink
	wg      sync.WaitGroup
}

type hostLink struct {
	ctx    context.Context
	cancel context.CancelFunc
	done   chan struct{}
}

type hostStop struct {
	attempt *hostStart
	conn    *client.Conn
	links   *hostLinkGroup
}

func (h *Host) stopLocked() hostStop {
	previous := hostStop{attempt: h.starting, conn: h.conn, links: h.links}
	h.starting, h.conn, h.links = nil, nil, nil
	h.offerURI = ""
	if previous.attempt != nil {
		previous.attempt.cancel()
	}
	if previous.links != nil {
		previous.links.cancel()
	}
	return previous
}

func (s hostStop) finish() {
	if s.conn != nil {
		s.conn.OnLink(nil)
		_ = s.conn.Close()
	}
	if s.attempt != nil {
		<-s.attempt.done
	}
	if s.links != nil {
		s.links.wg.Wait()
	}
}

func (h *Host) acceptLink(owner *hostLinkGroup, l *client.Link) {
	h.mu.Lock()
	// OnLink(nil) cannot retract a callback already captured by pairlink. An
	// old connection must not install a pump after Stop or into a new Reload.
	if h.links != owner || owner.ctx.Err() != nil {
		h.mu.Unlock()
		return
	}
	peer := string(l.PeerPub)
	if old := owner.current[peer]; old != nil {
		old.cancel()
	}
	ctx, cancel := context.WithCancel(owner.ctx)
	active := &hostLink{ctx: ctx, cancel: cancel, done: make(chan struct{})}
	owner.current[peer] = active
	owner.wg.Add(1)
	cfg := h.cfg.Remote
	h.mu.Unlock()
	go h.serveLink(owner, peer, active, l, cfg)
}

func (h *Host) serveLink(owner *hostLinkGroup, peer string, active *hostLink, l *client.Link, cfg config.RemoteConfig) {
	defer func() {
		active.cancel()
		h.mu.Lock()
		if owner.current[peer] == active {
			delete(owner.current, peer)
		}
		h.mu.Unlock()
		close(active.done)
		owner.wg.Done()
	}()
	if active.ctx.Err() != nil {
		return
	}
	h.log.Info("remote device linked", "path", l.Path(), "session", l.SessionID())
	pump := newLinkPump(h.eng, cfg, l)
	// A watch may already be waiting for sendMu when cancellation wins. Check
	// after that lock too, before entering the old Link's transport. A write
	// already in the dependency remains governed by its own write deadline.
	send := pump.send
	pump.send = func(raw []byte) error {
		if err := active.ctx.Err(); err != nil {
			return err
		}
		return send(raw)
	}
	defer pump.Close()
	for {
		select {
		case <-active.ctx.Done():
			return
		case msg, ok := <-l.Recv():
			if !ok || active.ctx.Err() != nil {
				return
			}
			pump.Dispatch(msg)
		}
	}
}
