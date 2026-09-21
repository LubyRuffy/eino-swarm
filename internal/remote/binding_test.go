package remote

import (
	"testing"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/LubyRuffy/pairlink/client"
)

func TestDecorateBindingsJoinsHubRowsOntoLocalLabels(t *testing.T) {
	seen := time.Date(2026, 9, 20, 16, 3, 32, 0, time.UTC)
	got := decorateBindings(
		[]client.BindingView{
			{ID: "b1", DeviceFP: "aa11bb22cc33dd44", CreatedAt: "2026-09-20T16:00:00Z", SessionID: "s1"},
			{ID: "b2", DeviceFP: "deadbeefdeadbeef", CreatedAt: "2026-09-20T16:01:00Z", SessionID: "s2"},
		},
		[]store.RemoteDevice{
			{DeviceFP: "aa11bb22cc33dd44", Label: "Phone 1.0 Device", SeenAt: seen},
			{DeviceFP: "orphan0000000000", Label: "leftover"},
		},
	)
	if len(got) != 2 {
		t.Fatalf("%d", len(got))
	}
	if got[0].Device != "Phone 1.0 Device" || got[0].LastSeen != "2026-09-20T16:03:32Z" {
		t.Fatalf("labeled %+v", got[0])
	}
	if got[1].Device != "" || got[1].LastSeen != "" {
		t.Fatalf("unknown phone must stay a fingerprint: %+v", got[1])
	}
	if decorateBindings(nil, nil) == nil || len(decorateBindings(nil, nil)) != 0 {
		t.Fatal("empty list must be a slice, not null JSON")
	}
}
