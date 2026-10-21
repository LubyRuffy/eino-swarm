package remote

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

func TestPhoneProjectionKeepsOutputIdentityWhileClippingText(t *testing.T) {
	var ev store.Event
	if err := json.Unmarshal([]byte(`{"seq":7,"kind":"reasoning","message_id":"output","text":"checking the current evidence"}`), &ev); err != nil {
		t.Fatal(err)
	}
	view := eventView(ev, config.RemoteConfig{EventChars: 8})
	raw, err := json.Marshal(view)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"message_id":"output"`) || view.Seq != 7 || view.Text == ev.Text {
		t.Fatalf("phone projection lost identity/sequence or did not clip: %s", raw)
	}
}
