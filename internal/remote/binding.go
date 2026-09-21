package remote

import (
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/LubyRuffy/pairlink/client"
)

// Binding is a hub row plus what this PC has learned about the phone.
// The hub still only has a fingerprint; device/last_seen come from hello.
type Binding struct {
	ID        string `json:"id"`
	DeviceFP  string `json:"device_fp"`
	Device    string `json:"device,omitempty"`
	CreatedAt string `json:"created_at"`
	LastSeen  string `json:"last_seen,omitempty"`
	SessionID string `json:"session_id"`
}

func decorateBindings(rows []client.BindingView, devices []store.RemoteDevice) []Binding {
	byFP := make(map[string]store.RemoteDevice, len(devices))
	for _, d := range devices {
		byFP[d.DeviceFP] = d
	}
	out := make([]Binding, 0, len(rows))
	for _, r := range rows {
		b := Binding{
			ID: r.ID, DeviceFP: r.DeviceFP,
			CreatedAt: r.CreatedAt, SessionID: r.SessionID,
		}
		if d, ok := byFP[r.DeviceFP]; ok {
			b.Device = d.Label
			if !d.SeenAt.IsZero() {
				b.LastSeen = d.SeenAt.UTC().Format("2006-01-02T15:04:05Z")
			}
		}
		out = append(out, b)
	}
	return out
}
