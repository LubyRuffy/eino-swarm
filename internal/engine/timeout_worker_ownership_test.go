package engine

import (
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/config"
)

// Saving phone timeouts cannot race the current goal's worker ownership.
func TestTimeoutWorkerOwnershipRemainsValidDuringSwarmSettingsUpdates(t *testing.T) {
	e := newTestEngine(t)
	th, err := e.CreateThread("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := e.store.UpdateThread(th.ID, map[string]any{"goal": "continue the assigned work"}); err != nil {
		t.Fatal(err)
	}
	rt := e.runtimeFor(th.ID)
	done := make(chan error, 1)
	go func() {
		for i := 0; i < 100; i++ {
			if err := e.cfg.Update(func(c *config.Config) error { c.Swarm.AgentTimeoutSeconds = 40 + i%2; return nil }); err != nil {
				done <- err
				return
			}
		}
		done <- nil
	}()
	for i := 0; i < 600; i++ {
		kind, id := rt.workerOwner()
		if kind != "goal" || id != th.ID {
			t.Errorf("timeout save lost worker owner: %s/%s", kind, id)
			break
		}
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
