package engine

import (
	"encoding/json"
	"testing"
	"time"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/provider"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

func TestUsageKindIsStable(t *testing.T) {
	if KindUsage != "usage" {
		t.Fatalf("KindUsage=%q", KindUsage)
	}
	if !isAuxiliaryAgent(TitleAgentID) || isAuxiliaryAgent(swarm.DefaultManagerID) {
		t.Fatal("the namer is not the conversation meter")
	}
}

func TestATurnRecordsAndBroadcastsTokenUsage(t *testing.T) {
	e := newTestEngine(t)
	th, err := e.CreateThread("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	sub := e.Subscribe(th.ID)
	t.Cleanup(sub.Close)
	sawUsage := make(chan store.Event, 8)
	go func() {
		for ev := range sub.C {
			if ev.Kind == KindUsage {
				select {
				case sawUsage <- ev:
				default:
				}
			}
		}
	}()

	turn, err := e.StartTurn(th.ID, "summarize the material")
	if err != nil {
		t.Fatal(err)
	}
	waitForTurn(t, e, turn.ID)

	calls, err := e.Store().ListLLMCalls(turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(calls) == 0 {
		t.Fatal("a finished turn must have recorded model calls")
	}
	sawTokens := false
	for _, c := range calls {
		if c.PromptTokens > 0 && c.TotalTokens > 0 {
			sawTokens = true
			break
		}
	}
	if !sawTokens {
		t.Fatalf("scripted calls must carry usage: %+v", calls)
	}

	snap, err := e.Usage(th.ID, turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	if snap.ContextTokens <= 0 {
		t.Fatalf("the meter needs the last manager prompt, got %+v", snap)
	}
	if snap.ContextWindow != provider.MockContextWindow {
		t.Fatalf("scripted window=%d want %d", snap.ContextWindow, provider.MockContextWindow)
	}
	if snap.Turn.Calls == 0 || snap.Thread.TotalTokens == 0 {
		t.Fatalf("billed totals empty: %+v", snap)
	}

	select {
	case ev := <-sawUsage:
		if ev.Seq != 0 {
			t.Fatalf("usage must not be stored, seq=%d", ev.Seq)
		}
		if ev.AgentID != swarm.DefaultManagerID {
			t.Fatalf("agent=%q", ev.AgentID)
		}
		var got store.UsageSnapshot
		if err := json.Unmarshal([]byte(ev.Text), &got); err != nil {
			t.Fatalf("usage payload: %v", err)
		}
		if got.ContextTokens <= 0 || got.ContextWindow != provider.MockContextWindow {
			t.Fatalf("live snapshot wrong: %+v", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("a running turn must broadcast usage so the composer meter can move")
	}

	replay, err := e.Replay(th.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, ev := range replay {
		if ev.Kind == KindUsage {
			t.Fatal("usage pulses must not be persisted; replay would double-count them")
		}
	}
}
