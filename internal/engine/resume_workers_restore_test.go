package engine

import (
	"encoding/json"
	"strings"
	"testing"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

func TestOrphanedWorkersTreatsCleanupAsFinished(t *testing.T) {
	running, finished := orphanedWorkers([]store.Event{
		{Kind: swarm.NotifySpawned.String(), AgentID: "worker-1", Role: "worker", Text: "do the assigned work"},
		{Kind: swarm.NotifySpawned.String(), AgentID: "helper-2", Role: "helper", Text: "do the assigned work"},
		{Kind: swarm.NotifyFinished.String(), AgentID: "helper-2", Role: "helper", Text: "helper done"},
		{Kind: KindCleanup, AgentID: swarm.DefaultManagerID, Text: "stopped 1 sub-agent(s) still running at the end of the turn"},
	})
	if len(running) != 0 {
		t.Fatalf("cleanup must not restore killed workers as running: %+v", running)
	}
	if len(finished) != 2 {
		t.Fatalf("finished=%+v", finished)
	}
	byID := map[string]swarm.FinishedWorker{}
	for _, w := range finished {
		byID[w.ID] = w
	}
	if byID["helper-2"].Result != "helper done" || byID["helper-2"].Err != nil {
		t.Fatalf("a finished sibling must stay done: %+v", byID["helper-2"])
	}
	if byID["worker-1"].Err == nil || byID["worker-1"].Err.Error() != cleanedUpWorkerErr {
		t.Fatalf("a killed worker must plant as stopped: %+v", byID["worker-1"])
	}
}

func TestOrphanedWorkersKeepsWorkersSpawnedAfterCleanup(t *testing.T) {
	running, finished := orphanedWorkers([]store.Event{
		{Kind: swarm.NotifySpawned.String(), AgentID: "worker-1", Role: "worker"},
		{Kind: KindCleanup, AgentID: swarm.DefaultManagerID},
		{Kind: swarm.NotifySpawned.String(), AgentID: "helper-2", Role: "helper"},
	})
	if len(running) != 1 || running[0].ID != "helper-2" {
		t.Fatalf("running=%+v", running)
	}
	if len(finished) != 1 || finished[0].ID != "worker-1" {
		t.Fatalf("finished=%+v", finished)
	}
}

func TestWorkersFromThreadSeesEarlierTurns(t *testing.T) {
	e := newTestEngine(t)
	th, err := e.CreateThread("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	prev := plantUnfinishedTurn(t, e, th.ID, "previous session")
	plantFinishedWorker(t, e, th.ID, prev.ID, "helper-2", "helper", "helper done")
	if err := e.Store().FinishTurn(prev.ID, store.TurnDone, "session ended", ""); err != nil {
		t.Fatal(err)
	}
	next := plantUnfinishedTurn(t, e, th.ID, "this session")
	running, finished := e.workersFromTurn(next)
	if len(running) != 0 || len(finished) != 0 {
		t.Fatalf("the new empty turn must not own the leftover: running=%+v finished=%+v", running, finished)
	}
	running, finished = e.workersFromThread(th.ID)
	if len(running) != 0 || len(finished) != 1 || finished[0].ID != "helper-2" || finished[0].Result != "helper done" {
		t.Fatalf("conversation leftover: running=%+v finished=%+v", running, finished)
	}
}

func TestWorkersFromThreadNilAndClosedStore(t *testing.T) {
	var missing *Engine
	running, finished := missing.workersFromThread("th")
	if running != nil || finished != nil {
		t.Fatalf("nil engine: %v %v", running, finished)
	}
	e := newTestEngine(t)
	running, finished = e.workersFromThread("")
	if running != nil || finished != nil {
		t.Fatalf("blank id: %v %v", running, finished)
	}
	_ = e.Store().Close()
	running, finished = e.workersFromThread("th_x")
	if running != nil || finished != nil {
		t.Fatalf("closed store: %v %v", running, finished)
	}
}

func TestAttachLeftoverWorkersPlantsAndPinsAContinuation(t *testing.T) {
	e := newTestEngine(t)
	th, err := e.CreateThread("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	prev := plantUnfinishedTurn(t, e, th.ID, "previous session")
	plantFinishedWorker(t, e, th.ID, prev.ID, "worker-1", "worker", "already on disk")
	if err := e.Store().FinishTurn(prev.ID, store.TurnDone, "session ended", ""); err != nil {
		t.Fatal(err)
	}
	rt := e.runtimeFor(th.ID)
	msgs := rt.attachLeftoverWorkers(false, true, 0, nil)
	running, finished := rt.takeWorkerRestore()
	if len(running) != 0 || len(finished) != 1 || finished[0].ID != "worker-1" || finished[0].Result != "already on disk" {
		t.Fatalf("restore=%+v finished=%+v", running, finished)
	}
	ids := spawnIDsInMessages(msgs)
	if len(ids) != 1 || ids[0] != "worker-1" {
		t.Fatalf("continuation must pin leftover ids: %v", ids)
	}
}

func TestAttachLeftoverWorkersDoesNotRestoreIntoAParkedRegistry(t *testing.T) {
	e := newTestEngine(t)
	th, err := e.CreateThread("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	prev := plantUnfinishedTurn(t, e, th.ID, "previous session")
	plantSpawnedWorker(t, e, th.ID, prev.ID, "worker-1", "worker")
	if err := e.Store().FinishTurn(prev.ID, store.TurnDone, "session ended", ""); err != nil {
		t.Fatal(err)
	}
	rt := e.runtimeFor(th.ID)
	msgs := rt.attachLeftoverWorkers(true, true, 1, nil)
	running, finished := rt.takeWorkerRestore()
	if len(running)+len(finished) != 0 {
		t.Fatalf("a parked registry already holds them: running=%+v finished=%+v", running, finished)
	}
	ids := spawnIDsInMessages(msgs)
	if len(ids) != 1 || ids[0] != "worker-1" {
		t.Fatalf("the parked path must still pin ids: %v", ids)
	}
	if !hasUserContent(msgs, parkedWorkersCue) {
		t.Fatal("live parked workers must keep the continue cue")
	}
}

func TestAttachLeftoverWorkersGuards(t *testing.T) {
	var rt *runtime
	if got := rt.attachLeftoverWorkers(false, true, 0, nil); got != nil {
		t.Fatalf("nil runtime: %+v", got)
	}
	empty := &runtime{}
	seed := []adk.Message{schema.UserMessage("keep")}
	if got := empty.attachLeftoverWorkers(false, true, 0, seed); len(got) != 1 {
		t.Fatalf("nil engine: %+v", got)
	}

	e := newTestEngine(t)
	th, err := e.CreateThread("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	prev := plantUnfinishedTurn(t, e, th.ID, "previous session")
	plantSpawnedWorker(t, e, th.ID, prev.ID, "worker-1", "worker")
	if err := e.Store().FinishTurn(prev.ID, store.TurnDone, "session ended", ""); err != nil {
		t.Fatal(err)
	}
	live := e.runtimeFor(th.ID)
	msgs := live.attachLeftoverWorkers(false, true, 0, []adk.Message{schema.UserMessage(resumeWorkersCue)})
	if n := countUserContent(msgs, resumeWorkersCue); n != 1 {
		t.Fatalf("resume cue duplicated: %d", n)
	}
	msgs = live.attachLeftoverWorkers(true, true, 1, []adk.Message{schema.UserMessage(parkedWorkersCue)})
	if n := countUserContent(msgs, parkedWorkersCue); n != 1 {
		t.Fatalf("parked cue duplicated: %d", n)
	}
}

func countUserContent(msgs []adk.Message, text string) int {
	n := 0
	for _, m := range msgs {
		if m != nil && m.Role == schema.User && m.Content == text {
			n++
		}
	}
	return n
}

func TestAttachLeftoverWorkersDoesNotPinARegularTurn(t *testing.T) {
	e := newTestEngine(t)
	th, err := e.CreateThread("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	prev := plantUnfinishedTurn(t, e, th.ID, "previous session")
	plantFinishedWorker(t, e, th.ID, prev.ID, "worker-1", "worker", "already on disk")
	if err := e.Store().FinishTurn(prev.ID, store.TurnDone, "session ended", ""); err != nil {
		t.Fatal(err)
	}
	rt := e.runtimeFor(th.ID)
	msgs := rt.attachLeftoverWorkers(false, false, 0, []adk.Message{schema.UserMessage("a new request")})
	_, finished := rt.takeWorkerRestore()
	if len(finished) != 1 || finished[0].ID != "worker-1" {
		t.Fatalf("a fresh registry must still plant leftovers: %+v", finished)
	}
	if ids := spawnIDsInMessages(msgs); len(ids) != 0 {
		t.Fatalf("a regular turn must not look like those workers were just spawned: %v", ids)
	}
}

func TestNextGoalSessionAfterRestartResolvesLeftoverWorkers(t *testing.T) {
	e := newTestEngine(t)
	th, err := e.CreateThread("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := e.SetThreadGoal(th.ID, "keep the standing objective"); err != nil {
		t.Fatal(err)
	}
	prev := plantUnfinishedTurn(t, e, th.ID, "previous session")
	plantFinishedWorker(t, e, th.ID, prev.ID, "worker-1", "worker", "already on disk")
	if err := e.Store().FinishTurn(prev.ID, store.TurnDone, "session ended", ""); err != nil {
		t.Fatal(err)
	}

	next, err := e.StartTurnInput(th.ID, UserInput{Text: GoalContinueText(), ContinueGoal: true})
	if err != nil {
		t.Fatal(err)
	}
	got := waitForTurn(t, e, next.ID)
	if got.Status != store.TurnDone {
		t.Fatalf("status=%s err=%s", got.Status, got.Error)
	}
	events, err := e.Store().ListTurnEvents(next.ID)
	if err != nil {
		t.Fatal(err)
	}
	status, result, errText, found := latestWaitFor(events, "worker-1")
	if !found {
		t.Fatal("wait_agents never listed the leftover worker")
	}
	if status == "unknown" || strings.Contains(errText, "unknown agent") {
		t.Fatalf("wait_agents lost leftover workers after a restart: status=%q err=%q", status, errText)
	}
	if status != "done" || result != "already on disk" {
		t.Fatalf("planted result vanished: status=%q result=%q err=%q", status, result, errText)
	}
}

func TestNextGoalSessionAfterRestartRestoresRunningWorkers(t *testing.T) {
	e := newTestEngine(t)
	th, err := e.CreateThread("", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := e.SetThreadGoal(th.ID, "keep the standing objective"); err != nil {
		t.Fatal(err)
	}
	prev := plantUnfinishedTurn(t, e, th.ID, "previous session")
	plantSpawnedWorker(t, e, th.ID, prev.ID, "worker-1", "worker")
	if err := e.Store().FinishTurn(prev.ID, store.TurnDone, "session ended", ""); err != nil {
		t.Fatal(err)
	}

	next, err := e.StartTurnInput(th.ID, UserInput{Text: GoalContinueText(), ContinueGoal: true})
	if err != nil {
		t.Fatal(err)
	}
	got := waitForTurn(t, e, next.ID)
	if got.Status != store.TurnDone {
		t.Fatalf("status=%s err=%s", got.Status, got.Error)
	}
	events, err := e.Store().ListTurnEvents(next.ID)
	if err != nil {
		t.Fatal(err)
	}
	status, _, errText, found := latestWaitFor(events, "worker-1")
	if !found {
		t.Fatal("wait_agents never listed the restored worker")
	}
	if status == "unknown" || strings.Contains(errText, "unknown agent") {
		t.Fatalf("a parked worker died across the restart: status=%q err=%q", status, errText)
	}
}

func plantFinishedWorker(t *testing.T, e *Engine, threadID, turnID, id, role, result string) {
	t.Helper()
	plantSpawnedWorker(t, e, threadID, turnID, id, role)
	if err := e.Store().AppendEvent(&store.Event{
		ThreadID: threadID, TurnID: turnID,
		Kind: swarm.NotifyFinished.String(), AgentID: id, Role: role,
		Text: result,
	}); err != nil {
		t.Fatal(err)
	}
}

func latestWaitFor(events []store.Event, agentID string) (status, result, errText string, found bool) {
	for _, ev := range events {
		if ev.Kind != swarm.NotifyToolResult.String() || !strings.Contains(ev.Text, `"agents"`) {
			continue
		}
		var report struct {
			Agents []struct {
				AgentID string `json:"agent_id"`
				Status  string `json:"status"`
				Result  string `json:"result"`
				Err     string `json:"error"`
			} `json:"agents"`
		}
		if json.Unmarshal([]byte(ev.Text), &report) != nil {
			continue
		}
		for _, a := range report.Agents {
			if a.AgentID != agentID {
				continue
			}
			found = true
			status, result, errText = a.Status, a.Result, a.Err
		}
	}
	return
}
