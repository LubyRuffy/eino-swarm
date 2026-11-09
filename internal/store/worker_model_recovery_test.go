package store

import (
	swarm "github.com/LubyRuffy/eino-swarm"
	"testing"
)

func TestWorkerRecoveryCommitIsAtomicAndKeepsOriginalHistory(t *testing.T) {
	s := openTestStore(t)
	th := &Thread{Title: "Recovery", ProjectID: ""}
	if err := s.CreateThread(th); err != nil {
		t.Fatal(err)
	}
	one, two := &Turn{ThreadID: th.ID, ProviderID: "removed"}, &Turn{ThreadID: th.ID, ProviderID: "removed"}
	for _, turn := range []*Turn{one, two} {
		if err := s.CreateTurn(turn); err != nil {
			t.Fatal(err)
		}
	}
	replacement := map[string]swarm.ModelBinding{"removed": {ProviderID: "available", Model: "chosen"}}
	changes := map[string]map[string]swarm.ModelBinding{one.ID: replacement, two.ID: replacement}
	if err := s.SaveWorkerModelRecovery(t.Context(), th.ID, "wrong-owner", changes); err == nil {
		t.Fatal("changed project accepted")
	}
	if err := s.SaveWorkerModelRecovery(t.Context(), "deleted", "", changes); err == nil {
		t.Fatal("deleted thread accepted")
	}
	// Whichever map entry executes first, a failing UPDATE must roll it back.
	if err := s.DB().Exec("CREATE TRIGGER refuse_recovery BEFORE UPDATE OF worker_model_recovery ON turns WHEN NEW.id = '" + two.ID + "' BEGIN SELECT RAISE(ABORT,'fixture'); END").Error; err != nil {
		t.Fatal(err)
	}
	if err := s.SaveWorkerModelRecovery(t.Context(), th.ID, "", changes); err == nil {
		t.Fatal("storage refusal ignored")
	}
	got, err := s.GetTurn(one.ID)
	if err != nil || len(got.WorkerModelRecovery) != 0 {
		t.Fatalf("partial recovery: %+v %v", got, err)
	}
	if err := s.DB().Exec("DROP TRIGGER refuse_recovery").Error; err != nil {
		t.Fatal(err)
	}
	changes["missing"] = replacement
	if err := s.SaveWorkerModelRecovery(t.Context(), th.ID, "", changes); err == nil {
		t.Fatal("missing source accepted")
	}
	delete(changes, "missing")
	if err := s.SaveWorkerModelRecovery(t.Context(), th.ID, "", changes); err != nil {
		t.Fatal(err)
	}
	got, err = s.GetTurn(one.ID)
	if err != nil || got.ProviderID != "removed" || got.WorkerModelRecovery["removed"].Model != "chosen" {
		t.Fatalf("selection=%+v %v", got, err)
	}
	if err := s.DeleteThread(th.ID); err != nil {
		t.Fatal(err)
	}
	if err := s.SaveWorkerModelRecovery(t.Context(), th.ID, "", changes); err == nil {
		t.Fatal("late recovery recreated deleted history")
	}
}
