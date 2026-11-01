package remote

import (
	"encoding/json"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/config"
)

func TestPhoneWebExecutionPreferencePersistsWithoutChangingPCSettings(t *testing.T) {
	eng := testEngine(t)
	thread, err := eng.CreateThread("Phone preference", "", "")
	if err != nil {
		t.Fatal(err)
	}
	original := eng.Config().Snapshot().Tools
	raw, err := json.Marshal(map[string]any{"id": "set-web", "op": "tune", "thread_id": thread.ID, "web_execution": "local"})
	if err != nil {
		t.Fatal(err)
	}
	var req Request
	if err := json.Unmarshal(raw, &req); err != nil {
		t.Fatal(err)
	}
	if response := Handle(eng, config.RemoteConfig{}, req, "relay", "session"); !response.OK {
		t.Fatalf("phone preference failed: %+v", response)
	}
	stored, err := eng.Store().GetThread(thread.ID)
	if err != nil {
		t.Fatal(err)
	}
	raw, err = json.Marshal(stored)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	if fields["web_execution"] != "local" {
		t.Fatalf("phone execution choice was not persisted: %s", raw)
	}
	if eng.Config().Snapshot().Tools.WebExecution != original.WebExecution {
		t.Fatal("phone preference changed the PC's global setting")
	}
	open := Handle(eng, config.RemoteConfig{}, Request{ID: "open-web", Op: OpOpen, ThreadID: thread.ID}, "relay", "session")
	if !open.OK || open.Detail == nil || open.Detail.WebExecution != config.WebExecutionLocal {
		t.Fatalf("phone could not read its saved preference: %+v", open)
	}
	for _, op := range []string{OpMeta, OpCatalog} {
		if reply := Handle(eng, config.RemoteConfig{}, Request{ID: "support", Op: op}, "relay", "session"); !reply.OK || !reply.WebExecutionSupported {
			t.Fatalf("phone cannot detect Web execution support from %s: %+v", op, reply)
		}
	}
	if reply := Handle(eng, config.RemoteConfig{}, Request{ID: "keep", Op: OpTune, ThreadID: thread.ID}, "relay", "session"); !reply.OK {
		t.Fatal(reply.Error)
	}
	stored, err = eng.Store().GetThread(thread.ID)
	if err != nil || stored.WebExecution != config.WebExecutionLocal {
		t.Fatal("an older phone silently reset the conversation preference")
	}
	invalid := "unknown"
	if reply := Handle(eng, config.RemoteConfig{}, Request{ID: "invalid", Op: OpTune, ThreadID: thread.ID, WebExecution: &invalid}, "relay", "session"); reply.OK {
		t.Fatal("invalid phone preference was accepted")
	}
	stored, err = eng.Store().GetThread(thread.ID)
	if err != nil || stored.WebExecution != config.WebExecutionLocal {
		t.Fatal("rejected preference partially changed the conversation")
	}
	inherit := ""
	if reply := Handle(eng, config.RemoteConfig{}, Request{ID: "inherit", Op: OpTune, ThreadID: thread.ID, WebExecution: &inherit}, "relay", "session"); !reply.OK {
		t.Fatal(reply.Error)
	}
	stored, err = eng.Store().GetThread(thread.ID)
	if err != nil || stored.WebExecution != "" {
		t.Fatal("explicit inheritance did not clear the conversation override")
	}
}
