package engine

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/store"
)

func TestNuwaVerifyAdmissionRefusesBeforeRecordingABadCheck(t *testing.T) {
	e := newTestEngine(t)
	project, err := e.CreateProject("Verification allowance", "", "", false, true)
	if err != nil {
		t.Fatal(err)
	}
	thread, err := e.CreateThread("One recorded check", "", project.ID)
	if err != nil {
		t.Fatal(err)
	}
	turn := &store.Turn{ThreadID: thread.ID, ProjectID: project.ID}
	if err := e.store.CreateTurn(turn); err != nil {
		t.Fatal(err)
	}
	node := store.NuwaNode{ProjectID: project.ID, Kind: "fact", Title: "Recorded deliverable", Summary: "Assigned result", ThreadID: thread.ID, TurnID: turn.ID, AgentID: "manager", Generation: 0}
	if err := e.store.UpsertNuwaNode(t.Context(), &node, 0); err != nil {
		t.Fatal(err)
	}
	admit := e.nuwaVerifyAdmission(turn)
	if err := admit(t.Context(), "verify", node.ID, "verify-call"); err != nil {
		t.Fatal(err)
	}
	if err := admit(t.Context(), "verify", node.ID, "verify-call"); err != nil {
		t.Fatal("replaying the same verification call consumed another slot")
	}
	if err := admit(t.Context(), "verify", node.ID, "verify-again"); err == nil || !strings.Contains(err.Error(), "one verify") {
		t.Fatalf("second verify err=%v", err)
	}
	if err := admit(t.Context(), "verify", "nw_other", "verify-call"); err == nil || !strings.Contains(err.Error(), "different verification target") {
		t.Fatalf("conflicting replay err=%v", err)
	}
	if err := admit(t.Context(), "fix", "nw_missing", "missing-target"); err == nil || !strings.Contains(err.Error(), "existing shared record") {
		t.Fatalf("unknown record err=%v", err)
	}
	if err := e.store.DB().Exec("CREATE TRIGGER refuse_nuwa_verify BEFORE INSERT ON nuwa_verify_slots BEGIN SELECT RAISE(ABORT, 'verification row refused'); END").Error; err != nil {
		t.Fatal(err)
	}
	err = admit(t.Context(), "fix", node.ID, "fix-call")
	if err == nil || !strings.Contains(err.Error(), "could not be recorded") || strings.Contains(err.Error(), "verification row refused") {
		t.Fatalf("storage failure exposed the database error: %v", err)
	}
	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	if err := admit(cancelled, "fix", node.ID, "cancelled-call"); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled admission err=%v", err)
	}
	if err := e.nuwaVerifyAdmission(nil)(t.Context(), "verify", node.ID, "no-turn"); err == nil || !strings.Contains(err.Error(), "no project") {
		t.Fatalf("missing turn err=%v", err)
	}
	if err := e.nuwaVerifyAdmission(&store.Turn{})(t.Context(), "verify", node.ID, "no-project"); err == nil || !strings.Contains(err.Error(), "no project") {
		t.Fatalf("missing project err=%v", err)
	}
}
