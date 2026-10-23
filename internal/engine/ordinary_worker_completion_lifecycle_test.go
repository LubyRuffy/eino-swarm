package engine

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/provider"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/cloudwego/eino/schema"
)

const ordinaryPrematureAnswer = "The delegated operation is complete."
const ordinaryCollectedAnswer = "The original worker results were collected."
const ordinaryCollectionCue = "The manager's completion attempt was withheld because worker results have not been collected"

type ordinaryLifecycleInput struct {
	Messages []*schema.Message `json:"messages"`
	Stream   bool              `json:"stream"`
	Tools    []struct {
		Function struct{ Name string } `json:"function"`
	} `json:"tools"`
	ToolChoice string `json:"tool_choice"`
}

type ordinaryLifecycleWait struct {
	Agents []struct {
		AgentID    string `json:"agent_id"`
		Generation int    `json:"generation"`
		Status     string `json:"status"`
		Result     string `json:"result"`
	} `json:"agents"`
	TimedOut bool `json:"timed_out"`
}

type ordinaryLifecycleFixture struct {
	e                    *Engine
	thread               *store.Thread
	turn                 *store.Turn
	mode                 string
	count                int
	schedule             string
	managers             atomic.Int32
	workers              atomic.Int32
	policies             atomic.Int32
	mu                   sync.Mutex
	ids                  []string
	wait                 ordinaryLifecycleWait
	entered              []chan struct{}
	release              []chan struct{}
	cancelled            []chan struct{}
	released             []atomic.Bool
	relOnce              []sync.Once
	candidate            chan struct{}
	corrected            chan struct{}
	allowBody            chan struct{}
	policy               chan struct{}
	allowPolicy          chan struct{}
	resumed              chan struct{}
	resumedCancelled     chan struct{}
	bodyOnce, policyOnce sync.Once
}

func newOrdinaryLifecycleFixture(t *testing.T, mode string) *ordinaryLifecycleFixture {
	t.Helper()
	restarting := mode == "restart" || mode == "restart-collect"
	f := &ordinaryLifecycleFixture{mode: mode, count: 1, candidate: make(chan struct{}), corrected: make(chan struct{}), allowBody: make(chan struct{}), policy: make(chan struct{}), allowPolicy: make(chan struct{}), resumed: make(chan struct{}), resumedCancelled: make(chan struct{})}
	if mode == "partial" {
		f.count = 2
	}
	f.ids = make([]string, f.count)
	f.entered, f.release, f.cancelled = make([]chan struct{}, f.count), make([]chan struct{}, f.count), make([]chan struct{}, f.count)
	f.released, f.relOnce = make([]atomic.Bool, f.count), make([]sync.Once, f.count)
	for i := 0; i < f.count; i++ {
		f.entered[i], f.release[i], f.cancelled[i] = make(chan struct{}), make(chan struct{}), make(chan struct{})
	}
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "" || r.URL.Path != "/chat/completions" {
			t.Error("ordinary lifecycle fixture escaped keyless loopback")
		}
		var input ordinaryLifecycleInput
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			t.Error(err)
			http.Error(w, "invalid fixture request", http.StatusBadRequest)
			return
		}
		if len(input.Messages) > 0 && input.Messages[0].Role == schema.System && input.Messages[0].Content == scheduleAnswerPrompt {
			if input.Stream || len(input.Tools) != 0 || input.ToolChoice != "none" || len(input.Messages) != 2 || input.Messages[1].Role != schema.User {
				t.Error("schedule policy must remain separate, non-streaming and tool-free")
			}
			if f.policies.Add(1) == 1 && (mode == "goal-revoked" || mode == "wake-revoked") {
				close(f.policy)
				select {
				case <-f.allowPolicy:
				case <-r.Context().Done():
					return
				}
			}
			writeContextFlowReply(w, false, map[string]any{"content": `{"claims":[]}`}, "stop")
			return
		}
		manager := false
		for _, tool := range input.Tools {
			manager = manager || tool.Function.Name == "spawn_agent"
		}
		if !manager {
			i := int(f.workers.Add(1)) - 1
			if restarting && i == 1 {
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = fmt.Fprint(w, "data: {\"id\":\"ordinary-restored\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"reasoning_content\":\"Continuing the assigned operation.\"},\"finish_reason\":null}]}\n\n")
				w.(http.Flusher).Flush()
				close(f.resumed)
				if mode == "restart-collect" {
					select {
					case <-f.release[0]:
						writeContextFlowReply(w, true, map[string]any{"content": f.result(0)}, "stop")
					case <-r.Context().Done():
						close(f.resumedCancelled)
					}
				} else {
					<-r.Context().Done()
					close(f.resumedCancelled)
				}
				return
			}
			if i >= f.count {
				t.Error("completion retry restarted a worker")
				http.Error(w, "unexpected worker", http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = fmt.Fprint(w, "data: {\"id\":\"ordinary-lifecycle\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"reasoning_content\":\"Inspecting the assigned inputs.\"},\"finish_reason\":null}]}\n\n")
			w.(http.Flusher).Flush()
			close(f.entered[i])
			select {
			case <-f.release[i]:
				writeContextFlowReply(w, true, map[string]any{"content": f.result(i)}, "stop")
			case <-r.Context().Done():
				if !f.released[i].Load() {
					close(f.cancelled[i])
				}
			}
			return
		}
		f.readIDs(t, input.Messages)
		call := func(id, name string, args any) {
			payload, _ := json.Marshal(args)
			writeContextFlowReply(w, true, map[string]any{"tool_calls": []any{map[string]any{"index": 0, "id": id, "type": "function", "function": map[string]any{"name": name, "arguments": string(payload)}}}}, "tool_calls")
		}
		step := int(f.managers.Add(1))
		if step <= f.count {
			// Serial spawn handshakes make each real worker HTTP slot stable.
			if step > 1 {
				select {
				case <-f.entered[step-2]:
				case <-r.Context().Done():
					return
				}
			}
			call(fmt.Sprintf("lifecycle-spawn-%d", step), "spawn_agent", map[string]any{"role": fmt.Sprintf("verifier%d", step), "task": "Inspect the assigned inputs and report the verified result."})
			return
		}
		switch step - f.count {
		case 1:
			for _, entered := range f.entered {
				select {
				case <-entered:
				case <-r.Context().Done():
					return
				}
			}
			close(f.candidate)
			if mode == "finished" {
				select {
				case <-f.allowBody:
				case <-r.Context().Done():
					return
				}
			}
			writeContextFlowReply(w, true, map[string]any{"content": ordinaryPrematureAnswer}, "stop")
		case 2:
			found := false
			for _, message := range input.Messages {
				found = found || message.Role == schema.User && strings.HasPrefix(message.Content, ordinaryCollectionCue)
				if message.Role == schema.Assistant && strings.Contains(message.Content, ordinaryPrematureAnswer) {
					t.Error("uncollected candidate reached corrective model input")
				}
			}
			if !found {
				t.Error("corrective model request did not carry the runtime collection cue")
			}
			close(f.corrected)
			if restarting {
				<-r.Context().Done()
				return
			} else if mode == "repeat" || mode == "continue" {
				writeContextFlowReply(w, true, map[string]any{"content": ordinaryPrematureAnswer}, "stop")
			} else if mode == "close" {
				call("lifecycle-close", "close_agent", map[string]any{"agent_id": f.workerIDs()[0]})
			} else {
				timeout := 30
				if mode == "timeout" {
					timeout = 1
				}
				call("lifecycle-wait", "wait_agents", map[string]any{"agent_ids": f.workerIDs(), "timeout_s": timeout})
			}
		case 3:
			if mode == "continue" || mode == "restart-collect" {
				call("lifecycle-wait", "wait_agents", map[string]any{"agent_ids": f.workerIDs(), "timeout_s": 30})
				return
			}
			if mode == "restart" {
				writeContextFlowReply(w, true, map[string]any{"content": ordinaryPrematureAnswer}, "stop")
				return
			}
			if mode == "close" {
				found := false
				for _, message := range input.Messages {
					if message.Role != schema.Tool || message.ToolCallID != "lifecycle-close" {
						continue
					}
					var receipt struct {
						AgentID   string `json:"agent_id"`
						Cancelled bool   `json:"cancelled"`
					}
					_ = json.Unmarshal([]byte(message.Content), &receipt)
					found = receipt.Cancelled && receipt.AgentID == f.workerIDs()[0]
				}
				if !found {
					t.Error("explicit close did not return its actual cancellation receipt")
				}
				select {
				case <-f.cancelled[0]:
				case <-r.Context().Done():
					return
				}
			} else {
				found := false
				for _, message := range input.Messages {
					if message.Role != schema.Tool || message.ToolCallID != "lifecycle-wait" {
						continue
					}
					var wait ordinaryLifecycleWait
					if err := json.Unmarshal([]byte(message.Content), &wait); err != nil {
						t.Error(err)
					}
					f.mu.Lock()
					f.wait = wait
					f.mu.Unlock()
					found = len(wait.Agents) == f.count
				}
				if !found {
					t.Error("manager did not receive the actual wait snapshot")
				}
			}
			answer := ordinaryCollectedAnswer
			if mode == "timeout" || mode == "partial" {
				answer = ordinaryPrematureAnswer
			}
			if mode == "close" {
				answer = "The unfinished delegation was explicitly closed."
			}
			writeContextFlowReply(w, true, map[string]any{"content": answer}, "stop")
		case 4:
			if mode != "continue" && mode != "restart-collect" {
				t.Errorf("fixture received an unexpected manager attempt: %d", step)
			}
			found := false
			for _, message := range input.Messages {
				if message.Role != schema.Tool || message.ToolCallID != "lifecycle-wait" {
					continue
				}
				var wait ordinaryLifecycleWait
				if err := json.Unmarshal([]byte(message.Content), &wait); err != nil {
					t.Error(err)
				}
				found = len(wait.Agents) == 1 && wait.Agents[0].AgentID == f.workerIDs()[0] && wait.Agents[0].Status == "done" && wait.Agents[0].Result == f.result(0)
			}
			if !found {
				t.Error("human extension did not collect the original worker's actual result")
			}
			writeContextFlowReply(w, true, map[string]any{"content": ordinaryCollectedAnswer}, "stop")
		default:
			t.Errorf("fixture received an unexpected manager attempt: %d", step)
			writeContextFlowReply(w, true, map[string]any{"content": ordinaryPrematureAnswer}, "stop")
		}
	}))
	t.Cleanup(endpoint.Close)
	f.e = newTestEngine(t)
	f.e.cfg.Swarm.AutoTitle = false
	f.e.cfg.Memory.Enabled, f.e.cfg.Memory.AutoReview = false, false
	f.e.cfg.Models.Providers[0].BaseURL, f.e.cfg.Models.Providers[0].APIKey = endpoint.URL, ""
	f.e.cfg.Models.Providers[0].Model = "offline-ordinary-lifecycle"
	f.e.pool = provider.New(f.e.cfg)
	var err error
	f.thread, err = f.e.CreateThread("ordinary completion boundary", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if mode == "goal-revoked" {
		if err := f.e.SetThreadGoal(f.thread.ID, "Inspect the remaining assigned work."); err != nil {
			t.Fatal(err)
		}
	} else if mode == "wake-revoked" {
		wake, err := f.e.CreateSchedule(ScheduleInput{Kind: store.ScheduleThread, ThreadID: f.thread.ID, EveryS: 3600, Prompt: "Inspect the remaining work.", CreatedBy: store.ScheduleCreatedHuman})
		if err != nil {
			t.Fatal(err)
		}
		f.schedule = wake.ID
	}
	t.Cleanup(func() {
		_ = f.e.Interrupt(f.thread.ID)
		f.bodyOnce.Do(func() { close(f.allowBody) })
		f.policyOnce.Do(func() { close(f.allowPolicy) })
		for i := 0; i < f.count; i++ {
			f.releaseWorker(i)
		}
	})
	return f
}

func (f *ordinaryLifecycleFixture) readIDs(t *testing.T, messages []*schema.Message) {
	t.Helper()
	for _, message := range messages {
		if message.Role != schema.Tool || !strings.HasPrefix(message.ToolCallID, "lifecycle-spawn-") {
			continue
		}
		var i int
		if _, err := fmt.Sscanf(message.ToolCallID, "lifecycle-spawn-%d", &i); err != nil || i < 1 || i > f.count {
			t.Error("spawn receipt does not belong to this fixture")
			continue
		}
		var receipt struct {
			AgentID string `json:"agent_id"`
		}
		if json.Unmarshal([]byte(message.Content), &receipt) != nil || receipt.AgentID == "" {
			t.Error("spawn did not return the actual worker identity")
			continue
		}
		f.mu.Lock()
		f.ids[i-1] = receipt.AgentID
		f.mu.Unlock()
	}
}

func (f *ordinaryLifecycleFixture) workerIDs() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.ids...)
}

func (f *ordinaryLifecycleFixture) result(i int) string {
	return fmt.Sprintf("Verified result from original worker slot %d.", i+1)
}

func (f *ordinaryLifecycleFixture) releaseWorker(i int) {
	f.relOnce[i].Do(func() { f.released[i].Store(true); close(f.release[i]) })
}

func (f *ordinaryLifecycleFixture) start(t *testing.T) {
	t.Helper()
	var err error
	f.turn, err = f.e.StartTurn(f.thread.ID, "Inspect the assigned inputs.")
	if err != nil {
		t.Fatal(err)
	}
}

func (f *ordinaryLifecycleFixture) pending() []swarm.PendingAgent {
	rt := f.e.runtimeFor(f.thread.ID)
	rt.mu.Lock()
	reg := rt.reg
	rt.mu.Unlock()
	if reg == nil {
		return nil
	}
	return reg.PendingAgents()
}

func ordinaryAwait(t *testing.T, ready <-chan struct{}, description string) {
	t.Helper()
	select {
	case <-ready:
	case <-time.After(5 * time.Second):
		t.Fatal(description)
	}
}

func ordinaryEventually(t *testing.T, check func() bool, description string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if check() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal(description)
}

func (f *ordinaryLifecycleFixture) assertWithheld(t *testing.T) {
	t.Helper()
	events, err := f.e.store.ListTurnEvents(f.turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range events {
		if event.Kind == "done" || event.Kind == KindCleanup || strings.Contains(event.Text, ordinaryPrematureAnswer) {
			t.Fatalf("uncollected completion escaped its turn: kind=%s seq=%d", event.Kind, event.Seq)
		}
	}
	if strings.Contains(replayDump(t, f.e, f.thread.ID), ordinaryPrematureAnswer) {
		t.Fatal("uncollected completion reached assistant replay")
	}
}

func (f *ordinaryLifecycleFixture) awaitPause(t *testing.T) {
	t.Helper()
	ordinaryEventually(t, func() bool {
		if !f.e.Status(f.thread.ID).AwaitingContinue {
			return false
		}
		events, _ := f.e.store.ListTurnEvents(f.turn.ID)
		for _, event := range events {
			if event.Kind == KindMaxIterations {
				return true
			}
		}
		return false
	}, "unresolved completion did not pause for an actual human continuation decision")
	turn, _ := f.e.store.GetTurn(f.turn.ID)
	if turn == nil || turn.Status != store.TurnRunning {
		t.Fatalf("cap pause ended the ordinary turn: %+v", turn)
	}
	f.assertWithheld(t)
	for i, cancelled := range f.cancelled {
		select {
		case <-cancelled:
			t.Fatalf("cap pause cancelled worker %d", i)
		default:
		}
	}
}

func (f *ordinaryLifecycleFixture) assertStopped(t *testing.T, i int) {
	t.Helper()
	if err := f.e.Interrupt(f.thread.ID); err != nil {
		t.Fatal(err)
	}
	finished := waitForTurn(t, f.e, f.turn.ID)
	if finished.Status != store.TurnCancelled || finished.Final != "" || finished.Error != "interrupted" {
		t.Fatalf("Stop lost its ordinary cancellation result: %+v", finished)
	}
	ordinaryAwait(t, f.cancelled[i], "Stop did not cancel the pending worker's real HTTP request")
	ordinaryEventually(t, func() bool {
		events, _ := f.e.store.ListTurnEvents(f.turn.ID)
		for _, event := range events {
			if event.Kind != "generation_interrupted" || event.AgentID != f.workerIDs()[i] {
				continue
			}
			var interruption struct{ Reason, Recovery string }
			if json.Unmarshal([]byte(event.Text), &interruption) == nil && interruption.Reason == "cancelled" && interruption.Recovery == "none" {
				return true
			}
		}
		return false
	}, "Stop did not retain cancelled/none on the actual worker generation")
}

func TestOrdinaryRepeatedCompletionPausesWithoutCancellingWorker(t *testing.T) {
	f := newOrdinaryLifecycleFixture(t, "repeat")
	f.start(t)
	f.awaitPause(t)
	if f.managers.Load() != 3 || f.workers.Load() != 1 || len(f.pending()) != 1 || !f.pending()[0].Running {
		t.Fatalf("ordinary pause changed worker ownership or retried indefinitely: requests=%d workers=%d pending=%+v", f.managers.Load(), f.workers.Load(), f.pending())
	}
	f.assertStopped(t, 0)
}

func TestOrdinaryHumanContinuationCollectsTheOriginalWorker(t *testing.T) {
	f := newOrdinaryLifecycleFixture(t, "continue")
	f.start(t)
	f.awaitPause(t)
	if err := f.e.ContinueTurn(f.thread.ID, true); err != nil {
		t.Fatal(err)
	}
	f.releaseWorker(0)
	finished := waitForTurn(t, f.e, f.turn.ID)
	if finished.Status != store.TurnDone || finished.Final != ordinaryCollectedAnswer || f.managers.Load() != 5 || f.workers.Load() != 1 || ordinaryCorrectionCount(t, f.e, f.turn.ID) != 2 {
		t.Fatalf("explicit human extension failed to renew one same-turn collection allowance: turn=%+v managers=%d worker=%d corrections=%d", finished, f.managers.Load(), f.workers.Load(), ordinaryCorrectionCount(t, f.e, f.turn.ID))
	}
}

func TestOrdinaryDecliningContinuationCancelsItsRealWorker(t *testing.T) {
	f := newOrdinaryLifecycleFixture(t, "repeat")
	f.start(t)
	f.awaitPause(t)
	if err := f.e.ContinueTurn(f.thread.ID, false); err != nil {
		t.Fatal(err)
	}
	finished := waitForTurn(t, f.e, f.turn.ID)
	if finished.Status != store.TurnCancelled || finished.Final != "" || !strings.Contains(finished.Error, "tool rounds") || f.managers.Load() != 3 || ordinaryCorrectionCount(t, f.e, f.turn.ID) != 1 {
		t.Fatalf("declined extension retried or exposed an uncollected answer: %+v", finished)
	}
	ordinaryAwait(t, f.cancelled[0], "declined extension did not cancel the worker's actual HTTP request")
}

func TestOrdinaryFinishedWorkerStillRequiresItsActualWaitReceipt(t *testing.T) {
	f := newOrdinaryLifecycleFixture(t, "finished")
	f.start(t)
	ordinaryAwait(t, f.candidate, "manager did not reach the uncollected finished-worker boundary")
	f.releaseWorker(0)
	ordinaryEventually(t, func() bool {
		_ = f.e.Status(f.thread.ID) // exercise the real Stats-prune boundary
		pending := f.pending()
		return len(pending) == 1 && !pending[0].Running && pending[0].Generation > 0
	}, "naturally completed worker vanished before its result was collected")
	f.bodyOnce.Do(func() { close(f.allowBody) })
	finished := waitForTurn(t, f.e, f.turn.ID)
	if finished.Status != store.TurnDone || finished.Final != ordinaryCollectedAnswer || f.managers.Load() != 4 || f.workers.Load() != 1 {
		t.Fatalf("finished-but-uncollected work bypassed actual receipt: turn=%+v manager=%d workers=%d", finished, f.managers.Load(), f.workers.Load())
	}
	f.mu.Lock()
	wait := f.wait
	f.mu.Unlock()
	if len(wait.Agents) != 1 || wait.Agents[0].AgentID != f.workerIDs()[0] || wait.Agents[0].Status != "done" || wait.Agents[0].Generation == 0 || wait.Agents[0].Result != f.result(0) {
		t.Fatalf("manager did not receive the exact generation's saved result: %+v", wait)
	}
}

func TestOrdinaryIncompleteWaitCannotApproveCompletion(t *testing.T) {
	for _, mode := range []string{"timeout", "partial"} {
		t.Run(mode, func(t *testing.T) {
			f := newOrdinaryLifecycleFixture(t, mode)
			f.start(t)
			ordinaryAwait(t, f.corrected, "manager did not attempt collection after premature completion")
			if mode == "partial" {
				f.releaseWorker(0)
			}
			f.awaitPause(t)
			f.mu.Lock()
			wait := f.wait
			f.mu.Unlock()
			if len(wait.Agents) != f.count || wait.TimedOut != (mode == "timeout") {
				t.Fatalf("fixture did not exercise a real incomplete wait snapshot: %+v", wait)
			}
			pending := f.pending()
			last := f.count - 1
			if len(pending) != 1 || pending[0].AgentID != f.workerIDs()[last] || !pending[0].Running {
				t.Fatalf("incomplete wait settled the wrong worker generation: %+v", pending)
			}
			if mode == "partial" && (wait.Agents[0].Status != "done" || wait.Agents[1].Status != "running" || wait.Agents[0].Result != f.result(0)) {
				t.Fatalf("first worker did not return independently of the second: %+v", wait)
			}
			if mode == "timeout" && wait.Agents[0].Status != "running" {
				t.Fatalf("timeout fixture unexpectedly completed its worker: %+v", wait)
			}
			if f.workers.Load() != int32(f.count) || f.managers.Load() != int32(f.count+3) {
				t.Fatalf("completion guard respawned or retried past its budget: manager=%d workers=%d", f.managers.Load(), f.workers.Load())
			}
			f.assertStopped(t, last)
		})
	}
}

func TestOrdinaryOwnerRevokedDuringPolicyStillCollectsItsWorker(t *testing.T) {
	for _, mode := range []string{"goal-revoked", "wake-revoked"} {
		t.Run(mode, func(t *testing.T) {
			f := newOrdinaryLifecycleFixture(t, mode)
			f.start(t)
			ordinaryAwait(t, f.policy, "scheduling policy never reached its owner-revocation barrier")
			if mode == "goal-revoked" {
				if err := f.e.CompleteThreadGoal(f.thread.ID, ""); err != nil {
					t.Fatal(err)
				}
			} else if err := f.e.CancelSchedule(f.schedule); err != nil {
				t.Fatal(err)
			}
			f.policyOnce.Do(func() { close(f.allowPolicy) })
			ordinaryAwait(t, f.corrected, "revoking the last owner during policy bypassed result collection")
			f.assertWithheld(t)
			select {
			case <-f.cancelled[0]:
				t.Fatal("owner revocation cancelled work before same-turn collection")
			default:
			}
			f.releaseWorker(0)
			finished := waitForTurn(t, f.e, f.turn.ID)
			if finished.Status != store.TurnDone || finished.Final != ordinaryCollectedAnswer || f.workers.Load() != 1 || f.managers.Load() != 4 {
				t.Fatalf("revoked owner prevented verified same-turn completion: turn=%+v managers=%d workers=%d", finished, f.managers.Load(), f.workers.Load())
			}
		})
	}
}

func TestOrdinaryExplicitCloseSettlesWithoutFabricatingWorkerSuccess(t *testing.T) {
	f := newOrdinaryLifecycleFixture(t, "close")
	f.start(t)
	finished := waitForTurn(t, f.e, f.turn.ID)
	if finished.Status != store.TurnDone || finished.Final != "The unfinished delegation was explicitly closed." || f.workers.Load() != 1 || f.managers.Load() != 4 {
		t.Fatalf("explicit disposal was mistaken for unresolved work or success: turn=%+v managers=%d workers=%d", finished, f.managers.Load(), f.workers.Load())
	}
	ordinaryAwait(t, f.cancelled[0], "close_agent did not cancel the worker's actual HTTP request")
	ordinaryEventually(t, func() bool {
		events, _ := f.e.store.ListTurnEvents(f.turn.ID)
		for _, event := range events {
			if event.Kind != "generation_interrupted" || event.AgentID != f.workerIDs()[0] {
				continue
			}
			var interruption struct{ Reason, Recovery string }
			if json.Unmarshal([]byte(event.Text), &interruption) == nil && interruption.Reason == "cancelled" && interruption.Recovery == "none" {
				return true
			}
		}
		return false
	}, "explicit close fabricated completion or automatic recovery")
}
