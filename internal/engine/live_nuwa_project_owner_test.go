package engine

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/provider"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"gorm.io/gorm"
)

// The source is a separate thread that never moves. A live worker must not
// newly read that project's fact after its conversation moves to another project.
func TestNuwaLiveWorkerCannotReadFormerProjectAfterMove(t *testing.T) {
	t.Run("admission-refuses-foreign-registry-and-move-back-recovers", func(t *testing.T) {
		testNuwaLiveWorkerMove(t, false, false, "")
	})
	t.Run("parked-executor-read-refuses-former-project-and-stop-cleans", func(t *testing.T) {
		testNuwaLiveWorkerMove(t, true, false, "")
	})
	t.Run("admission-refuses-registry-with-only-collected-work", func(t *testing.T) {
		testNuwaLiveWorkerMove(t, false, true, "")
	})
}

func TestNuwaProjectMoveOrdersLiveAdmission(t *testing.T) {
	for _, origin := range []string{"wake", "goal"} {
		t.Run(origin, func(t *testing.T) {
			testNuwaLiveWorkerMove(t, false, false, origin)
		})
	}
}

func testNuwaLiveWorkerMove(t *testing.T, readWhileParked, collectFirst bool, interleave string) {
	const (
		providerID = "live-owner-fixture"
		planner    = "fixture/planner"
		executor   = "fixture/executor"
		marker     = "Former project only: independent source marker."
		readID     = "live-owner-read"
		writeID    = "live-owner-write"
	)
	workerEntered, adoption, readReply, writeReply, waitReply := make(chan struct{}), make(chan struct{}),
		make(chan string, 1), make(chan string, 1), make(chan string, 1)
	firstWait := make(chan string, 1)
	var managerCalls, workerCalls atomic.Int32
	var spawned atomic.Pointer[toolScopeWorkerID]
	var releaseOnce sync.Once
	releaseWorker := func() { releaseOnce.Do(func() { close(adoption) }) }
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct {
			scheduleCompletionRequest
			Model string `json:"model"`
		}
		fail := func(reason string) {
			t.Error(reason)
			http.Error(w, "unexpected live owner fixture input", http.StatusBadRequest)
		}
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			fail(err.Error())
			return
		}
		if len(request.Messages) > 0 && replyNoScheduleClaimsFixture(t, w, request.Messages[0].Content, request.Stream, len(request.Tools)) {
			return
		}
		result := func(id string) string { return nuwaValidationReceipt(request.scheduleCompletionRequest, id) }
		call := func(id, name, args string) { nuwaValidationTool(w, request.scheduleCompletionRequest, id, name, args) }
		answer := func(s string) { writeContextFlowReply(w, request.Stream, map[string]any{"content": s}, "stop") }
		switch request.Model {
		case executor:
			switch workerCalls.Add(1) {
			case 1:
				close(workerEntered)
				select {
				case <-adoption:
					call(readID, "read_work", `{"kind":"fact"}`)
				case <-r.Context().Done():
				}
			case 2:
				readReply <- result(readID)
				call(writeID, "record_work", `{"kind":"fact","title":"An obsolete write attempt"}`)
			case 3:
				writeReply <- result(writeID)
				if body := result(readID); strings.Contains(body, marker) {
					answer(body)
				} else {
					answer("Former project facts were not available.")
				}
			default:
				fail("unexpected worker request")
			}
		case planner:
			step := managerCalls.Add(1)
			if collectFirst {
				switch {
				case step == 3:
					firstWait <- result("live-owner-collect-first")
					call("live-owner-report-first", ToolReportSchedule, `{"findings":"The inspection ended."}`)
					return
				case step >= 4:
					step--
				}
			}
			switch step {
			case 1:
				call("live-owner-spawn", "spawn_agent", fmt.Sprintf(
					`{"role":"source-reader","task":"Inspect shared facts.","model_ref":%q,"tool_names":[]}`, nuwaModelRef(providerID, executor)))
			case 2:
				var identity toolScopeWorkerID
				if err := json.Unmarshal([]byte(result("live-owner-spawn")), &identity); err != nil || identity.AgentID == "" || identity.Generation <= 0 {
					fail("spawn did not return a real worker identity")
					return
				}
				spawned.Store(&identity)
				if collectFirst {
					releaseWorker()
					call("live-owner-collect-first", "wait_agents", fmt.Sprintf(`{"agent_ids":[%q],"timeout_s":5}`, identity.AgentID))
					return
				}
				call("live-owner-report-first", ToolReportSchedule, `{"findings":"The inspection is still running."}`)
			case 3, 6:
				answer("")
			case 4:
				if interleave == "goal" {
					<-r.Context().Done()
					return
				}
				// This is the adopting manager's actual request, after its event
				// accumulator was attached. Only now allow a new Worker read.
				identity := spawned.Load()
				if identity == nil {
					fail("missing spawn identity")
					return
				}
				releaseWorker()
				call("live-owner-wait", "wait_agents", fmt.Sprintf(`{"agent_ids":[%q],"timeout_s":5}`, identity.AgentID))
			case 5:
				waitReply <- result("live-owner-wait")
				call("live-owner-report-next", ToolReportSchedule, `{"findings":"The inspection ended."}`)
			default:
				fail("unexpected manager request")
			}
		default:
			fail("unexpected model route")
		}
	}))
	t.Cleanup(endpoint.Close)
	e := newTestEngine(t)
	t.Cleanup(releaseWorker)
	e.cfg.Swarm.AutoTitle = false
	e.cfg.Swarm.ComposerPredictions = false
	e.cfg.Memory.AutoReview = false
	e.cfg.Models = config.ModelsConfig{Default: providerID, Providers: []config.Provider{
		{ID: providerID, BaseURL: endpoint.URL, Model: planner, Catalog: []string{planner, executor}},
	}}
	e.pool = provider.New(e.cfg)

	original, err := e.CreateProject("Original project", "", t.TempDir(), false, true)
	if err != nil {
		t.Fatal(err)
	}
	destination, err := e.CreateProject("Destination project", "", t.TempDir(), false, true)
	if err != nil {
		t.Fatal(err)
	}
	source, err := e.CreateThread("Original source stays in place", "", original.ID)
	if err != nil {
		t.Fatal(err)
	}
	sourceTurn := &store.Turn{ThreadID: source.ID, ProjectID: original.ID}
	if err := e.store.CreateTurn(sourceTurn); err != nil {
		t.Fatal(err)
	}
	seed := store.NuwaNode{ProjectID: original.ID, ThreadID: source.ID, TurnID: sourceTurn.ID,
		AgentID: "manager", Kind: "fact", Title: "Original project fact", Summary: marker}
	if err := e.store.UpsertNuwaNode(t.Context(), &seed, 0); err != nil {
		t.Fatal(err)
	}
	thread, err := e.CreateThread("Conversation moves with a parked worker", "", original.ID)
	if err != nil {
		t.Fatal(err)
	}
	wake, err := e.CreateSchedule(ScheduleInput{Kind: store.ScheduleThread, ThreadID: thread.ID, Title: "Project inspection",
		Prompt: "Check the inspection progress.", EveryS: 3600, CreatedBy: store.ScheduleCreatedHuman})
	if err != nil {
		t.Fatal(err)
	}
	first, err := e.RunScheduleNow(wake.ID)
	if err != nil {
		t.Fatal(err)
	}
	first = waitForTurn(t, e, first.ID)
	waitSettled(t, e, thread.ID)
	if first.Status != store.TurnDone || first.ProjectID != original.ID {
		t.Fatalf("first wake did not complete in original project: %+v", first)
	}
	select {
	case <-workerEntered:
	case <-time.After(5 * time.Second):
		t.Fatal("worker did not reach its actual executor endpoint")
	}
	runtime := e.runtimeFor(thread.ID)
	runtime.mu.Lock()
	parked := runtime.parked
	runtime.mu.Unlock()
	identity := spawned.Load()
	if parked == nil || identity == nil {
		t.Fatal("original worker was not parked under its actual Registry")
	}
	priorManagerCalls := int32(3)
	if collectFirst {
		priorManagerCalls = 4
		if len(firstWait) != 1 || !strings.Contains(<-firstWait, marker) || len(parked.PendingAgents()) != 0 {
			t.Fatal("the Registry did not park a previously collected Worker containing the original fact")
		}
	}
	if interleave != "" {
		if interleave == "goal" {
			if err := e.SetThreadGoal(thread.ID, "Collect the retained inspection."); err != nil {
				t.Fatal(err)
			}
		}
		testMoveWhileOwnerLookupReturns(t, e, thread.ID, first.ID, original.ID, destination.ID, interleave, parked,
			&managerCalls, priorManagerCalls, func() (*store.Turn, error) {
				if interleave == "goal" {
					return e.ResumeThreadGoal(thread.ID)
				}
				return e.RunScheduleNow(wake.ID)
			})
		return
	}
	if err := e.MoveThread(thread.ID, destination.ID); err != nil {
		t.Fatal(err)
	}
	moved, err := e.store.GetThread(thread.ID)
	if err != nil || moved.ProjectID != destination.ID {
		t.Fatalf("conversation did not move: %+v, %v", moved, err)
	}
	if _, nodes, err := e.store.NuwaSnapshot(t.Context(), destination.ID); err != nil || len(nodes) != 0 {
		t.Fatalf("destination already had data: %+v, %v", nodes, err)
	}
	if readWhileParked {
		releaseWorker()
		var read, write string
		select {
		case read = <-readReply:
		case <-time.After(5 * time.Second):
			t.Fatal("parked Worker did not receive its actual read response")
		}
		select {
		case write = <-writeReply:
		case <-time.After(5 * time.Second):
			t.Fatal("parked Worker did not receive its actual write response")
		}
		receipt, err := awaitNuwaToolResult(t.Context(), e.store, readID)
		if err != nil || receipt.TurnID != first.ID || receipt.AgentID != identity.AgentID ||
			receipt.WorkerGeneration != identity.Generation || receipt.ToolReceiptScope == "" {
			t.Fatalf("parked read lost its original actor: %+v, %v", receipt, err)
		}
		const ownerRefusal = "source thread belongs to another project"
		if !strings.Contains(read, ownerRefusal) || !strings.Contains(receipt.Text, ownerRefusal) ||
			!strings.Contains(write, ownerRefusal) || strings.Contains(read, marker) {
			t.Fatalf("parked read must refuse a moved owner: model_read=%t durable_read_seq=%d write_refused=%t",
				strings.Contains(read, marker), receipt.Seq, strings.Contains(write, ownerRefusal))
		}
		if err := e.Interrupt(thread.ID); err != nil {
			t.Fatal(err)
		}
		waitSettled(t, e, thread.ID)
		runtime.mu.Lock()
		retained := runtime.parked
		runtime.mu.Unlock()
		if retained != nil {
			t.Fatal("Stop left the mismatched Registry parked")
		}
		return
	}
	second, err := e.RunScheduleNow(wake.ID)
	moveBack := errors.Is(err, errTurnProjectOwnerChanged)
	if moveBack {
		runtime.mu.Lock()
		retained := runtime.parked
		runtime.mu.Unlock()
		pending := parked.PendingAgents()
		validPending := len(pending) == 1 && pending[0].AgentID == identity.AgentID &&
			pending[0].Generation == identity.Generation && pending[0].Running
		if collectFirst {
			validPending = len(pending) == 0
		}
		if second != nil || retained != parked || managerCalls.Load() != priorManagerCalls || !validPending {
			t.Fatalf("admission refusal changed pending ownership: turn=%+v manager=%d pending=%+v",
				second, managerCalls.Load(), pending)
		}
		if err := e.SetThreadGoal(thread.ID, "Collect the retained findings."); err != nil {
			t.Fatal(err)
		}
		before, beforeErr := e.store.ListTurns(thread.ID)
		if beforeErr != nil {
			t.Fatal(beforeErr)
		}
		if resumed, err := e.ResumeThreadGoal(thread.ID); resumed != nil || !errors.Is(err, errTurnProjectOwnerChanged) {
			t.Fatalf("Continue should refuse before opening a successor: %+v, %v", resumed, err)
		}
		after, afterErr := e.store.ListTurns(thread.ID)
		if afterErr != nil || len(after) != len(before) || managerCalls.Load() != priorManagerCalls {
			t.Fatalf("Continue changed turn history or reached the model: before=%d after=%d calls=%d err=%v",
				len(before), len(after), managerCalls.Load(), afterErr)
		}
		if err := e.MoveThread(thread.ID, original.ID); err != nil {
			t.Fatal(err)
		}
		second, err = e.RunScheduleNow(wake.ID)
	}
	if err != nil {
		t.Fatal(err)
	}
	second = waitForTurn(t, e, second.ID)
	waitSettled(t, e, thread.ID)
	expectedProject := destination.ID
	if moveBack {
		expectedProject = original.ID
	}
	if second.Status != store.TurnDone || second.ProjectID != expectedProject {
		t.Fatalf("adopting wake did not complete in expected project: %+v", second)
	}
	if len(readReply) != 1 || len(writeReply) != 1 || len(waitReply) != 1 {
		t.Fatalf("missing actual model receipts: manager=%d worker=%d", managerCalls.Load(), workerCalls.Load())
	}
	read, write, wait := <-readReply, <-writeReply, <-waitReply
	var collection toolScopeWait
	if err := json.Unmarshal([]byte(wait), &collection); err != nil || collection.TimedOut ||
		len(collection.Agents) != 1 || collection.Agents[0].toolScopeWorkerID != *identity || collection.Agents[0].Status != "done" {
		t.Fatalf("adopting manager did not observe the original worker generation: %s, %v", wait, err)
	}
	if collectFirst {
		if !moveBack {
			t.Fatalf("foreign manager adopted a Registry with only collected work: same_gen_wait=true former_project_fact=%t",
				strings.Contains(wait, marker))
		}
		if !strings.Contains(wait, marker) {
			t.Fatal("original project could not see the prior collected result after moving back")
		}
		return
	}
	receipt, err := awaitNuwaToolResult(t.Context(), e.store, readID)
	if err != nil || receipt.TurnID != second.ID || receipt.AgentID != identity.AgentID ||
		receipt.WorkerGeneration != identity.Generation || receipt.ToolReceiptScope == "" {
		t.Fatalf("read did not come from the original execution under the adopting turn: %+v, %v", receipt, err)
	}
	if moveBack {
		if !strings.Contains(read, marker) || !strings.Contains(wait, marker) || !strings.Contains(write, "\"ok\":true") {
			t.Fatalf("same Worker could not access and return its original facts after moving back: read=%t wait=%t write=%s",
				strings.Contains(read, marker), strings.Contains(wait, marker), write)
		}
		return
	}
	if !strings.Contains(write, "source thread belongs to another project") {
		t.Fatalf("expected existing moved-owner write refusal: %q", write)
	}
	if strings.Contains(read, marker) || strings.Contains(receipt.Text, marker) || strings.Contains(wait, marker) {
		t.Fatalf("former-project fact crossed ownership on retained worker %s@%d: from=%s to=%s durable_read_seq=%d model_read=%t parent_wait=%t",
			identity.AgentID, identity.Generation, first.ProjectID, second.ProjectID, receipt.Seq,
			strings.Contains(read, marker), strings.Contains(wait, marker))
	}
}

// Pause after the original owner was read by a standalone query. Pausing
// inside inheritance's write transaction would falsely serialize Move by
// occupying SQLite's only connection rather than proving engine ordering.
func testMoveWhileOwnerLookupReturns(t *testing.T, e *Engine, threadID, tag, old, destination, origin string,
	parked *swarm.Registry, calls *atomic.Int32, originalCalls int32, start func() (*store.Turn, error)) {
	t.Helper()
	before, err := e.store.ListTurns(threadID)
	if err != nil {
		t.Fatal(err)
	}
	eventsBefore, err := e.store.ListEvents(threadID, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	ownerRead, release := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(unblock)
	var armed, fired, inheritance atomic.Bool
	db := e.store.DB()
	if err := db.Callback().Query().After("gorm:query").Register("move-at-original-owner-read", func(tx *gorm.DB) {
		switch record := tx.Statement.Dest.(type) {
		case *store.Turn:
			if record.ID == tag {
				armed.Store(true)
			}
		case *store.Thread:
			if record.ID == threadID && record.ProjectID == old && armed.Load() && fired.CompareAndSwap(false, true) {
				close(ownerRead)
				<-release
			}
		}
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.Callback().Create().Before("gorm:create").Register("move-at-inheritance-order", func(tx *gorm.DB) {
		if event, ok := tx.Statement.Dest.(*store.Event); ok &&
			event.ThreadID == threadID && event.Kind == "history_registry_inherited" {
			inheritance.Store(true)
		}
	}); err != nil {
		t.Fatal(err)
	}
	type admission struct {
		turn *store.Turn
		err  error
	}
	type move struct {
		beforeInheritance bool
		err               error
	}
	admitted, moved := make(chan admission, 1), make(chan move, 1)
	go func() {
		turn, err := start()
		admitted <- admission{turn, err}
	}()
	select {
	case <-ownerRead:
	case <-time.After(5 * time.Second):
		unblock()
		outcome := <-admitted
		t.Fatalf("actual admission did not read original config owner: %+v", outcome)
	}
	moveStarted := make(chan struct{})
	go func() {
		close(moveStarted)
		err := e.MoveThread(threadID, destination)
		moved <- move{!inheritance.Load(), err}
	}()
	<-moveStarted
	var relocation move
	var joinedMove bool
	select {
	case relocation = <-moved:
		joinedMove = true
	case <-time.After(time.Second):
		// With correct owner ordering Move is waiting at the admission
		// lock; release the read so the two calls can finish in order.
	}
	unblock()
	var outcome admission
	select {
	case outcome = <-admitted:
	case <-time.After(5 * time.Second):
		t.Fatal("actual admission did not finish after release")
	}
	if !joinedMove {
		select {
		case relocation = <-moved:
		case <-time.After(5 * time.Second):
			t.Fatal("actual Move did not finish after release")
		}
	}
	current, err := e.store.GetThread(threadID)
	if err != nil || relocation.err != nil || current.ProjectID != destination {
		t.Fatalf("Move did not commit to destination: current=%+v read=%v move=%v", current, err, relocation.err)
	}
	if outcome.turn != nil {
		waitRestartState(t, func() bool { return calls.Load() > originalCalls })
	}
	after, err := e.store.ListTurns(threadID)
	if err != nil {
		t.Fatal(err)
	}
	eventsAfter, err := e.store.ListEvents(threadID, 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	newResumed, newInheritance := 0, 0
	for _, event := range eventsAfter[len(eventsBefore):] {
		switch event.Kind {
		case KindGoalResumed:
			newResumed++
		case "history_registry_inherited":
			newInheritance++
		}
	}
	if relocation.beforeInheritance {
		rt := e.runtimeFor(threadID)
		rt.mu.Lock()
		retained := rt.parked == parked
		rt.mu.Unlock()
		if outcome.turn != nil || !errors.Is(outcome.err, errTurnProjectOwnerChanged) || !retained ||
			newResumed != 0 || newInheritance != 0 || calls.Load() != originalCalls {
			t.Fatalf("Move committed before %s admission: current=%s retained=%v accepted=%v err=%v extra_turns=%d resumed=%d inheritance=%d actual_manager_requests=%d",
				origin, current.ProjectID, retained, outcome.turn != nil, outcome.err, len(after)-len(before),
				newResumed, newInheritance, calls.Load()-originalCalls)
		}
		if origin == "goal" && len(after) != len(before) {
			t.Fatalf("Move-first goal preflight created %d successor turns", len(after)-len(before))
		}
	} else if outcome.err != nil || outcome.turn == nil || outcome.turn.ProjectID != old ||
		newInheritance != 1 || len(after) != len(before)+1 || (origin == "goal" && newResumed != 1) {
		t.Fatalf("claim-first was not durably ordered before Move: turn=%+v err=%v extra_turns=%d resumed=%d inheritance=%d",
			outcome.turn, outcome.err, len(after)-len(before), newResumed, newInheritance)
	}
	if err := e.Interrupt(threadID); err != nil && !errors.Is(err, ErrIdle) {
		t.Fatal(err)
	}
	waitSettled(t, e, threadID)
}
