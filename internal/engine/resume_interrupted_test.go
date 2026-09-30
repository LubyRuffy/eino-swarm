package engine

import (
	"strings"
	"testing"
	"time"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

func plantProcessStoppedWorker(t *testing.T, e *Engine, threadID, turnID, id, role string) {
	t.Helper()
	plantSpawnedWorker(t, e, threadID, turnID, id, role)
	if err := e.Store().AppendEvent(&store.Event{
		ThreadID: threadID, TurnID: turnID,
		Kind: swarm.NotifyFinished.String(), AgentID: id, Role: role,
		Err: "[NodeRunError] failed to receive stream chunk: context canceled",
	}); err != nil {
		t.Fatal(err)
	}
}

func TestProcessStoppedWorkersKeepsOnlyTheLatestDispatch(t *testing.T) {
	events := []store.Event{
		{Kind: swarm.NotifySpawned.String(), TurnID: "old", AgentID: "worker-1", Role: "worker"},
		{Kind: swarm.NotifyFinished.String(), TurnID: "old", AgentID: "worker-1", Err: "context canceled"},
		{Kind: swarm.NotifySpawned.String(), TurnID: "new", AgentID: "worker-2", Role: "worker"},
		{Kind: swarm.NotifyFinished.String(), TurnID: "new", AgentID: "worker-2", Text: "already on disk"},
		{Kind: swarm.NotifySpawned.String(), TurnID: "new", AgentID: swarm.DefaultManagerID, Role: "manager"},
	}
	if got := processStoppedWorkers(events); len(got) != 0 {
		t.Fatalf("an older stopped worker must not block once a later dispatch finished cleanly: %+v", got)
	}

	stopped := []store.Event{
		{Kind: swarm.NotifySpawned.String(), TurnID: "old", AgentID: "worker-1", Role: "worker"},
		{Kind: swarm.NotifyFinished.String(), TurnID: "old", AgentID: "worker-1", Text: "done"},
		{Kind: swarm.NotifySpawned.String(), TurnID: "new", AgentID: "worker-2"},
		{Kind: swarm.NotifyFinished.String(), TurnID: "new", AgentID: "worker-2", Role: "worker", Err: "context deadline exceeded"},
		{Kind: swarm.NotifySpawned.String(), TurnID: "new", AgentID: "worker-3", Role: "worker"},
		{Kind: swarm.NotifyFinished.String(), TurnID: "new", AgentID: "worker-3", Err: "the previous process stopped"},
		{Kind: swarm.NotifySpawned.String(), TurnID: "new", AgentID: "worker-4", Role: "worker"},
		{Kind: KindCleanup, AgentID: swarm.DefaultManagerID},
	}
	got := processStoppedWorkers(stopped)
	if len(got) != 2 || got[0].ID != "worker-2" || got[1].ID != "worker-3" {
		t.Fatalf("latest dispatch=%+v", got)
	}
	if got[0].Role != "worker" {
		t.Fatalf("role must come from the finish when spawn omitted it: %+v", got[0])
	}
	resumed := append(stopped, store.Event{
		Kind: swarm.NotifySpawned.String(), TurnID: "later", AgentID: "worker-2", Role: "worker",
	})
	got = processStoppedWorkers(resumed)
	if len(got) != 1 || got[0].ID != "worker-3" {
		t.Fatalf("resuming one id must leave its stopped sibling: %+v", got)
	}
	replaced := append(stopped, store.Event{
		Kind: swarm.NotifySpawned.String(), TurnID: "later", AgentID: "worker-9", Role: "worker",
	})
	if got = processStoppedWorkers(replaced); len(got) != 0 {
		t.Fatalf("a new id is a new dispatch: %+v", got)
	}
	if processStoppedWorkers(nil) != nil {
		t.Fatal("no events must not invent a stopped worker")
	}
	if processStopErr("") || processStopErr(cleanedUpWorkerErr) || processStopErr("tool failed") {
		t.Fatal("a clean stop or an ordinary tool failure is not a process death")
	}
}

func TestInterruptedWorkerCueIsGenericAndNamesTheIds(t *testing.T) {
	if formatInterruptedWorkersCue(nil) != "" {
		t.Fatal("no stopped workers must not invent a cue")
	}
	if interruptedWakeBlock([]stoppedWorker{{ID: "  "}}) != "" {
		t.Fatal("a blank id must not become a wake error")
	}
	cue := formatInterruptedWorkersCue([]stoppedWorker{{ID: "worker-2", Role: "worker"}})
	block := interruptedWakeBlock([]stoppedWorker{{ID: "worker-2"}})
	for _, blob := range []string{cue, block, interruptedWorkersCueLead} {
		for _, leak := range []string{"notes.md", "chapter", "disk", "scribe", "w104"} {
			if strings.Contains(strings.ToLower(blob), leak) {
				t.Fatalf("%q leaked into %q", leak, blob)
			}
		}
	}
	if !strings.Contains(cue, "worker-2") || !strings.Contains(cue, "exact ids") {
		t.Fatalf("cue=%q", cue)
	}
	if !strings.Contains(block, "worker-2") || !strings.HasPrefix(block, "these sub-agents are not running") {
		t.Fatalf("block=%q", block)
	}
	if !isEngineOnlyUser(cue) || !isEngineOnlyUser(resumeCue) || isEngineOnlyUser("a real request") {
		t.Fatal("the stopped-worker cue must stay off the human transcript")
	}
	if hasUserPrefix(nil, "") || hasUserPrefix([]adk.Message{schema.UserMessage("other")}, interruptedWorkersCueLead) {
		t.Fatal("prefix match must require the cue lead")
	}
}

func TestAppendInterruptedWorkerCueOnce(t *testing.T) {
	e := newTestEngine(t)
	th, err := e.CreateThread("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	turn := plantUnfinishedTurn(t, e, th.ID, "continue the leftover request")
	plantProcessStoppedWorker(t, e, th.ID, turn.ID, "worker-2", "worker")

	once := e.appendInterruptedWorkerCue(th.ID, nil)
	if n := countUserPrefix(once, interruptedWorkersCueLead); n != 1 || !strings.Contains(once[0].Content, "worker-2") {
		t.Fatalf("cue=%v", once)
	}
	twice := e.appendInterruptedWorkerCue(th.ID, once)
	if n := countUserPrefix(twice, interruptedWorkersCueLead); n != 1 {
		t.Fatalf("cue duplicated: %d", n)
	}

	msgs, err := e.resumeConversation(turn)
	if err != nil {
		t.Fatal(err)
	}
	if n := countUserPrefix(msgs, interruptedWorkersCueLead); n != 1 {
		t.Fatalf("resume must name the stopped worker once, got %d", n)
	}

	dead, err := e.processStoppedOn("")
	if err != nil || len(dead) != 0 {
		t.Fatalf("empty thread=%v err=%v", dead, err)
	}
	var none *Engine
	if n, err := none.PullInterruptedWorkerWakes(); n != 0 || err != nil {
		t.Fatalf("nil engine pull=%d err=%v", n, err)
	}
	if msg, err := none.interruptedWakeRefusal("th"); msg != "" || err != nil {
		t.Fatalf("nil refusal=%q err=%v", msg, err)
	}
	if got := none.appendInterruptedWorkerCue("th", nil); got != nil {
		t.Fatalf("nil append=%v", got)
	}
}

func countUserPrefix(msgs []adk.Message, prefix string) int {
	n := 0
	for _, m := range msgs {
		if m != nil && m.Role == schema.User && strings.HasPrefix(m.Content, prefix) {
			n++
		}
	}
	return n
}

func TestStartupPullsAWakeWhenTheLatestDispatchWasStopped(t *testing.T) {
	e := newTestEngine(t)
	th, err := e.CreateThread("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	other, err := e.CreateThread("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	turn := plantUnfinishedTurn(t, e, th.ID, "continue the leftover request")
	plantProcessStoppedWorker(t, e, th.ID, turn.ID, "worker-2", "worker")
	deadWake := mustCreateWake(t, e, th.ID)
	liveWake := mustCreateWake(t, e, other.ID)
	due := mustCreateWake(t, e, th.ID)
	// Two active wakes on one thread: the store allows it in tests via
	// direct create. The second row is already due and must keep its time.
	past := e.clock().Add(-time.Minute)
	if err := e.Store().UpdateSchedule(due.ID, map[string]any{"next_run_at": past}); err != nil {
		t.Fatal(err)
	}
	paused := mustCreateWake(t, e, other.ID)
	if _, err := e.PatchSchedule(paused.ID, store.SchedulePaused); err != nil {
		t.Fatal(err)
	}

	n, err := e.PullInterruptedWorkerWakes()
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("pulled %d, only the future wake on the stopped dispatch", n)
	}
	got, err := e.Store().GetSchedule(deadWake.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.NextRunAt.After(e.clock()) {
		t.Fatalf("stopped dispatch still waits until %s", got.NextRunAt)
	}
	untouched, err := e.Store().GetSchedule(liveWake.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !untouched.NextRunAt.After(e.clock()) {
		t.Fatal("a conversation with no stopped dispatch must keep its timer")
	}
	already, err := e.Store().GetSchedule(due.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !already.NextRunAt.Equal(past.UTC()) && !already.NextRunAt.Equal(past) {
		t.Fatalf("an already-due wake moved: %s", already.NextRunAt)
	}
}

func TestStartupLeavesAWakeAloneWhileTheConversationIsRunning(t *testing.T) {
	e := newTestEngine(t)
	th, err := e.CreateThread("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	turn := plantUnfinishedTurn(t, e, th.ID, "continue the leftover request")
	plantProcessStoppedWorker(t, e, th.ID, turn.ID, "worker-2", "worker")
	sch := mustCreateWake(t, e, th.ID)
	before, err := e.Store().GetSchedule(sch.ID)
	if err != nil {
		t.Fatal(err)
	}
	rt := e.runtimeFor(th.ID)
	rt.mu.Lock()
	rt.running = true
	rt.mu.Unlock()

	n, err := e.PullInterruptedWorkerWakes()
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("a live turn was pulled: %d", n)
	}
	got, err := e.Store().GetSchedule(sch.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !got.NextRunAt.Equal(before.NextRunAt) {
		t.Fatalf("next moved from %s to %s", before.NextRunAt, got.NextRunAt)
	}
}

func TestScheduleWakeRefusesUntilAReplacementDispatchStarts(t *testing.T) {
	e := newTestEngine(t)
	th, err := e.CreateThread("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	turn := plantUnfinishedTurn(t, e, th.ID, "continue the leftover request")
	plantProcessStoppedWorker(t, e, th.ID, turn.ID, "worker-2", "worker")

	out, err := e.scheduleWakeJSON(th.ID, turn.ID, `{"prompt":"`+scheduleToolWaitPrompt+`","every_s":60}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `"ok":false`) || !strings.Contains(out, "worker-2") {
		t.Fatalf("wake armed over a stopped sub-agent: %s", out)
	}
	if rows, err := e.ListSchedules(); err != nil || len(rows) != 0 {
		t.Fatalf("refusal stored a wake: %v %v", rows, err)
	}

	next := plantUnfinishedTurn(t, e, th.ID, "continue the leftover request")
	plantSpawnedWorker(t, e, th.ID, next.ID, "worker-3", "worker")
	out, err = e.scheduleWakeJSON(th.ID, next.ID, `{"prompt":"`+scheduleToolWaitPrompt+`","every_s":60}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `"ok":true`) {
		t.Fatalf("a newer spawn must allow the wait: %s", out)
	}
}

func TestReportScheduleRefusesToRearmWhileThoseSubAgentsStayStopped(t *testing.T) {
	e := newTestEngine(t)
	th, err := e.CreateThread("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	sch := mustCreateWake(t, e, th.ID)
	before, err := e.Store().GetSchedule(sch.ID)
	if err != nil {
		t.Fatal(err)
	}
	run := &store.ScheduleRun{ScheduleID: sch.ID, ThreadID: th.ID, Status: store.ScheduleRunRunning}
	if err := e.Store().CreateRun(run); err != nil {
		t.Fatal(err)
	}
	turn := mustStoredTurn(t, e, th.ID, store.Turn{ScheduleContinue: true, ScheduleRunID: run.ID})
	plantProcessStoppedWorker(t, e, th.ID, turn.ID, "worker-2", "worker")

	out, err := e.reportScheduleJSON(th.ID, turn.ID, `{"findings":"note","next_in_s":120}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `"ok":false`) || !strings.Contains(out, "worker-2") {
		t.Fatalf("recadence armed over a stopped sub-agent: %s", out)
	}
	got, err := e.Store().GetSchedule(sch.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.EveryS != before.EveryS || !got.NextRunAt.Equal(before.NextRunAt) {
		t.Fatalf("refusal changed the wait: before=%+v after=%+v", before, got)
	}

	out, err = e.reportScheduleJSON(th.ID, turn.ID, `{"findings":"note"}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `"ok":true`) {
		t.Fatalf("a report that does not arm a wait must still land: %s", out)
	}
}

func TestShouldPullInterruptedWake(t *testing.T) {
	now := time.Date(2026, 9, 30, 8, 0, 0, 0, time.UTC)
	future := now.Add(time.Minute)
	row := store.Schedule{
		Kind: store.ScheduleThread, Status: store.ScheduleActive,
		ThreadID: "th", NextRunAt: future,
	}
	if !shouldPullInterruptedWake(row, false, now) {
		t.Fatal("a future thread wake on an idle conversation is the one to pull")
	}
	if shouldPullInterruptedWake(row, true, now) {
		t.Fatal("a live turn already owns the conversation")
	}
	row.NextRunAt = now
	if shouldPullInterruptedWake(row, false, now) {
		t.Fatal("an already-due wake keeps its timestamp")
	}
	row.NextRunAt = future
	row.Status = store.SchedulePaused
	if shouldPullInterruptedWake(row, false, now) {
		t.Fatal("paused")
	}
	row.Status = store.ScheduleActive
	row.Kind = store.ScheduleStandalone
	if shouldPullInterruptedWake(row, false, now) {
		t.Fatal("standalone")
	}
	row.Kind = store.ScheduleThread
	row.ThreadID = " "
	if shouldPullInterruptedWake(row, false, now) {
		t.Fatal("no conversation")
	}
}

func TestPullInterruptedWorkerWakesReportsAClosedStore(t *testing.T) {
	e := newTestEngine(t)
	if err := e.Store().Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := e.PullInterruptedWorkerWakes(); err == nil {
		t.Fatal("a closed store must surface on the pull")
	}
	out, err := e.scheduleWakeJSON("th", "tn", `{"prompt":"`+scheduleToolWaitPrompt+`","every_s":60}`)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, `"ok":false`) {
		t.Fatalf("a closed store must fail the wake: %s", out)
	}
	kept := e.appendInterruptedWorkerCue("th", []adk.Message{schema.UserMessage("keep")})
	if len(kept) != 1 || kept[0].Content != "keep" {
		t.Fatalf("a closed store must leave the request in place: %+v", kept)
	}
}
