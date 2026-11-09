package engine

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/config"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/cloudwego/eino/schema"
	"gorm.io/gorm"
)

// A previous non-idempotent tool effect belongs to the same turn. Recovering a
// later reasoning-only cap must preserve its call/result without executing it again.
func TestManagerMidturnOutputCapPreservesOneCommittedEffectOverHTTP(t *testing.T) {
	const callID = "midturn-effect-call"
	const reasoning = "Unfinished private planning must not be replayed."
	const answer = "The existing workspace effect is verified."
	e, wire := newScheduleCompletionHTTPEngine(t, func(w http.ResponseWriter, _ *http.Request, step int, request scheduleCompletionRequest) {
		switch step {
		case 1:
			args, _ := json.Marshal(map[string]any{"command": "printf 'effect-once\\n' >> committed-effects; printf 'committed receipt\\n'"})
			writeContextFlowReply(w, request.Stream, map[string]any{"tool_calls": []any{map[string]any{
				"index": 0, "id": callID, "type": "function", "function": map[string]any{"name": "exec", "arguments": string(args)},
			}}}, "tool_calls")
		case 2:
			writeOutputLimitFrames(w, map[string]any{"reasoning_content": reasoning})
		case 3:
			var calls, results int
			for _, message := range request.Messages {
				if message == nil {
					continue
				}
				for _, call := range message.ToolCalls {
					if call.ID == callID {
						calls++
					}
				}
				if message.Role == schema.Tool && message.ToolCallID == callID && strings.Contains(message.Content, "committed receipt") {
					results++
				}
				if strings.Contains(message.Content, reasoning) || strings.Contains(message.ReasoningContent, reasoning) {
					t.Error("interrupted reasoning crossed the actual recovery HTTP boundary")
				}
			}
			if calls != 1 || results != 1 {
				t.Errorf("recovery HTTP input lost or duplicated original tool identity/result: calls=%d results=%d", calls, results)
			}
			writeContextFlowReply(w, request.Stream, map[string]any{"content": answer}, "stop")
		default:
			t.Errorf("unexpected extra recovery request %d", step)
			writeContextFlowReply(w, request.Stream, map[string]any{"content": answer}, "stop")
		}
	})
	e.cfg.Memory.Enabled, e.cfg.Memory.AutoReview = false, false
	e.cfg.Swarm.RecoverReasoningOutputLimit = true
	e.cfg.Swarm.ReasoningOutputRecoveryEffort = config.ReasoningLow
	e.cfg.Swarm.ManagerMaxIterations = 100
	e.cfg.Swarm.MaxCompletionTokens = 16384
	thread, err := e.CreateThread("Midturn output recovery fixture", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := e.SetThreadReasoning(thread.ID, config.ReasoningHigh); err != nil {
		t.Fatal(err)
	}
	turn, err := e.StartTurn(thread.ID, "Commit one workspace effect and then verify its receipt.")
	if err != nil {
		t.Fatal(err)
	}
	got := waitForTurn(t, e, turn.ID)
	waitSettled(t, e, thread.ID)
	requests := wire.snapshot()
	effect, readErr := os.ReadFile(filepath.Join(e.WorkspaceDir(thread.ID), "committed-effects"))
	if readErr != nil || string(effect) != "effect-once\n" {
		t.Fatalf("non-idempotent workspace effect was lost or repeated: %q, %v", effect, readErr)
	}
	if len(requests) != 3 || got.Status != store.TurnDone || got.Final != answer {
		t.Fatalf("later cap did not recover within the same turn after committed work: requests=%d turn=%+v", len(requests), got)
	}
	for i, request := range requests {
		if request.ReasoningEffort == nil || *request.ReasoningEffort != config.ReasoningHigh || request.MaxTokens != 16384 || request.Model != requests[0].Model {
			t.Fatalf("request %d changed high reasoning, model, or response cap", i+1)
		}
	}
	turns, err := e.store.ListTurns(thread.ID)
	if err != nil || len(turns) != 1 || turns[0].ID != turn.ID {
		t.Fatalf("recovery replaced the original turn: %+v, %v", turns, err)
	}
	assertOutputLimitRecoveryEvents(t, e, turn.ID, 1, true, false)
}

// A midturn recovery consumes the same one-shot allowance and external request
// budget. Stop, incomplete tool arguments and failed durable claims cannot replay effects.
func TestManagerMidturnRecoverySafetyBoundariesOverHTTP(t *testing.T) {
	for _, tc := range []struct {
		name                                                                          string
		disabled, secondCap, partial, storageFail, exhausted, durableStop, actualStop bool
		wantCalls, wantClaim, wantCaps                                                int
	}{
		{name: "default off", disabled: true, wantCalls: 2, wantCaps: 1},
		{name: "second cap is terminal", secondCap: true, wantCalls: 3, wantClaim: 1, wantCaps: 2},
		{name: "partial new tool cannot retry", partial: true, wantCalls: 2, wantCaps: 1},
		{name: "claim write failure", storageFail: true, wantCalls: 2, wantCaps: 1},
		{name: "shared hundred request budget exhausted", exhausted: true, wantCalls: 2, wantCaps: 1},
		{name: "durable Stop before cancellation", durableStop: true, wantCalls: 2, wantCaps: 1},
		{name: "actual Stop during recovery request", actualStop: true, wantCalls: 3, wantClaim: 1, wantCaps: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			const callID = "committed-effect-before-cap"
			capEntered, capRelease := make(chan struct{}), make(chan struct{})
			recoveryEntered, recoveryCancelled := make(chan struct{}), make(chan struct{})
			var releaseOnce sync.Once
			release := func() { releaseOnce.Do(func() { close(capRelease) }) }
			defer release()
			e, wire := newScheduleCompletionHTTPEngine(t, func(w http.ResponseWriter, r *http.Request, step int, request scheduleCompletionRequest) {
				if step == 1 {
					args, _ := json.Marshal(map[string]any{"command": "printf 'once\\n' >> safety-effects; printf 'side effect recorded\\n'"})
					writeManagedExecHTTPCall(w, request, callID, string(args))
					return
				}
				if step == 2 || step == 3 && tc.secondCap {
					if step == 2 && tc.durableStop {
						close(capEntered)
						select {
						case <-capRelease:
						case <-r.Context().Done():
							return
						}
					}
					if tc.exhausted {
						w.Header().Set("X-Zwai-Model-Request-Limit", "100")
						w.Header().Set("X-Zwai-Model-Requests-Remaining", "0")
					}
					delta := map[string]any{"reasoning_content": "An unfinished internal check."}
					if tc.partial {
						delta["tool_calls"] = []any{map[string]any{"index": 0, "id": "incomplete-effect-call", "type": "function", "function": map[string]any{"name": "exec", "arguments": `{"command":"`}}}
					}
					writeOutputLimitFrames(w, delta)
					return
				}
				if tc.actualStop && step == 3 {
					w.Header().Set("Content-Type", "text/event-stream")
					w.(http.Flusher).Flush()
					close(recoveryEntered)
					<-r.Context().Done()
					close(recoveryCancelled)
					return
				}
				writeContextFlowReply(w, request.Stream, map[string]any{"content": "The recovery must not reach this extra reply."}, "stop")
			})
			e.cfg.Memory.Enabled, e.cfg.Memory.AutoReview = false, false
			e.cfg.Swarm.RecoverReasoningOutputLimit = !tc.disabled
			e.cfg.Swarm.ManagerMaxIterations = 100
			e.cfg.Swarm.MaxCompletionTokens = 16384
			var faults atomic.Int32
			if tc.storageFail {
				if err := e.store.DB().Callback().Create().Before("gorm:create").Register("midturn_claim_write_failure", func(tx *gorm.DB) {
					if event, ok := tx.Statement.Dest.(*store.Event); ok && event.Kind == KindModelRetry {
						faults.Add(1)
						_ = tx.AddError(errors.New("private fixture refused a recovery claim"))
					}
				}); err != nil {
					t.Fatal(err)
				}
			}
			thread, err := e.CreateThread("Midturn safety fixture", "", "")
			if err != nil {
				t.Fatal(err)
			}
			if err := e.SetThreadReasoning(thread.ID, config.ReasoningHigh); err != nil {
				t.Fatal(err)
			}
			turn, err := e.StartTurn(thread.ID, "Record one workspace effect and inspect its receipt.")
			if err != nil {
				t.Fatal(err)
			}
			if tc.durableStop {
				select {
				case <-capEntered:
				case <-time.After(5 * time.Second):
					t.Fatal("later generation never reached cap fixture")
				}
				target := &restartTarget{rt: e.runtimeFor(thread.ID), turnID: turn.ID}
				if err := e.writeRestartEvent(target, KindUserStopRequested, "Stop requested by the user."); err != nil {
					t.Fatal(err)
				}
				release()
			}
			if tc.actualStop {
				select {
				case <-recoveryEntered:
				case <-time.After(5 * time.Second):
					t.Fatal("the recovery HTTP request did not start")
				}
				if err := e.Interrupt(thread.ID); err != nil {
					t.Fatal(err)
				}
				select {
				case <-recoveryCancelled:
				case <-time.After(5 * time.Second):
					t.Fatal("Stop left the actual recovery request alive")
				}
			}
			got := waitForTurn(t, e, turn.ID)
			waitSettled(t, e, thread.ID)
			requests := wire.snapshot()
			if len(requests) != tc.wantCalls {
				t.Fatalf("safety boundary sent requests=%d, want=%d, turn=%+v", len(requests), tc.wantCalls, got)
			}
			for i, request := range requests {
				if request.ReasoningEffort == nil || *request.ReasoningEffort != config.ReasoningHigh || request.MaxTokens != 16384 {
					t.Fatalf("request%d changed explicit high reasoning or output cap", i+1)
				}
			}
			expected := store.TurnError
			if tc.actualStop {
				expected = store.TurnCancelled
			}
			if got.Status != expected || got.Final != "" {
				t.Fatalf("safety boundary lost terminal cause: %+v", got)
			}
			effect, err := os.ReadFile(filepath.Join(e.WorkspaceDir(thread.ID), "safety-effects"))
			if err != nil || string(effect) != "once\n" {
				t.Fatalf("effect repeated or disappeared: %q, %v", effect, err)
			}
			events, err := e.store.ListTurnEvents(turn.ID)
			if err != nil {
				t.Fatal(err)
			}
			claims, caps := 0, 0
			for _, event := range events {
				if event.Kind == KindModelRetry && strings.Contains(event.Text, `"reason":"output_limit"`) {
					claims++
				}
				if event.Kind == swarm.NotifyGenerationInterrupted.String() {
					var cause swarm.GenerationInterruption
					if json.Unmarshal([]byte(event.Text), &cause) == nil && cause.Reason == "output_limit" {
						caps++
					}
				}
			}
			if claims != tc.wantClaim || caps != tc.wantCaps {
				t.Fatalf("allowance/cap evidence differed: claims=%d want=%d caps=%d want=%d", claims, tc.wantClaim, caps, tc.wantCaps)
			}
			if tc.storageFail && faults.Load() != 1 {
				t.Fatalf("claim failure was not exercised once: %d", faults.Load())
			}
		})
	}
}

// A background command belongs to the turn across model recovery. The original
// ID and live shell PID survive, and only its one eventual terminal result is delivered.
func TestManagerMidturnRecoveryRetainsBackgroundExecIdentityOverHTTP(t *testing.T) {
	if goruntime.GOOS != "darwin" && goruntime.GOOS != "linux" {
		t.Skip("managed exec process ownership requires POSIX")
	}
	const callID = "midturn-owned-command"
	const thought = "A truncated internal plan that must not return to model input."
	const answer = "The original command completed once."
	capEntered, capRelease := make(chan struct{}), make(chan struct{})
	var releaseOnce sync.Once
	releaseCap := func() { releaseOnce.Do(func() { close(capRelease) }) }
	defer releaseCap()
	var session atomic.Value
	session.Store("")
	var firstPID atomic.Int64
	var workspace string
	e, wire := newScheduleCompletionHTTPEngine(t, func(w http.ResponseWriter, r *http.Request, step int, request scheduleCompletionRequest) {
		switch step {
		case 1:
			command := `printf '%s\n' "$$" > process-id; printf 'once\n' >> process-starts; printf 'ready\n'; while [ ! -f release-process ]; do sleep 0.02; done; printf 'settled\n'`
			args, _ := json.Marshal(map[string]any{"command": command, "yield_after_ms": 1})
			writeManagedExecHTTPCall(w, request, callID, string(args))
		case 2:
			running := assertManagedExecHTTPStatus(t, request, callID, "running")
			session.Store(running.SessionID)
			close(capEntered)
			select {
			case <-capRelease:
			case <-r.Context().Done():
				return
			}
			writeOutputLimitFrames(w, map[string]any{"reasoning_content": thought})
		case 3:
			running := assertManagedExecHTTPStatus(t, request, callID, "running")
			if running.SessionID != session.Load().(string) {
				t.Error("recovery replaced the original running session ID")
			}
			bytes, err := os.ReadFile(filepath.Join(workspace, "process-id"))
			pid, _ := strconv.Atoi(strings.TrimSpace(string(bytes)))
			if err != nil || int64(pid) != firstPID.Load() {
				t.Errorf("recovery changed the original process PID: %q, %v", bytes, err)
			}
			process, err := os.FindProcess(pid)
			if err != nil || process.Signal(syscall.Signal(0)) != nil {
				t.Error("original owned process was not live after recovery")
			}
			for _, message := range request.Messages {
				if message != nil && (strings.Contains(message.Content, thought) || strings.Contains(message.ReasoningContent, thought)) {
					t.Error("cap reasoning leaked into the recovery request")
				}
			}
			if err := os.WriteFile(filepath.Join(workspace, "release-process"), nil, 0600); err != nil {
				t.Error(err)
			}
			args, _ := json.Marshal(map[string]any{"session_id": running.SessionID, "idle_timeout_ms": 4000})
			writeContextFlowReply(w, request.Stream, map[string]any{"tool_calls": []any{map[string]any{"index": 0, "id": "midturn-wait-original", "type": "function", "function": map[string]any{"name": "wait_exec", "arguments": string(args)}}}}, "tool_calls")
		case 4:
			terminal := assertManagedExecHTTPStatus(t, request, callID, "exited")
			if terminal.SessionID != session.Load().(string) || terminal.ExitCode == nil || *terminal.ExitCode != 0 || terminal.Stdout != "ready\nsettled\n" {
				t.Errorf("original command terminal evidence changed: %+v", terminal)
			}
			writeContextFlowReply(w, request.Stream, map[string]any{"content": answer}, "stop")
		default:
			t.Errorf("unexpected background recovery request %d", step)
			writeContextFlowReply(w, request.Stream, map[string]any{"content": answer}, "stop")
		}
	})
	e.cfg.Memory.Enabled, e.cfg.Memory.AutoReview = false, false
	e.cfg.Swarm.RecoverReasoningOutputLimit = true
	e.cfg.Swarm.ManagerMaxIterations = 100
	e.cfg.Swarm.MaxCompletionTokens = 16384
	e.cfg.Tools.Enabled = []string{"exec_session"}
	thread, err := e.CreateThread("Midturn background ownership fixture", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := e.SetThreadReasoning(thread.ID, config.ReasoningHigh); err != nil {
		t.Fatal(err)
	}
	workspace = e.WorkspaceDir(thread.ID)
	turn, err := e.StartTurn(thread.ID, "Observe one owned command through its terminal receipt.")
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-capEntered:
	case <-time.After(5 * time.Second):
		t.Fatal("background turn did not reach the later generation")
	}
	ordinaryEventually(t, func() bool {
		data, err := os.ReadFile(filepath.Join(workspace, "process-id"))
		if err != nil {
			return false
		}
		pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
		if err != nil || pid < 1 {
			return false
		}
		firstPID.Store(int64(pid))
		return true
	}, "original background process did not publish its identity")
	releaseCap()
	got := waitForTurn(t, e, turn.ID)
	waitSettled(t, e, thread.ID)
	if got.Status != store.TurnDone || got.Final != answer || len(wire.snapshot()) != 4 {
		t.Fatalf("background exec did not survive within the same recovered turn: requests=%d turn=%+v", len(wire.snapshot()), got)
	}
	starts, err := os.ReadFile(filepath.Join(workspace, "process-starts"))
	if err != nil || string(starts) != "once\n" {
		t.Fatalf("recovery repeated the background process: %q, %v", starts, err)
	}
	events, err := e.store.ListTurnEvents(turn.ID)
	if err != nil {
		t.Fatal(err)
	}
	originalCalls, originalTerminals, claims := 0, 0, 0
	for _, event := range events {
		if event.ToolCallID == callID {
			if event.Kind == swarm.NotifyToolCall.String() {
				originalCalls++
			}
			if event.Kind == swarm.NotifyToolResult.String() {
				originalTerminals++
			}
		}
		if event.Kind == KindModelRetry && strings.Contains(event.Text, `"reason":"output_limit"`) {
			claims++
		}
	}
	if originalCalls != 1 || originalTerminals != 1 || claims != 1 {
		t.Fatalf("original process effects or durable recovery repeated: calls=%d terminal=%d claims=%d", originalCalls, originalTerminals, claims)
	}
	for i, request := range wire.snapshot() {
		if request.ReasoningEffort == nil || *request.ReasoningEffort != config.ReasoningHigh || request.MaxTokens != 16384 {
			t.Fatalf("background request%d changed high reasoning or cap", i+1)
		}
	}
}
