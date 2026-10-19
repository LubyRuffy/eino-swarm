package app

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/engine"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/cloudwego/eino/schema"
)

func TestNewRestoresParkedWorkersWithoutStartingTheirNextManagerTurn(t *testing.T) {
	t.Setenv("OPENAI_BASE_URL", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("OPENAI_MODEL", "")
	dir := t.TempDir()
	first, err := New(Options{DataDir: dir, Addr: "127.0.0.1:0", Mock: true, NoAssets: true})
	if err != nil {
		t.Fatal(err)
	}
	th, err := first.Engine.CreateThread("parked", "", "")
	if err != nil {
		t.Fatal(err)
	}
	turn := &store.Turn{ThreadID: th.ID, ProviderID: th.ProviderID, Model: th.Model, UserText: "continue work"}
	if err := first.Store.CreateTurn(turn); err != nil {
		t.Fatal(err)
	}
	if err := first.Store.FinishTurn(turn.ID, store.TurnDone, "waiting", ""); err != nil {
		t.Fatal(err)
	}
	wake, err := first.Engine.CreateSchedule(engine.ScheduleInput{Kind: store.ScheduleThread, ThreadID: th.ID,
		Prompt: "check progress", EveryS: 3600, CreatedBy: store.ScheduleCreatedManager})
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]any{"version": 1, "workers": []swarm.WorkerCheckpoint{{ID: "worker-7", Role: "verifier",
		Instruction: "Inspect assigned work.", Task: "continue", History: []*schema.Message{schema.UserMessage("continue")}}}})
	if err := first.Store.AppendEvent(&store.Event{ThreadID: th.ID, TurnID: turn.ID, Kind: engine.KindRestartCheckpoint,
		AgentID: swarm.DefaultManagerID, Text: string(body)}); err != nil {
		t.Fatal(err)
	}
	first.Shutdown(context.Background())
	second, err := New(Options{DataDir: dir, Addr: "127.0.0.1:0", Mock: true, NoAssets: true})
	if err != nil {
		t.Fatal(err)
	}
	defer second.Shutdown(context.Background())
	deadline := time.Now().Add(10 * time.Second)
	finished := false
	for time.Now().Before(deadline) {
		events, _ := second.Store.ListTurnEvents(turn.ID)
		for _, ev := range events {
			finished = finished || ev.Kind == swarm.NotifyFinished.String() && ev.AgentID == "worker-7" && ev.Err == ""
		}
		if finished {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !finished {
		t.Fatal("startup left a parked worker waiting for its future wake")
	}
	if turns, _ := second.Store.ListTurns(th.ID); len(turns) != 1 || turns[0].Status != store.TurnDone {
		t.Fatalf("parked recovery changed the manager lifecycle: %+v", turns)
	}
	if after, _ := second.Store.GetSchedule(wake.ID); !after.NextRunAt.Equal(wake.NextRunAt) {
		t.Fatal("startup fired or pulled forward the future wake")
	}
}
