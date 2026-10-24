package engine

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
	"time"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/cloudwego/eino/components/model"
)

// Checkpoints have not been rewritten while workers summarize. That is a
// separate count from workers whose summary has already settled.
func TestRestartSummaryReportsSettledWorkersAndPendingIdentities(t *testing.T) {
	e := newTestEngine(t)
	var output bytes.Buffer
	e.log = slog.New(slog.NewTextHandler(&output, nil))
	th, err := e.CreateThread("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	turn := plantUnfinishedTurn(t, e, th.ID, "continue")
	reg := swarm.NewRegistry()
	t.Cleanup(reg.Close)
	entered := make(chan struct{})
	reg.ModelBuilder = func(_, _ string) model.BaseChatModel { return &holdModel{entered: entered} }
	h, err := reg.Spawn(t.Context(), "observer", "inspect current state", reg.ModelBuilder)
	if err != nil {
		t.Fatal(err)
	}
	<-entered
	if err := reg.PlantFinished(swarm.FinishedWorker{ID: "archived-2", Role: "reader", Result: "confirmed"}); err != nil {
		t.Fatal(err)
	}
	rt := e.runtimeFor(th.ID)
	rt.mu.Lock()
	rt.turnID, rt.parked = turn.ID, reg
	rt.mu.Unlock()
	ctx, cancel := context.WithTimeout(t.Context(), 800*time.Millisecond)
	defer cancel()
	done := make(chan RestartStatus, 1)
	go func() { status, _ := e.PrepareRestart(ctx); done <- status }()
	waitRestartState(t, func() bool { return e.GetRestartStatus().Stage == "summarizing" })
	raw, err := json.Marshal(e.GetRestartStatus())
	if err != nil {
		t.Fatal(err)
	}
	var status struct {
		Workers      int `json:"workers"`
		Checkpointed int `json:"checkpointed"`
		Settled      int `json:"settled"`
	}
	if err := json.Unmarshal(raw, &status); err != nil {
		t.Fatal(err)
	}
	if status.Workers != 2 || status.Settled != 1 || status.Checkpointed != 0 {
		t.Errorf("summary progress hid an already-settled worker: %s", raw)
	}
	final := <-done
	if !final.Ready {
		t.Fatalf("handoff did not finish: %+v", final)
	}
	if logs := output.String(); !strings.Contains(logs, "level=WARN") || !strings.Contains(logs, h.ID) || !strings.Contains(logs, "observer") || strings.Contains(logs, "archived-2") {
		t.Fatalf("half-budget warning did not identify only pending workers: %s", logs)
	}
}
