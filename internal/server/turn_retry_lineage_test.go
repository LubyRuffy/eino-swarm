package server_test

import (
	"net/http"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/store"
)

func TestTurnListReadsLegacyRetryLineageWithoutWritingItOnSend(t *testing.T) {
	h := newHarness(t)
	id := h.newThread()
	st := h.app.Store
	path := "/api/threads/" + id + "/turns"
	original := &store.Turn{ThreadID: id, Status: store.TurnError, UserText: "Original request"}
	legacy := &store.Turn{ThreadID: id, Status: store.TurnError, UserText: "Legacy retry"}
	for _, turn := range []*store.Turn{original, legacy} {
		if err := st.CreateTurn(turn); err != nil {
			t.Fatal(err)
		}
	}
	// Simulate the older client, which could write this lineage column.
	if !st.DB().Migrator().HasColumn("turns", "retry_turn_id") {
		if err := st.DB().Exec("ALTER TABLE turns ADD COLUMN retry_turn_id TEXT").Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := st.DB().Exec("UPDATE turns SET retry_turn_id = ? WHERE id = ?", original.ID, legacy.ID).Error; err != nil {
		t.Fatal(err)
	}

	accepted := h.json(http.MethodPost, path, map[string]any{
		"text":          "A new request",
		"retry_turn_id": legacy.ID,
	}, http.StatusAccepted)["turn"].(map[string]any)
	if _, present := accepted["retry_turn_id"]; present {
		t.Fatalf("new send accepted legacy lineage: %v", accepted)
	}
	turns := h.json(http.MethodGet, path, nil, http.StatusOK)["turns"].([]any)
	if len(turns) != 3 {
		t.Fatalf("got %d turns, want three", len(turns))
	}
	for i, row := range turns {
		turn := row.(map[string]any)
		lineage, present := turn["retry_turn_id"]
		if i == 1 {
			if turn["id"] != legacy.ID || lineage != original.ID {
				t.Fatalf("legacy lineage missing from turn list: %v", turn)
			}
		} else if present {
			t.Fatalf("turn received unexpected lineage: %v", turn)
		}
	}
}
