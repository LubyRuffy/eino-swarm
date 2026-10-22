package engine

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

func TestGenerationInterruptionPersistsOriginalOutputAcrossManagerTurns(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	first := &store.Turn{ThreadID: th.ID}
	_ = e.store.CreateTurn(first)
	next := &store.Turn{ThreadID: th.ID}
	_ = e.store.CreateTurn(next)
	a := newAccumulator(e, th.ID, first.ID, 0)
	a.onNotify(swarm.Notification{Kind: swarm.NotifyGenerationStarted, AgentID: "worker-1", MessageID: "old"})
	a.onNotify(swarm.Notification{Kind: swarm.NotifyReasoningDelta, AgentID: "worker-1", MessageID: "old", Text: "a partial thought"})
	a.flushAll()
	b := newAccumulator(e, th.ID, next.ID, 0)
	b.onNotify(swarm.Notification{Kind: swarm.NotifyGenerationInterrupted, AgentID: "worker-1", MessageID: "old", Text: `{"reason":"engine_restart","detail":"handoff","recovery":"pending"}`})
	b.onNotify(swarm.Notification{Kind: swarm.NotifyGenerationStarted, AgentID: "worker-1", MessageID: "new"})
	events, _ := e.store.ListTurnEvents(first.ID)
	found := map[string]bool{}
	for _, ev := range events {
		if ev.Kind == "generation_interrupted" || ev.Kind == "generation_recovered" {
			found[ev.Kind] = true
			if ev.MessageID != "old" {
				t.Fatalf("identity lost: %+v", ev)
			}
		}
		if ev.Kind == "finished" {
			t.Fatalf("generation interrupted worker lifetime: %+v", ev)
		}
	}
	if !found["generation_interrupted"] || !found["generation_recovered"] {
		t.Fatalf("original Trace lacks interruption/recovery: %+v", events)
	}
}

func TestColdGenerationRecoveryClosesOnlyUnfinishedRequests(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	turn := &store.Turn{ThreadID: th.ID}
	_ = e.store.CreateTurn(turn)
	for _, ev := range []store.Event{
		{Kind: "generation_started", AgentID: "worker-1", MessageID: "unfinished"},
		{Kind: "generation_started", AgentID: "worker-2", MessageID: "complete"},
		{Kind: "generation_completed", AgentID: "worker-2", MessageID: "complete"},
	} {
		ev.ThreadID, ev.TurnID = th.ID, turn.ID
		e.record(ev)
	}
	e.interruptOrphanedGenerations(th.ID, false)
	e.interruptOrphanedGenerations(th.ID, false)
	events, _ := e.store.ListTurnEvents(turn.ID)
	n := 0
	for _, ev := range events {
		if ev.Kind != "generation_interrupted" {
			continue
		}
		n++
		var body swarm.GenerationInterruption
		if err := json.Unmarshal([]byte(ev.Text), &body); err != nil {
			t.Fatal(err)
		}
		if ev.MessageID != "unfinished" || body.Reason != "process_exit" || body.Recovery != "pending" {
			t.Fatalf("cold recovery fabricated outcome: %+v %+v", ev, body)
		}
	}
	if n != 1 {
		t.Fatalf("want exactly one orphan closure, got %d events=%+v", n, events)
	}
}

func TestGenerationCauseUsesActualEngineStateAndSafeModelDetail(t *testing.T) {
	for _, tc := range []struct {
		name, payload, reason       string
		stopped, abandoned, planned bool
		err                         error
	}{
		{name: "unknown", payload: "not-json", reason: "unknown"},
		{name: "human stop", payload: `{"reason":"cancelled","detail":"cancelled","recovery":"none"}`, reason: "cancelled", stopped: true, abandoned: true},
		{name: "restart", payload: `{"reason":"cancelled","detail":"cancelled","recovery":"none"}`, reason: "engine_restart", abandoned: true, planned: true},
		{name: "safe service error", payload: `{"reason":"model_error","detail":"incomplete","recovery":"none"}`, reason: "model_error", err: errors.New("api_key=private-secret " + strings.Repeat("long-failure ", 100))},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newTestEngine(t)
			th, _ := e.CreateThread("", "", "")
			turn := &store.Turn{ThreadID: th.ID}
			_ = e.store.CreateTurn(turn)
			rt := e.runtimeFor(th.ID)
			rt.userStopped, rt.abandoned = tc.stopped, tc.abandoned
			if tc.planned {
				e.restart.status.Stage = "preparing"
			}
			a := newAccumulator(e, th.ID, turn.ID, 0)
			a.onNotify(swarm.Notification{Kind: swarm.NotifyGenerationStarted, AgentID: "worker", MessageID: "output"})
			a.onNotify(swarm.Notification{Kind: swarm.NotifyGenerationInterrupted, AgentID: "worker", MessageID: "output", Text: tc.payload, Err: tc.err})
			events, _ := e.store.ListTurnEvents(turn.ID)
			var body swarm.GenerationInterruption
			last := events[len(events)-1]
			_ = json.Unmarshal([]byte(last.Text), &body)
			if body.Reason != tc.reason || body.Detail == "" || len([]rune(body.Detail)) > 512 || strings.Contains(last.Text, "private-secret") || last.Err != "" {
				t.Fatalf("wrong or unsafe cause: %+v %+v", last, body)
			}
		})
	}
}

func TestGenerationReceiptsAreNotPublishedOrCachedWhenStorageRejectsThem(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	sub := e.Subscribe(th.ID)
	defer sub.Close()
	e.droppedTurns["dropped"] = struct{}{}
	if e.recordGeneration(&store.Event{ThreadID: th.ID, TurnID: "dropped", Kind: "generation_started"}) {
		t.Fatal("rewound turn persisted a request")
	}
	_ = e.store.Close()
	if e.recordGeneration(&store.Event{ThreadID: th.ID, TurnID: "missing", Kind: "generation_started"}) {
		t.Fatal("storage failure became success")
	}
	e.generations.Lock()
	_ = e.generationLog(th.ID)
	_, cached := e.generations.threads[th.ID]
	e.generations.Unlock()
	if cached {
		t.Fatal("failed storage read was cached as complete history")
	}
	select {
	case ev := <-sub.C:
		t.Fatalf("unpersisted receipt published: %+v", ev)
	default:
	}
}

func TestGenerationColdHandoffAndDeletionReleaseCachedLifetimes(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	a := newAccumulator(e, th.ID, "original", 0)
	a.onNotify(swarm.Notification{Kind: swarm.NotifyGenerationStarted, AgentID: "worker", MessageID: "output"})
	e.record(store.Event{ThreadID: th.ID, TurnID: "original", Kind: KindRestartCheckpoint, Text: `{"version":1}`})
	e.interruptOrphanedGenerations(th.ID, true)
	events, _ := e.store.ListGenerationEvents(th.ID)
	if len(events) != 2 || !strings.Contains(events[1].Text, `"reason":"engine_restart"`) {
		t.Fatalf("handoff lost its known cause: %+v", events)
	}
	if err := e.DeleteThread(th.ID); err != nil {
		t.Fatal(err)
	}
	e.generations.Lock()
	_, cached := e.generations.threads[th.ID]
	e.generations.Unlock()
	if cached {
		t.Fatal("deleted conversation retained generation cache")
	}
}

func TestColdHumanStopClosesGenerationWithoutClaimingRecoveryWasScheduled(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	turn := plantUnfinishedTurn(t, e, th.ID, "Inspect inputs.")
	e.record(store.Event{ThreadID: th.ID, TurnID: turn.ID, Kind: "generation_started", AgentID: "worker", MessageID: "unfinished"})
	e.record(store.Event{ThreadID: th.ID, TurnID: turn.ID, Kind: KindUserStopRequested, AgentID: "manager"})
	if n, err := e.ResumeOrphanedTurns(); n != 0 || err != nil {
		t.Fatalf("human stop resumed: %d %v", n, err)
	}
	events, _ := e.store.ListGenerationEvents(th.ID)
	if len(events) != 2 || !strings.Contains(events[1].Text, `"reason":"cancelled"`) || !strings.Contains(events[1].Text, `"recovery":"none"`) {
		t.Fatalf("human stop left a pending generation: %+v", events)
	}
}

func TestColdParkedGenerationHasNoPendingRecoveryWithoutItsOwner(t *testing.T) {
	e := newTestEngine(t)
	th, turn, wake := plantParkedRestart(t, e)
	e.record(store.Event{ThreadID: th.ID, TurnID: turn.ID, Kind: "generation_started", AgentID: "worker-7", MessageID: "unfinished"})
	if _, err := e.store.UpdateActiveSchedule(wake.ID, map[string]any{"status": store.SchedulePaused}); err != nil {
		t.Fatal(err)
	}
	if n, err := e.ResumeParkedWorkers(); n != 0 || err != nil {
		t.Fatalf("unowned worker resumed: %d %v", n, err)
	}
	events, _ := e.store.ListGenerationEvents(th.ID)
	if len(events) != 2 || !strings.Contains(events[1].Text, `"recovery":"none"`) {
		t.Fatalf("unowned generation pretended recovery was pending: %+v", events)
	}
}

func TestColdRecoveryExplainsEveryPrefetchedGenerationOfTheSameWorker(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	turn := plantUnfinishedTurn(t, e, th.ID, "Check the current evidence.")
	a := newAccumulator(e, th.ID, turn.ID, 0)
	for _, id := range []string{"first-prefetch", "second-prefetch"} {
		a.onNotify(swarm.Notification{Kind: swarm.NotifyGenerationStarted, AgentID: "worker", MessageID: id})
	}
	e.interruptOrphanedGenerations(th.ID, false)
	a.onNotify(swarm.Notification{Kind: swarm.NotifyGenerationStarted, AgentID: "worker", MessageID: "replacement"})
	events, _ := e.store.ListGenerationEvents(th.ID)
	recovered := map[string]bool{}
	for _, ev := range events {
		if ev.Kind == "generation_recovered" {
			recovered[ev.MessageID] = true
		}
	}
	if !recovered["first-prefetch"] || !recovered["second-prefetch"] {
		t.Fatalf("one interrupted generation stayed pending after execution resumed: %+v", events)
	}
}

func TestGenerationRecoveryFailureClearsOnlyThePendingState(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	a := newAccumulator(e, th.ID, "original", 0)
	a.onNotify(swarm.Notification{Kind: swarm.NotifyGenerationStarted, AgentID: "worker", MessageID: "output"})
	a.onNotify(swarm.Notification{Kind: swarm.NotifyGenerationInterrupted, AgentID: "worker", MessageID: "output", Text: `{"reason":"engine_restart","detail":"handoff","recovery":"pending"}`})
	e.stopOrphanedGenerations(th.ID, false)
	e.stopOrphanedGenerations(th.ID, true)
	events, _ := e.store.ListGenerationEvents(th.ID)
	if len(events) != 3 || !strings.Contains(events[2].Text, `"reason":"engine_restart"`) || !strings.Contains(events[2].Text, `"recovery":"none"`) {
		t.Fatalf("failed recovery changed the output cause or remained pending: %+v", events)
	}
}

func TestNewHumanTurnDoesNotClaimToRecoverAnEarlierManagerFailure(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	a := newAccumulator(e, th.ID, "old", 0)
	a.onNotify(swarm.Notification{Kind: swarm.NotifyGenerationStarted, AgentID: "manager", MessageID: "failure"})
	a.onNotify(swarm.Notification{Kind: swarm.NotifyGenerationInterrupted, AgentID: "manager", MessageID: "failure", Text: `{"reason":"model_error","detail":"service refused","recovery":"none"}`})
	newAccumulator(e, th.ID, "new", 0).onNotify(swarm.Notification{Kind: swarm.NotifyGenerationStarted, AgentID: "manager", MessageID: "new-request"})
	events, _ := e.store.ListGenerationEvents(th.ID)
	for _, ev := range events {
		if ev.Kind == "generation_recovered" {
			t.Fatalf("unrelated human request fabricated recovery: %+v", ev)
		}
	}
	e.generations.Lock()
	left := len(e.generations.threads[th.ID].pending)
	e.generations.Unlock()
	if left != 0 {
		t.Fatalf("an unrelated old manager failure remained in the live recovery cache: %d", left)
	}
}

func TestOldRestartCheckpointCannotExplainANewerAbruptlyInterruptedGeneration(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	turn := plantUnfinishedTurn(t, e, th.ID, "Inspect the current evidence.")
	e.record(store.Event{ThreadID: th.ID, TurnID: turn.ID, Kind: KindRestartCheckpoint, Text: `{"version":1}`})
	a := newAccumulator(e, th.ID, turn.ID, 0)
	a.onNotify(swarm.Notification{Kind: swarm.NotifyGenerationStarted, AgentID: "worker", MessageID: "newer-than-restart"})
	e.interruptOrphanedGenerations(th.ID, true)
	events, _ := e.store.ListGenerationEvents(th.ID)
	if len(events) != 2 || !strings.Contains(events[1].Text, `"reason":"process_exit"`) {
		t.Fatalf("old checkpoint fabricated a new restart cause: %+v", events)
	}
}

func TestPrefetchedGenerationRecoveryIsRecordedWhenItsStreamActuallyDrains(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	turn := plantUnfinishedTurn(t, e, th.ID, "Check the current evidence.")
	a := newAccumulator(e, th.ID, turn.ID, 0)
	for _, id := range []string{"first", "prefetched"} {
		a.onNotify(swarm.Notification{Kind: swarm.NotifyGenerationStarted, AgentID: "worker", MessageID: id})
	}
	a.onNotify(swarm.Notification{Kind: swarm.NotifyGenerationInterrupted, AgentID: "worker", MessageID: "first", Text: `{"reason":"output_limit","detail":"partial generation","recovery":"none"}`})
	a.onNotify(swarm.Notification{Kind: swarm.NotifyTurn, AgentID: "worker", MessageID: "prefetched"})
	events, _ := e.store.ListGenerationEvents(th.ID)
	recovered := 0
	for _, ev := range events {
		if ev.Kind == "generation_recovered" && ev.MessageID == "first" {
			recovered++
		}
	}
	if recovered != 1 {
		t.Fatalf("prefetched execution resumed without updating its previous interruption: %+v", events)
	}
}

func TestDeletingAProjectRemovesItsConversationsGenerationCache(t *testing.T) {
	e := newTestEngine(t)
	project, err := e.CreateProject("generation project", "", "", false)
	if err != nil {
		t.Fatal(err)
	}
	th, err := e.CreateThread("", "", project.ID)
	if err != nil {
		t.Fatal(err)
	}
	newAccumulator(e, th.ID, "original", 0).onNotify(swarm.Notification{Kind: swarm.NotifyGenerationStarted, AgentID: "worker", MessageID: "output"})
	if err := e.DeleteProject(project.ID); err != nil {
		t.Fatal(err)
	}
	e.generations.Lock()
	_, cached := e.generations.threads[th.ID]
	e.generations.Unlock()
	if cached {
		t.Fatal("deleted project retained a conversation generation cache")
	}
	if _, err := e.store.GetThread(th.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("project conversation survived deletion: %v", err)
	}
}

func TestRewindCannotReuseDeletedGenerationOwnership(t *testing.T) {
	e := newTestEngine(t)
	th, _ := e.CreateThread("", "", "")
	turn := plantUnfinishedTurn(t, e, th.ID, "Inspect inputs.")
	user := store.Event{ThreadID: th.ID, TurnID: turn.ID, Kind: KindUser, AgentID: "manager", Text: "Inspect inputs."}
	if err := e.store.AppendEvent(&user); err != nil {
		t.Fatal(err)
	}
	a := newAccumulator(e, th.ID, turn.ID, 0)
	a.onNotify(swarm.Notification{Kind: swarm.NotifyGenerationStarted, AgentID: "worker", MessageID: "output"})
	if err := e.applyRewind(th.ID, user.Seq); err != nil {
		t.Fatal(err)
	}
	e.generations.Lock()
	_, cached := e.generations.threads[th.ID]
	e.generations.Unlock()
	if cached {
		t.Fatal("rewind retained ownership of a deleted generation")
	}
	a.onNotify(swarm.Notification{Kind: swarm.NotifyGenerationInterrupted, AgentID: "worker", MessageID: "output", Text: `{"reason":"cancelled","detail":"stopped","recovery":"none"}`})
	events, _ := e.store.ListGenerationEvents(th.ID)
	if len(events) != 0 {
		t.Fatalf("a late old generation returned after rewind: %+v", events)
	}
}

func TestLateExplicitModelFailureCannotBecomeHumanCancellation(t *testing.T) {
	for _, reason := range []string{"model_error", "output_limit", "timeout", "engine_restart"} {
		t.Run(reason, func(t *testing.T) {
			e := newTestEngine(t)
			th, _ := e.CreateThread("", "", "")
			turn := plantUnfinishedTurn(t, e, th.ID, "Inspect inputs.")
			rt := e.runtimeFor(th.ID)
			rt.mu.Lock()
			rt.userStopped = true
			rt.abandoned = true
			rt.mu.Unlock()
			a := newAccumulator(e, th.ID, turn.ID, 0)
			a.onNotify(swarm.Notification{Kind: swarm.NotifyGenerationStarted, AgentID: "worker", MessageID: "output"})
			a.onNotify(swarm.Notification{Kind: swarm.NotifyGenerationInterrupted, AgentID: "worker", MessageID: "output", Text: generationBody(swarm.GenerationInterruption{Reason: reason, Detail: "actual earlier cause", Recovery: "pending"})})
			events, _ := e.store.ListGenerationEvents(th.ID)
			var body swarm.GenerationInterruption
			_ = json.Unmarshal([]byte(events[len(events)-1].Text), &body)
			if body.Reason != reason || body.Detail != "actual earlier cause" || body.Recovery != "none" {
				t.Fatalf("late Stop replaced the actual generation cause: %+v", body)
			}
		})
	}
}
