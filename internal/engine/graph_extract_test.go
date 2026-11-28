package engine

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/graph"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

// The catch-up request is a standing instruction. A sample from one
// conversation, or a kind id, must not become part of it.
func TestGraphExtractTextIsGeneric(t *testing.T) {
	text := GraphExtractText() + "\n" + graphReplayInstruction
	if !strings.Contains(graphReplayInstruction, "update_graph") || !strings.Contains(GraphExtractText(), "one completed round") {
		t.Fatalf("the replay does not say it walks rounds: %s", text)
	}
	catalog := graph.MustDefault()
	for _, id := range catalog.IDs() {
		if strings.Contains(text, id) {
			t.Errorf("kind %q leaked into the replay request", id)
		}
	}
	for _, leak := range []string{"doris", "fofa", "203.0.113", "空仓", "credential", "repository"} {
		if strings.Contains(strings.ToLower(text), leak) {
			t.Errorf("%q leaked into the replay request", leak)
		}
	}
}

func TestReplayKeepsAConfirmedSubjectAsALead(t *testing.T) {
	batch := graph.Batch{
		Subjects: []graph.SubjectInput{{Kind: "ip", Status: graph.StatusConfirmed, Attrs: map[string]any{"ip": "203.0.113.10"}}},
		Todos:    []graph.TodoInput{{Title: "check", Status: graph.TodoDone, Result: "done"}},
		Evidence: []graph.Evidence{{URL: "https://example.test"}},
	}
	softenReplayBatch(&batch)
	if batch.Subjects[0].Status != graph.StatusLead || batch.Todos[0].Status != graph.TodoOpen || batch.Todos[0].Result != "" || len(batch.Evidence) != 0 {
		t.Fatalf("batch=%+v", batch)
	}
}

func TestGraphReplayWalksCompletedRoundsInOrder(t *testing.T) {
	turns := []store.Turn{
		{ID: "tn_1", Seq: 1, Status: store.TurnDone, UserText: "first request"},
		{ID: "tn_2", Seq: 2, Status: store.TurnDone, UserText: "second request"},
		{ID: "tn_replay", Seq: 3, Status: store.TurnDone, UserText: GraphExtractText(), GraphReplay: true},
	}
	messages := []store.Message{
		{TurnID: "tn_2", Role: "assistant", Content: "second response"},
		{TurnID: "tn_1", Role: "tool", Content: "first response"},
		{TurnID: "tn_replay", Role: "assistant", Content: "must not be a round"},
		{TurnID: "tn_empty", Role: "user", Content: "no response"},
	}
	rounds := graphReplayRounds("tn_replay", turns, messages)
	if len(rounds) != 2 || rounds[0].Seq != 1 || rounds[0].TurnID != "tn_1" || rounds[1].Seq != 2 || rounds[1].TurnID != "tn_2" {
		t.Fatalf("rounds=%+v", rounds)
	}
	if !strings.Contains(rounds[0].Body, "first request") || !strings.Contains(rounds[0].Body, "first response") {
		t.Fatalf("first round=%s", rounds[0].Body)
	}
	if !strings.Contains(rounds[1].Body, "second response") || strings.Contains(rounds[1].Body, "must not") {
		t.Fatalf("second round=%s", rounds[1].Body)
	}
}

func TestGraphReplayUsesTheManagerIterationCap(t *testing.T) {
	if graphReplayIterations(config.SwarmConfig{}) != config.DefaultManagerIterations {
		t.Fatalf("default cap = %d", graphReplayIterations(config.SwarmConfig{}))
	}
	if got := graphReplayIterations(config.SwarmConfig{ManagerMaxIterations: 40}); got != 40 {
		t.Fatalf("configured cap = %d", got)
	}
}

func TestExtractGraphRefusesWhenTheSwitchIsOff(t *testing.T) {
	e := newTestEngine(t)
	if err := e.cfg.Update(func(c *config.Config) error {
		c.Swarm.Graph = false
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	th, err := e.CreateThread("extract", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.ExtractGraph(th.ID, true, th.ID); !errors.Is(err, ErrGraphDisabled) {
		t.Fatalf("graph off: %v", err)
	}
}

// Clearing is part of the walk, after the turn has been admitted. A busy
// conversation therefore never loses its graph, and the next revision is
// higher so the panel replaces the picture it already has.
func TestReplayClearsTheWorkingGraphBeforeTheWalk(t *testing.T) {
	e := newTestEngine(t)
	th, err := e.CreateThread("extract", "", "")
	if err != nil {
		t.Fatal(err)
	}
	turn := &store.Turn{ThreadID: th.ID, GraphTools: true, GraphReplay: true, GraphClear: true}
	if err := e.store.CreateTurn(turn); err != nil {
		t.Fatal(err)
	}
	if _, _, err := e.store.ApplyGraph(context.Background(), store.GraphCaller{ThreadID: th.ID, TurnID: turn.ID, AgentID: "manager"},
		graph.Batch{Subjects: []graph.SubjectInput{{Kind: "ip", Attrs: map[string]any{"ip": "203.0.113.9"}}}}); err != nil {
		t.Fatal(err)
	}
	text, err := (&runtime{engine: e}).replayGraph(context.Background(), turn)
	if err != nil || text != "No completed round to replay." {
		t.Fatalf("%q %v", text, err)
	}
	view, err := e.store.ReadGraph(context.Background(), th.ID, store.GraphQuery{All: true, Limit: -1})
	if err != nil || len(view.Subjects) != 0 || len(view.Links) != 0 || view.Revision < 2 {
		t.Fatalf("view=%+v err=%v", view, err)
	}
}
