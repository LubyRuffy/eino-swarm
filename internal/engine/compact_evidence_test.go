package engine

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/store"
)

func TestAutoCompactShowsProseWhileKeepingWorkerEvidenceForTheModel(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	turn := &store.Turn{ThreadID: th.ID, UserText: "continue"}
	if err := e.Store().CreateTurn(turn); err != nil {
		t.Fatal(err)
	}
	const prose = "The current investigation is unfinished."
	summary := workerEvidenceSummary(prose, nil, []string{turn.ID})
	m := &autoCompact{engine: e, threadID: th.ID, turnID: turn.ID}
	m.persist(summary, 2, 100, 50)
	stored, _ := e.Store().GetThread(th.ID)
	if stored.CompactSummary != summary || !strings.Contains(compactSection(stored.CompactSummary), `"new_workers":0`) {
		t.Fatal("presentation removed evidence from the model's persisted briefing")
	}
	events, _ := e.Store().ListTurnEvents(turn.ID)
	if len(events) != 1 || events[0].Kind != KindCompacted {
		t.Fatalf("missing compact notice: %+v", events)
	}
	var payload compactPayload
	if err := json.Unmarshal([]byte(events[0].Text), &payload); err != nil || payload.Summary != prose || !payload.Auto || payload.TokensBefore != 100 {
		t.Fatalf("the user briefing leaked internal evidence or lost its metadata: %+v, %v", payload, err)
	}
	// Legacy diagnostic text and failure events must still pass through.
	e.recordCompact(th.ID, turn.ID, "legacy diagnostic", "fixture failure")
	events, _ = e.Store().ListTurnEvents(turn.ID)
	if events[1].Text != "legacy diagnostic" || events[1].Err != "fixture failure" {
		t.Fatal("presentation rewrote an unrelated diagnostic")
	}
}
