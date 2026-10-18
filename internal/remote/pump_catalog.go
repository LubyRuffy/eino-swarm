package remote

import (
	"context"

	"github.com/LubyRuffy/eino-swarm/internal/config"
)

// Discovery must not keep the receive loop from handling Stop or live messages.
func (p *LinkPump) startCatalogRefresh(req Request) {
	p.catalogMu.Lock()
	if p.catalogClosed {
		p.catalogMu.Unlock()
		return
	}
	if req.V != 0 && req.V != ProtocolV {
		p.catalogMu.Unlock()
		p.reply(fail(req.ID, p.path, p.sessionID, "bad_version", "unsupported protocol version"))
		return
	}
	if p.catalogCancel != nil {
		p.catalogMu.Unlock()
		p.reply(fail(req.ID, p.path, p.sessionID, "busy", "model refresh already running"))
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), config.DefaultDiscoverTimeout)
	p.catalogCancel = cancel
	p.catalogWG.Add(1)
	p.catalogMu.Unlock()
	go func() {
		defer p.catalogWG.Done()
		defer cancel()
		defer func() {
			p.catalogMu.Lock()
			p.catalogCancel = nil
			p.catalogMu.Unlock()
		}()
		resp := handleCatalogRefresh(ctx, p.eng, req, p.path, p.sessionID)
		p.catalogMu.Lock()
		closed := p.catalogClosed
		p.catalogMu.Unlock()
		if !closed {
			p.reply(resp)
		}
	}()
}

func (p *LinkPump) stopCatalogRefresh() {
	p.catalogMu.Lock()
	p.catalogClosed = true
	cancel := p.catalogCancel
	p.catalogMu.Unlock()
	if cancel != nil {
		cancel()
	}
	p.catalogWG.Wait()
}
