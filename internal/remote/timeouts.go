package remote

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"strings"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/engine"
)

var errTimeoutReplyTooLarge = errors.New("timeout settings exceed frame budget")

func handleTimeoutSettings(e *engine.Engine, req Request, path, sessionID string) Response {
	if req.timeoutInvalid {
		return fail(req.ID, path, sessionID, "bad_request", "invalid timeout settings")
	}
	resp := okBase(req.ID, path, sessionID)
	capture := func(c *config.Config) error {
		v := c.Timeouts()
		resp.TimeoutSettings = &v
		if !timeoutReplyFits(resp) {
			return errTimeoutReplyTooLarge
		}
		return nil
	}
	if strings.TrimSpace(req.Op) == OpSetTimeouts {
		var patch config.TimeoutPatch
		d := json.NewDecoder(bytes.NewReader(req.TimeoutSettings))
		d.DisallowUnknownFields()
		if err := d.Decode(&patch); err != nil {
			return fail(req.ID, path, sessionID, "bad_request", "invalid timeout settings")
		}
		var trailing any
		if err := d.Decode(&trailing); err != io.EOF {
			return fail(req.ID, path, sessionID, "bad_request", "invalid timeout settings")
		}
		err := e.UpdateConfig(func(next *config.Config) error {
			if err := config.ApplyTimeoutPatch(next, patch); err != nil {
				return err
			}
			return capture(next)
		})
		if errors.Is(err, errTimeoutReplyTooLarge) {
			return fail(req.ID, path, sessionID, "too_large", "timeout settings do not fit one frame")
		}
		if errors.Is(err, config.ErrTimeoutSettings) {
			return fail(req.ID, path, sessionID, "bad_request", "invalid timeout settings")
		}
		if err != nil {
			return fail(req.ID, path, sessionID, "save_failed", "could not save timeout settings")
		}
	}
	if strings.TrimSpace(req.Op) == OpGetTimeouts {
		if err := capture(e.Config()); err != nil {
			return fail(req.ID, path, sessionID, "too_large", "timeout settings do not fit one frame")
		}
	}
	return resp
}

func timeoutReplyFits(resp Response) bool {
	raw, _ := json.Marshal(resp)
	return len(raw) <= maxCatalogFrame
}

// Only the new settings operations are strict; older RPC payloads stay additive.
func (r *Request) UnmarshalJSON(raw []byte) error {
	type plain Request
	var decoded plain
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return err
	}
	*r = Request(decoded)
	if strings.TrimSpace(r.Op) != OpGetTimeouts && strings.TrimSpace(r.Op) != OpSetTimeouts {
		return nil
	}
	var narrow struct {
		V        int             `json:"v"`
		ID       string          `json:"id"`
		Op       string          `json:"op"`
		Settings json.RawMessage `json:"timeout_settings"`
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	r.timeoutInvalid = d.Decode(&narrow) != nil || (strings.TrimSpace(r.Op) == OpGetTimeouts && len(narrow.Settings) > 0)
	return nil
}
