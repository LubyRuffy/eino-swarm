package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/provider"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

// Exercise the engine continuation as well as the Registry. Filtering every
// system role would erase this separately supplied constraint after a later cap.
func TestManagerMidturnRecoveryKeepsIndependentSystemAndPriorToolEvidenceOverHTTP(t *testing.T) {
	const constraint = "Independent system evidence: preserve the established operating boundary."
	const callID = "system-boundary-prior-effect"
	const answer = "The existing result remains grounded in its constraints."
	var baseSystem atomic.Value
	baseSystem.Store("")
	var firstSystems atomic.Value
	firstSystems.Store([]string{})
	e, wire := newScheduleCompletionHTTPEngine(t, func(w http.ResponseWriter, _ *http.Request, step int, request scheduleCompletionRequest) {
		var own, independent int
		var systems []string
		baseInstruction := baseSystem.Load().(string)
		for _, m := range request.Messages {
			if m != nil && m.Role == schema.System {
				systems = append(systems, m.Content)
				if m.Content == constraint {
					independent++
				} else if baseInstruction == "" {
					baseInstruction = m.Content
					baseSystem.Store(baseInstruction)
				}
				if m.Content == baseInstruction {
					own++
				}
			}
		}
		if step == 1 {
			firstSystems.Store(append([]string(nil), systems...))
		} else if !reflect.DeepEqual(systems, firstSystems.Load().([]string)) {
			t.Errorf("actual engine recovery changed system ordering: first=%q next=%q", firstSystems.Load(), systems)
		}
		if independent != 1 || own != 1 {
			t.Errorf("actual HTTP request%d did not preserve one constructor instruction and independent system: own=%d independent=%d", step, own, independent)
		}
		switch step {
		case 1:
			args, _ := json.Marshal(map[string]any{"command": "printf 'recorded\\n'"})
			writeManagedExecHTTPCall(w, request, callID, string(args))
		case 2:
			writeOutputLimitFrames(w, map[string]any{"reasoning_content": "An interrupted intermediate check."})
		case 3:
			calls, results := 0, 0
			for _, m := range request.Messages {
				if m == nil {
					continue
				}
				for _, c := range m.ToolCalls {
					if c.ID == callID {
						calls++
					}
				}
				if m.Role == schema.Tool && m.ToolCallID == callID && strings.Contains(m.Content, "recorded") {
					results++
				}
			}
			if calls != 1 || results != 1 {
				t.Errorf("independent-system recovery lost original tool evidence: calls=%d results=%d", calls, results)
			}
			writeContextFlowReply(w, request.Stream, map[string]any{"content": answer}, "stop")
		default:
			t.Errorf("unexpected continuation request%d", step)
			writeContextFlowReply(w, request.Stream, map[string]any{"content": answer}, "stop")
		}
	})
	e.cfg.Memory.Enabled, e.cfg.Memory.AutoReview = false, false
	e.cfg.Swarm.RecoverReasoningOutputLimit = true
	e.cfg.Swarm.ManagerMaxIterations = 100
	e.cfg.Swarm.MaxCompletionTokens = 16384
	thread, err := e.CreateThread("Independent system recovery fixture", "", "")
	if err != nil {
		t.Fatal(err)
	}
	turn := &store.Turn{ThreadID: thread.ID, UserText: "Inspect the existing receipt under its established constraints.", ProviderID: thread.ProviderID, Model: thread.Model, ReasoningEffort: config.ReasoningHigh}
	if err := e.store.CreateTurn(turn); err != nil {
		t.Fatal(err)
	}
	set := buildTestToolset(t, e, thread.ID)
	ctx := context.Background()
	builder, err := e.pool.ModelBuilder(ctx, turn.ProviderID, turn.Model, config.ReasoningHigh, e.callRecorder(thread.ID, turn.ID))
	if err != nil {
		t.Fatal(err)
	}
	reg := e.newTurnRegistry(builder, set, nil, turn)
	defer reg.Close()
	acc := newAccumulator(e, thread.ID, turn.ID, 0)
	res, err := e.runtimeFor(thread.ID).runOwnedTestManager(ctx, turn, reg, acc, set, nil, []adk.Message{schema.UserMessage(turn.UserText), schema.SystemMessage(constraint)})
	if err != nil || res.Final != answer || len(wire.snapshot()) != 3 {
		t.Fatalf("actual engine continuation failed: requests=%d result=%q error=%v", len(wire.snapshot()), res.Final, err)
	}
	for i, request := range wire.snapshot() {
		if request.ReasoningEffort == nil || *request.ReasoningEffort != config.ReasoningHigh || request.MaxTokens != 16384 {
			t.Fatalf("request%d changed reasoning or cap", i+1)
		}
	}
}

// Consuming an allowance is durable across a real engine shutdown, closed SQLite
// handle, reopened Store and resumed original turn; a new runtime cannot grant it again.
func TestManagerMidturnRecoveryClaimSurvivesColdEngineAndStoreReopenOverHTTP(t *testing.T) {
	recoveryEntered, recoveryCancelled := make(chan struct{}), make(chan struct{})
	e, wire := newScheduleCompletionHTTPEngine(t, func(w http.ResponseWriter, r *http.Request, step int, request scheduleCompletionRequest) {
		switch step {
		case 1:
			args, _ := json.Marshal(map[string]any{"command": "printf 'once\\n' >> persisted-effects; printf 'durable receipt\\n'"})
			writeManagedExecHTTPCall(w, request, "persistent-original-effect", string(args))
		case 2, 4:
			writeOutputLimitFrames(w, map[string]any{"reasoning_content": "An unfinished manager check."})
		case 3:
			w.Header().Set("Content-Type", "text/event-stream")
			w.(http.Flusher).Flush()
			close(recoveryEntered)
			<-r.Context().Done()
			close(recoveryCancelled)
		default:
			t.Errorf("a fresh runtime refreshed the spent output-limit allowance: HTTP request%d", step)
			writeContextFlowReply(w, request.Stream, map[string]any{"content": "An invalid second recovery reached the model."}, "stop")
		}
	})
	e.cfg.Memory.Enabled, e.cfg.Memory.AutoReview = false, false
	e.cfg.Swarm.RecoverReasoningOutputLimit = true
	e.cfg.Swarm.ManagerMaxIterations = 100
	e.cfg.Swarm.MaxCompletionTokens = 16384
	thread, err := e.CreateThread("Cold output recovery allowance fixture", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := e.SetThreadReasoning(thread.ID, config.ReasoningHigh); err != nil {
		t.Fatal(err)
	}
	turn, err := e.StartTurn(thread.ID, "Record one effect and continue the current verification.")
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-recoveryEntered:
	case <-time.After(5 * time.Second):
		t.Fatal("first actual recovered HTTP request did not begin")
	}
	events, err := e.store.ListTurnEvents(turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	var oldClaim store.Event
	for _, event := range events {
		if event.Kind == KindModelRetry && strings.Contains(event.Text, `"reason":"output_limit"`) {
			if oldClaim.ID != 0 {
				t.Fatal("initial recovery already spent multiple claims")
			}
			oldClaim = event
		}
	}
	if oldClaim.ID == 0 {
		t.Fatal("recovery reached HTTP without its durable allowance")
	}
	e.Shutdown()
	select {
	case <-recoveryCancelled:
	case <-time.After(5 * time.Second):
		t.Fatal("old actual HTTP request survived engine shutdown")
	}

	original, err := e.store.GetTurn(turn.ID)
	if err != nil || original.Status != store.TurnRunning {
		t.Fatalf("shutdown lost the unfinished original turn: %+v %v", original, err)
	}
	if err := e.store.Close(); err != nil {
		t.Fatal(err)
	}
	next := openRestartReplayEngine(t, e.cfg)
	if next.store == e.store || next.runtimeFor(thread.ID) == e.runtimeFor(thread.ID) {
		t.Fatal("cold boundary reused the old Store or runtime")
	}
	before, err := next.store.ListTurnEvents(turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	foundOriginal := false
	for _, event := range before {
		if event.Kind == KindModelRetry && event.ID == oldClaim.ID && event.MessageID == oldClaim.MessageID && event.Seq == oldClaim.Seq {
			foundOriginal = true
		}
	}
	if !foundOriginal {
		t.Fatal("the spent claim did not survive SQLite reopen")
	}
	if n, err := next.ResumeOrphanedTurns(); err != nil || n != 1 {
		t.Fatalf("original turn did not resume through actual restoration: n=%d err=%v", n, err)
	}
	got := waitForTurn(t, next, turn.ID)
	waitSettled(t, next, thread.ID)
	if got.Status != store.TurnError || !strings.Contains(got.Error, "output budget") || len(wire.snapshot()) != 4 {
		t.Fatalf("fresh runtime repeated a spent recovery: requests=%d turn=%+v", len(wire.snapshot()), got)
	}
	finalEvents, err := next.store.ListTurnEvents(turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	claims := 0
	for _, event := range finalEvents {
		if event.Kind == KindModelRetry && strings.Contains(event.Text, `"reason":"output_limit"`) {
			claims++
		}
	}
	if claims != 1 {
		t.Fatalf("durable recovery allowance refreshed across restart: %d", claims)
	}
	effect, err := os.ReadFile(filepath.Join(next.WorkspaceDir(thread.ID), "persisted-effects"))
	if err != nil || string(effect) != "once\n" {
		t.Fatalf("cold restoration repeated an already committed effect: %q %v", effect, err)
	}
	for i, request := range wire.snapshot() {
		if request.ReasoningEffort == nil || *request.ReasoningEffort != config.ReasoningHigh || request.MaxTokens != 16384 {
			t.Fatalf("cold request%d changed high reasoning or original cap", i+1)
		}
	}
}

// Manager generation recovery cannot replace or cancel an already executing
// delegated HTTP worker. The original generation must be collected exactly once.
func TestManagerMidturnRecoveryRetainsRealWorkerGenerationOverHTTP(t *testing.T) {
	const workerProof = "The original worker verified its assigned evidence."
	const answer = "The original worker result was collected once."
	var managers, workers atomic.Int32
	var workerID atomic.Value
	workerID.Store("")
	var generation atomic.Int64
	entered, released, cancelled := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var enterOnce, releaseOnce, cancelOnce sync.Once
	var e *Engine
	var threadID string
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request scheduleCompletionRequest
		if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
			t.Error(err)
			http.Error(w, "bad fixture input", 400)
			return
		}
		if len(request.Messages) > 0 && replyNoScheduleClaimsFixture(t, w, request.Messages[0].Content, request.Stream, len(request.Tools)) {
			return
		}
		manager := false
		for _, tool := range request.Tools {
			manager = manager || tool.Function.Name == "spawn_agent"
		}
		if !manager {
			if workers.Add(1) != 1 {
				t.Error("manager cap restarted the actual worker HTTP request")
			}
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = fmt.Fprint(w, "data: {\"id\":\"midturn-worker\",\"choices\":[{\"index\":0,\"delta\":{\"role\":\"assistant\",\"reasoning_content\":\"Checking assigned evidence.\"},\"finish_reason\":null}]}\n\n")
			w.(http.Flusher).Flush()
			enterOnce.Do(func() { close(entered) })
			select {
			case <-released:
				writeContextFlowReply(w, true, map[string]any{"content": workerProof}, "stop")
			case <-r.Context().Done():
				cancelOnce.Do(func() { close(cancelled) })
			}
			return
		}
		call := func(id, name string, args any) {
			body, _ := json.Marshal(args)
			writeContextFlowReply(w, true, map[string]any{"tool_calls": []any{map[string]any{"index": 0, "id": id, "type": "function", "function": map[string]any{"name": name, "arguments": string(body)}}}}, "tool_calls")
		}
		switch step := managers.Add(1); step {
		case 1:
			call("original-midturn-spawn", "spawn_agent", map[string]any{"role": "evidence-reader", "task": "Inspect the assigned evidence and return its verified result."})
		case 2:
			for _, message := range request.Messages {
				if message.Role == schema.Tool && message.ToolCallID == "original-midturn-spawn" {
					var receipt struct {
						AgentID string `json:"agent_id"`
					}
					if err := json.Unmarshal([]byte(message.Content), &receipt); err != nil || receipt.AgentID == "" {
						t.Errorf("actual spawn receipt lost worker ID: %v", err)
					}
					workerID.Store(receipt.AgentID)
				}
			}
			select {
			case <-entered:
			case <-r.Context().Done():
				return
			}
			rt := e.runtimeFor(threadID)
			rt.mu.Lock()
			reg := rt.reg
			rt.mu.Unlock()
			for _, worker := range reg.SnapshotWorkers() {
				if worker.ID == workerID.Load().(string) {
					generation.Store(int64(worker.Generation))
				}
			}
			if generation.Load() < 1 {
				t.Error("actual running worker generation was not observed before cap")
			}
			writeOutputLimitFrames(w, map[string]any{"reasoning_content": "An interrupted manager-only plan."})
		case 3:
			select {
			case <-cancelled:
				t.Error("manager recovery cancelled its pending worker HTTP stream")
			default:
			}
			id := workerID.Load().(string)
			rt := e.runtimeFor(threadID)
			rt.mu.Lock()
			reg := rt.reg
			rt.mu.Unlock()
			same := false
			for _, worker := range reg.SnapshotWorkers() {
				same = same || worker.ID == id && int64(worker.Generation) == generation.Load() && !worker.Finished
			}
			if !same {
				t.Error("recovered manager did not retain its original live worker generation")
			}
			call("original-midturn-wait", "wait_agents", map[string]any{"agent_ids": []string{id}, "timeout_s": 30})
		case 4:
			found := false
			for _, message := range request.Messages {
				if message.Role == schema.Tool && message.ToolCallID == "original-midturn-wait" {
					var receipt struct {
						Agents []struct {
							AgentID string `json:"agent_id"`
							Status  string `json:"status"`
							Result  string `json:"result"`
						} `json:"agents"`
					}
					if err := json.Unmarshal([]byte(message.Content), &receipt); err != nil {
						t.Error(err)
					}
					found = len(receipt.Agents) == 1 && receipt.Agents[0].AgentID == workerID.Load().(string) && receipt.Agents[0].Status == "done" && receipt.Agents[0].Result == workerProof
				}
			}
			if !found {
				t.Error("recovered manager did not receive the original worker result once")
			}
			writeContextFlowReply(w, true, map[string]any{"content": answer}, "stop")
		default:
			t.Errorf("unexpected extra manager HTTP request%d", step)
			writeContextFlowReply(w, true, map[string]any{"content": answer}, "stop")
		}
	}))
	t.Cleanup(endpoint.Close)
	e = newTestEngine(t)
	e.cfg.Swarm.AutoTitle = false
	e.cfg.Memory.Enabled, e.cfg.Memory.AutoReview = false, false
	e.cfg.Swarm.RecoverReasoningOutputLimit = true
	e.cfg.Swarm.ManagerMaxIterations = 100
	e.cfg.Swarm.MaxCompletionTokens = 16384
	e.cfg.Models.Providers[0].BaseURL = endpoint.URL
	e.cfg.Models.Providers[0].APIKey = ""
	e.cfg.Models.Providers[0].Model = "offline-midturn-worker"
	e.pool = provider.New(e.cfg)
	thread, err := e.CreateThread("Midturn worker ownership fixture", "", "")
	if err != nil {
		t.Fatal(err)
	}
	threadID = thread.ID
	if err := e.SetThreadReasoning(thread.ID, config.ReasoningHigh); err != nil {
		t.Fatal(err)
	}
	turn, err := e.StartTurn(thread.ID, "Delegate one evidence check and collect the original result.")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = e.Interrupt(thread.ID); releaseOnce.Do(func() { close(released) }) })
	ordinaryEventually(t, func() bool {
		events, err := e.store.ListTurnEvents(turn.ID)
		if err != nil {
			return false
		}
		for _, event := range events {
			if event.Kind == swarm.NotifyToolCall.String() && event.ToolCallID == "original-midturn-wait" {
				return true
			}
		}
		return false
	}, "recovered manager never invoked a real wait on its original worker")
	select {
	case <-cancelled:
		t.Fatal("manager cap was treated as worker cancellation")
	default:
	}
	releaseOnce.Do(func() { close(released) })
	got := waitForTurn(t, e, turn.ID)
	waitSettled(t, e, thread.ID)
	if got.Status != store.TurnDone || got.Final != answer || managers.Load() != 4 || workers.Load() != 1 {
		t.Fatalf("original worker was lost or repeated: managers=%d workers=%d turn=%+v", managers.Load(), workers.Load(), got)
	}
	events, err := e.store.ListTurnEvents(turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	spawned, finished, collected := 0, 0, 0
	var workerMessageID string
	for _, event := range events {
		if event.AgentID == workerID.Load().(string) && event.Kind == swarm.NotifyGenerationStarted.String() {
			if workerMessageID != "" {
				t.Fatal("manager recovery started a replacement worker generation")
			}
			workerMessageID = event.MessageID
		}
	}
	if workerMessageID == "" {
		t.Fatal("original worker stream has no durable message identity")
	}
	for _, event := range events {
		if event.AgentID == workerID.Load().(string) {
			if (event.Kind == swarm.NotifySpawned.String() || event.Kind == swarm.NotifyFinished.String() || event.WorkerGeneration != 0) && event.WorkerGeneration != int(generation.Load()) {
				t.Fatalf("explicit worker generation changed: %+v", event)
			}
			switch event.Kind {
			case swarm.NotifyGenerationStarted.String(), swarm.NotifyGenerationCompleted.String(), swarm.NotifyGenerationInterrupted.String(), KindReasoning, swarm.NotifyReasoningDelta.String(), swarm.NotifyDelta.String(), swarm.NotifyAgentMessage.String():
				if event.MessageID == "" || event.MessageID != workerMessageID {
					t.Fatalf("manager recovery replaced or lost the original worker output identity: %+v", event)
				}
			}
			if event.Kind == swarm.NotifySpawned.String() {
				spawned++
			}
			if event.Kind == swarm.NotifyFinished.String() {
				finished++
				if event.Err != "" {
					t.Fatalf("worker did not finish successfully: %+v", event)
				}
			}
		}
		if event.Kind == swarm.NotifyToolResult.String() && event.ToolCallID == "original-midturn-wait" {
			collected++
		}
	}
	t.Log("Worker generation is verified on actual registry snapshots and explicit spawn/finish receipts; legacy generation_started may omit WorkerGeneration and remains unknown.")
	if spawned != 1 || finished != 1 || collected != 1 {
		t.Fatalf("worker ownership/collection repeated: spawned=%d finished=%d collected=%d", spawned, finished, collected)
	}
}

// The optional first-request lowering applies only at the original turn's start.
// A cold resumed turn is still midturn even when its new runtime's local used is one.
func TestManagerMidturnResumedRecoveryDoesNotApplyOptionalFirstRequestEffortOverHTTP(t *testing.T) {
	pending, cancelled := make(chan struct{}), make(chan struct{})
	const answer = "The resumed request preserved its original model settings."
	e, wire := newScheduleCompletionHTTPEngine(t, func(w http.ResponseWriter, r *http.Request, step int, request scheduleCompletionRequest) {
		if request.ReasoningEffort != nil {
			t.Errorf("actual HTTP request%d changed originally unspecified reasoning effort to %q", step, *request.ReasoningEffort)
		}
		switch step {
		case 1:
			args, _ := json.Marshal(map[string]any{"command": "printf 'once\\n' >> optional-effort-effects; printf 'original receipt\\n'"})
			writeManagedExecHTTPCall(w, request, "optional-effort-original-effect", string(args))
		case 2:
			w.Header().Set("Content-Type", "text/event-stream")
			w.(http.Flusher).Flush()
			close(pending)
			<-r.Context().Done()
			close(cancelled)
		case 3:
			writeOutputLimitFrames(w, map[string]any{"reasoning_content": "An unfinished resumed intermediate check."})
		case 4:
			writeContextFlowReply(w, request.Stream, map[string]any{"content": answer}, "stop")
		default:
			t.Errorf("unexpected optional-effort request%d", step)
			writeContextFlowReply(w, request.Stream, map[string]any{"content": answer}, "stop")
		}
	})
	e.cfg.Memory.Enabled, e.cfg.Memory.AutoReview = false, false
	e.cfg.Swarm.RecoverReasoningOutputLimit = true
	e.cfg.Swarm.ReasoningOutputRecoveryEffort = config.ReasoningLow
	e.cfg.Swarm.ManagerMaxIterations = 100
	e.cfg.Swarm.MaxCompletionTokens = 16384
	thread, err := e.CreateThread("Resumed optional effort fixture", "", "")
	if err != nil {
		t.Fatal(err)
	}
	turn, err := e.StartTurn(thread.ID, "Verify the original effect while preserving current model settings.")
	if err != nil {
		t.Fatal(err)
	}
	if turn.ReasoningEffort != "" {
		t.Fatal("fixture must exercise omitted turn reasoning effort")
	}
	select {
	case <-pending:
	case <-time.After(5 * time.Second):
		t.Fatal("initial non-capped request did not reach real HTTP")
	}
	e.Shutdown()
	select {
	case <-cancelled:
	case <-time.After(5 * time.Second):
		t.Fatal("old non-capped HTTP request survived shutdown")
	}
	old, err := e.store.GetTurn(turn.ID)
	if err != nil || old.Status != store.TurnRunning {
		t.Fatalf("original unfinished turn was lost: %+v %v", old, err)
	}
	events, err := e.store.ListTurnEvents(turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range events {
		if event.Kind == KindModelRetry && strings.Contains(event.Text, `"reason":"output_limit"`) {
			t.Fatal("fixture unexpectedly consumed a recovery before restart")
		}
	}
	if err := e.store.Close(); err != nil {
		t.Fatal(err)
	}
	next := openRestartReplayEngine(t, e.cfg)
	if next.store == e.store || next.runtimeFor(thread.ID) == e.runtimeFor(thread.ID) {
		t.Fatal("fixture did not cross real Store and runtime identities")
	}
	if n, err := next.ResumeOrphanedTurns(); err != nil || n != 1 {
		t.Fatalf("original unspent turn did not resume: n=%d err=%v", n, err)
	}
	got := waitForTurn(t, next, turn.ID)
	waitSettled(t, next, thread.ID)
	if got.Status != store.TurnDone || got.Final != answer || len(wire.snapshot()) != 4 {
		t.Fatalf("resumed unused recovery failed: requests=%d turn=%+v", len(wire.snapshot()), got)
	}
	events, err = next.store.ListTurnEvents(turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	claims := 0
	for _, event := range events {
		if event.Kind == KindModelRetry && strings.Contains(event.Text, `"reason":"output_limit"`) {
			claims++
		}
	}
	if claims != 1 {
		t.Fatalf("resumed turn did not consume exactly one original allowance: %d", claims)
	}
	effect, err := os.ReadFile(filepath.Join(next.WorkspaceDir(thread.ID), "optional-effort-effects"))
	if err != nil || string(effect) != "once\n" {
		t.Fatalf("resumed recovery repeated original effect: %q %v", effect, err)
	}
	for i, request := range wire.snapshot() {
		if request.MaxTokens != 16384 || request.Model != wire.snapshot()[0].Model {
			t.Fatalf("resumed request%d changed output cap or model", i+1)
		}
	}
}

// Typed metadata can be serializable for tracing but unavailable to a lossless
// snapshot. The actual manager must keep the cap terminal rather than discard that state.
func TestManagerMidturnRecoveryRefusesUncopyableMetadataOverHTTP(t *testing.T) {
	e, wire := newScheduleCompletionHTTPEngine(t, func(w http.ResponseWriter, _ *http.Request, step int, request scheduleCompletionRequest) {
		switch step {
		case 1:
			args, _ := json.Marshal(map[string]any{"command": "printf 'once\\n' >> opaque-effects; printf 'metadata receipt\\n'"})
			writeManagedExecHTTPCall(w, request, "opaque-metadata-effect", string(args))
		case 2:
			writeOutputLimitFrames(w, map[string]any{"reasoning_content": "An unfinished intermediate generation."})
		default:
			t.Error("an unavailable independent snapshot authorized another HTTP request")
			writeContextFlowReply(w, request.Stream, map[string]any{"content": "This recovery must be refused."}, "stop")
		}
	})
	e.cfg.Memory.Enabled, e.cfg.Memory.AutoReview = false, false
	e.cfg.Swarm.RecoverReasoningOutputLimit = true
	e.cfg.Swarm.ManagerMaxIterations = 100
	e.cfg.Swarm.MaxCompletionTokens = 16384
	thread, err := e.CreateThread("Uncopyable recovery metadata fixture", "", "")
	if err != nil {
		t.Fatal(err)
	}
	turn := &store.Turn{ThreadID: thread.ID, UserText: "Inspect the existing receipt and preserve its metadata.", ProviderID: thread.ProviderID, Model: thread.Model, ReasoningEffort: config.ReasoningHigh}
	if err := e.store.CreateTurn(turn); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	set := buildTestToolset(t, e, thread.ID)
	builder, err := e.pool.ModelBuilder(ctx, turn.ProviderID, turn.Model, config.ReasoningHigh, e.callRecorder(thread.ID, turn.ID))
	if err != nil {
		t.Fatal(err)
	}
	reg := e.newTurnRegistry(builder, set, nil, turn)
	defer reg.Close()
	metadata := schema.SystemMessage("Retain the typed runtime receipt context.")
	metadata.Extra = map[string]any{"runtime_receipt_time": time.Unix(1700000000, 0).UTC()}
	acc := newAccumulator(e, thread.ID, turn.ID, 0)
	result, err := e.runtimeFor(thread.ID).runOwnedTestManager(ctx, turn, reg, acc, set, nil, []adk.Message{schema.UserMessage(turn.UserText), metadata})
	var cap *swarm.ReasoningOutputLimit
	if !errors.As(err, &cap) || cap.RecoveryUnavailable == "" || len(cap.RecoveryInput) != 0 || result.Final != "" || len(wire.snapshot()) != 2 {
		t.Fatalf("opaque input was lost or authorized recovery: requests=%d final=%q error=%v", len(wire.snapshot()), result.Final, err)
	}
	events, eventErr := e.store.ListTurnEvents(turn.ID)
	if eventErr != nil {
		t.Fatal(eventErr)
	}
	for _, event := range events {
		if event.Kind == KindModelRetry && strings.Contains(event.Text, `"reason":"output_limit"`) {
			t.Fatal("unavailable snapshot persisted a false recovery allowance")
		}
	}
	effect, readErr := os.ReadFile(filepath.Join(e.WorkspaceDir(thread.ID), "opaque-effects"))
	if readErr != nil || string(effect) != "once\n" {
		t.Fatalf("snapshot refusal lost or repeated a committed effect: %q %v", effect, readErr)
	}
}
