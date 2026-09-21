package swarm

import (
	"context"
	"strings"
	"testing"
)

func TestCloseAlreadyFinishedIsNotCancelled(t *testing.T) {
	// A long /goal plants leftover finished workers so resume_agent still
	// resolves the id. cancelled:true on those made the manager treat one
	// close as a canary and walk the rest of the roster.
	reg := NewRegistry()
	if err := reg.PlantFinished(FinishedWorker{ID: "helper-1", Role: "helper", Result: "already on disk"}); err != nil {
		t.Fatal(err)
	}
	closeT := invokable(t, reg.Tools()[3])
	out, err := closeT.InvokableRun(context.Background(), `{"agent_id":"helper-1"}`)
	if err != nil {
		t.Fatalf("close_agent: %v", err)
	}
	if strings.Contains(out, `"cancelled":true`) || !strings.Contains(out, `"already_finished":true`) {
		t.Fatalf("a leftover finished worker must not look cancelled: %s", out)
	}
	h, ok := reg.get("helper-1")
	if !ok {
		t.Fatal("the planted worker vanished")
	}
	result, ferr, done := h.Result()
	if !done || ferr != nil || result != "already on disk" {
		t.Fatalf("close must not rewrite a finished result: result=%q err=%v done=%v", result, ferr, done)
	}
}
