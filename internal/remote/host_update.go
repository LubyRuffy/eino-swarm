package remote

import (
	"context"
	"errors"
	"strings"

	"github.com/LubyRuffy/eino-swarm/internal/update"
)

type hostUpdateOffer struct {
	Status  string             `json:"status"`
	Current string             `json:"current,omitempty"`
	Message string             `json:"message,omitempty"`
	Offer   *hostUpdateVersion `json:"offer,omitempty"`
}

type hostUpdateVersion struct {
	Version string `json:"version"`
}

// HostUpdateTrial verifies a challenge over the initiating phone's current
// encrypted connection. A nil callback allows the normal RPC surface.
type HostUpdateTrial func(operationID, challenge, peer string) (string, error)

// SetHostUpdateTrial holds work admission until the stable supervisor promotes
// the candidate. Existing links observe the same gate without reconnecting.
func (h *Host) SetHostUpdateTrial(trial HostUpdateTrial) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.updateTrial = trial
}

// SetHostUpdater supplies the installed host's update capability to paired
// links. Setting it after bind does not require reconnecting an active phone.
func (h *Host) SetHostUpdater(service *update.HostService) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.hostUpdater = service
}

func (p *LinkPump) withHostUpdateMeta(resp Response) Response {
	if resp.OK && p.hostUpdateAccess != nil {
		service, version, current := p.hostUpdateAccess()
		if current {
			resp.HostVersion = version
			resp.HostUpdateSupported = service.Supported()
		}
	}
	return resp
}

func (p *LinkPump) dispatchHostUpdate(req Request) {
	if req.V != 0 && req.V != ProtocolV {
		p.reply(fail(req.ID, p.path, p.sessionID, "bad_version", "unsupported protocol version"))
		return
	}
	if p.hostUpdateAccess == nil {
		p.reply(fail(req.ID, p.path, p.sessionID, "link_required", "host updates require a paired phone"))
		return
	}
	service, version, authorized := p.hostUpdateAccess()
	if !authorized {
		p.reply(hostUpdateFailure(req, p.path, p.sessionID, update.ErrHostUpdateUnauthorized))
		return
	}
	if service == nil {
		p.reply(hostUpdateFailure(req, p.path, p.sessionID, update.ErrHostUpdateUnsupported))
		return
	}
	op := strings.TrimSpace(req.Op)
	if err := requireStartupReady(p.eng, op); err != nil {
		p.reply(mapErr(req.ID, p.path, p.sessionID, err))
		return
	}
	if op == OpHostUpdateCheck {
		p.startHostUpdateCheck(req, service)
		return
	}
	var info update.HostUpdate
	var err error
	if op == OpHostUpdateStart {
		peer := ""
		if req.HostUpdateVerify {
			peer = p.deviceFP
		}
		info, err = service.StartFor(req.OperationID, req.Version, peer, func() bool {
			currentService, _, current := p.hostUpdateAccess()
			return current && currentService == service
		})
	} else {
		info, err = service.Status(req.OperationID)
	}
	if err != nil {
		p.reply(hostUpdateFailure(req, p.path, p.sessionID, err))
		return
	}
	resp := okBase(req.ID, p.path, p.sessionID)
	resp.HostUpdate, resp.HostVersion = &info, version
	resp.Startup, _ = p.eng.StartupStatus()
	if op == OpHostUpdateStatus && p.hostUpdateTrial != nil {
		if trial := p.hostUpdateTrial(); trial != nil {
			if currentService, _, current := p.hostUpdateAccess(); !current || currentService != service {
				p.reply(hostUpdateFailure(req, p.path, p.sessionID, update.ErrHostUpdateUnauthorized))
				return
			}
			challenge, err := trial(req.OperationID, req.HostUpdateChallenge, p.deviceFP)
			if err != nil {
				p.reply(hostUpdateFailure(req, p.path, p.sessionID, err))
				return
			}
			resp.HostUpdateChallenge = challenge
		}
	}
	p.reply(resp)
}

func hostUpdateFailure(req Request, path, sessionID string, err error) Response {
	var code string
	switch {
	case errors.Is(err, update.ErrHostUpdateNotFound):
		code = "not_found"
	case errors.Is(err, update.ErrHostUpdateUnsupported):
		code = "update_unsupported"
	case errors.Is(err, update.ErrHostUpdateUnauthorized):
		code = "link_required"
	case errors.Is(err, update.ErrBusy):
		code = "update_busy"
	case errors.Is(err, update.ErrHostUpdateInput):
		code = "bad_request"
	default:
		return fail(req.ID, path, sessionID, "update_unavailable", "Could not read or save the host update.")
	}
	return fail(req.ID, path, sessionID, code, err.Error())
}

func (p *LinkPump) startHostUpdateCheck(req Request, service *update.HostService) {
	p.mu.Lock()
	if p.hostCheckClosed || p.hostCheckCancel != nil {
		p.mu.Unlock()
		p.reply(hostUpdateFailure(req, p.path, p.sessionID, update.ErrBusy))
		return
	}
	ctx, cancel := context.WithCancel(p.hostUpdateContext)
	p.hostCheckCancel = cancel
	p.hostCheckWG.Add(1)
	p.mu.Unlock()
	go func() {
		defer p.hostCheckWG.Done()
		defer cancel()
		result := service.Check(ctx, req.Fresh)
		p.mu.Lock()
		p.hostCheckCancel = nil
		p.mu.Unlock()
		if ctx.Err() != nil {
			return
		}
		offer := &hostUpdateOffer{Status: result.Status, Current: result.Current, Message: result.Message}
		if result.Offer != nil {
			offer.Offer = &hostUpdateVersion{Version: result.Offer.Version}
		}
		resp := okBase(req.ID, p.path, p.sessionID)
		resp.HostUpdateOffer = offer
		p.reply(resp)
	}()
}

func (p *LinkPump) stopHostUpdateCheck() {
	p.mu.Lock()
	p.hostCheckClosed = true
	if p.hostCheckCancel != nil {
		p.hostCheckCancel()
	}
	p.mu.Unlock()
	p.hostCheckWG.Wait()
}
