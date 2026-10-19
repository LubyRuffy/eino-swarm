package swarm

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

// Restart is a pause: an unfinished worker's checkpoint must not turn into a
// successful result, lose unread steering, or reset its watchdog budget.
func TestRestartDeadlineKeepsUnreadWorkerState(t *testing.T) {
	reg := NewRegistry()
	t.Cleanup(reg.Close)
	started := make(chan struct{})
	release := make(chan struct{})
	reg.ModelBuilder = func(string, string) model.BaseChatModel {
		return &chunkedModel{turns: []turnScript{{content: []string{"eventual answer"}, inspect: func([]*schema.Message) {
			close(started)
			<-release
		}}}}
	}
	h, err := reg.Spawn(t.Context(), "worker", "inspect current state", reg.ModelBuilder)
	if err != nil {
		t.Fatal(err)
	}
	<-started
	h.mu.Lock()
	h.timeoutFailures = 2
	h.mu.Unlock()
	h.pushInbox("unread peer information")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	before := time.Now()
	workers := reg.PrepareRestart(ctx)
	if time.Since(before) > time.Second {
		t.Fatal("restart waited for an uncooperative model past its deadline")
	}
	if len(workers) != 1 || workers[0].ID != h.ID || workers[0].Finished || workers[0].TimeoutFailures != 2 ||
		workers[0].Task != "inspect current state" || workers[0].Instruction == "" ||
		len(workers[0].Inbox) != 1 || workers[0].Inbox[0] != "unread peer information" {
		t.Fatalf("restart lost the worker checkpoint: %+v", workers)
	}
	close(release)
	waitAgent(t, h)
	if _, err, _ := h.Result(); !errors.Is(err, ErrHandoff) {
		t.Fatalf("restart cancellation became task completion: %v", err)
	}
}

// A cooperative worker uses its next normal model round to summarize. A tool
// proposed by the already-running model must never start after draining began.
func TestRestartSummaryCannotRunToolsOrCompleteTheTask(t *testing.T) {
	reg := NewRegistry()
	t.Cleanup(reg.Close)
	started, release := make(chan struct{}), make(chan struct{})
	var effects atomic.Int32
	reg.ModelBuilder = func(string, string) model.BaseChatModel {
		return &chunkedModel{turns: []turnScript{
			{calls: []schema.ToolCall{rawCall("late-write", "write", "{}")}, inspect: func([]*schema.Message) {
				close(started)
				<-release
			}},
			{content: []string{"Checked the input; output still needs verification."}, inspect: func(msgs []*schema.Message) {
				found := false
				for _, msg := range msgs {
					found = found || strings.Contains(msg.Content, handoffInstruction)
				}
				if !found {
					t.Error("worker did not receive the restart handoff instruction")
				}
			}},
		}}
	}
	rec := &recorder{}
	reg.SetHostNotify(rec.cb())
	h, err := reg.Spawn(t.Context(), "worker", "perform work", reg.ModelBuilder, &fnTool{name: "write", fn: func(context.Context, string) (string, error) {
		effects.Add(1)
		return "written", nil
	}})
	if err != nil {
		t.Fatal(err)
	}
	<-started
	reg.BeginHandoff()
	reg.BeginHandoff()
	close(release)
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	workers := reg.PrepareRestart(ctx)
	if len(workers) != 1 || workers[0].Finished || workers[0].Summary != "Checked the input; output still needs verification." || effects.Load() != 0 {
		t.Fatalf("handoff executed a tool or lost its summary: %+v, effects=%d", workers, effects.Load())
	}
	if len(rec.ofKind(NotifyFinished)) != 0 {
		t.Fatal("a handoff summary was reported as worker completion")
	}
	if _, err, done := h.Result(); !done || !errors.Is(err, ErrHandoff) {
		t.Fatalf("handoff result done=%v error=%v", done, err)
	}
	if next, err := reg.Spawn(t.Context(), "new", "new work", reg.ModelBuilder); next != nil || !errors.Is(err, ErrHandoff) {
		t.Fatalf("spawn admitted during restart: %v, %v", next, err)
	}
	if next, err := reg.Resume(t.Context(), h.ID, "more", reg.ModelBuilder); next != nil || !errors.Is(err, ErrHandoff) {
		t.Fatalf("resume admitted during restart: %v, %v", next, err)
	}
	if next, err := reg.Restore(t.Context(), RestoredWorker{ID: "new-9", Role: "new", Task: "more"}, reg.ModelBuilder); next != nil || !errors.Is(err, ErrHandoff) {
		t.Fatalf("restore admitted during restart: %v, %v", next, err)
	}
}

func TestRestartKeepsActualCompletedWorkers(t *testing.T) {
	reg := NewRegistry()
	t.Cleanup(reg.Close)
	h, err := reg.Spawn(t.Context(), "reader", "inspect", oneShot("verified result"))
	if err != nil {
		t.Fatal(err)
	}
	waitAgent(t, h)
	h.stopForHandoff()
	reg.Stats() // Completed workers must also survive handle pruning.
	workers := reg.PrepareRestart(t.Context())
	if len(workers) != 1 || !workers[0].Finished || workers[0].Result != "verified result" || workers[0].Summary != "" {
		t.Fatalf("actual completed worker was changed by restart: %+v", workers)
	}
}

func TestRestartCheckpointKeepsUnknownToolAndDoesNotConsumeMessages(t *testing.T) {
	reg := NewRegistry()
	t.Cleanup(reg.Close)
	started := make(chan struct{})
	blocked := &fnTool{name: "write", fn: func(ctx context.Context, _ string) (string, error) {
		close(started)
		<-ctx.Done()
		return "unconfirmed output", ctx.Err()
	}}
	reg.ModelBuilder = func(string, string) model.BaseChatModel {
		return &chunkedModel{turns: []turnScript{{calls: []schema.ToolCall{rawCall("write-1", "write", `{"mode":"append"}`)}}}}
	}
	h, err := reg.Spawn(t.Context(), "writer", "write once", reg.ModelBuilder, blocked)
	if err != nil {
		t.Fatal(err)
	}
	<-started
	h.pushInbox("keep unread")
	reg.SetHistory([]adk.Message{schema.UserMessage("original request")})
	reg.SteerManager("pending guidance")
	reg.BeginHandoff()
	if out, err := reg.spawn(t.Context(), `{"role":"writer","task":"unwanted new work"}`); err != nil || sendJSON(t, out)["error"] == nil {
		t.Fatalf("same-role spawn bypassed restart admission: out=%s error=%v", out, err)
	}
	first := reg.SnapshotWorkers()
	if len(first) != 1 || len(first[0].InFlight) != 1 || first[0].InFlight[0].CallID != "write-1" || first[0].InFlight[0].Arguments != `{"mode":"append"}` {
		t.Fatalf("unknown external operation was lost: %+v", first)
	}
	first[0].Inbox[0] = "mutated"
	first[0].History[0].Content = "mutated"
	first[0].InFlight[0].Name = "mutated"
	reg.ManagerHistory()[0].Content = "mutated"
	reg.ManagerInbox()[0].Content = "mutated"
	if reg.SnapshotWorkers()[0].Inbox[0] != "keep unread" || reg.ManagerHistory()[0].Content != "original request" ||
		!strings.Contains(reg.ManagerInbox()[0].Content, "pending guidance") {
		t.Fatal("checkpoint mutated or consumed live state")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	reg.PrepareRestart(ctx)
	waitAgent(t, h)
	last := reg.SnapshotWorkers()
	if len(last[0].InFlight) != 1 || last[0].Finished || len(last[0].Inbox) != 1 || len(last[0].History) == 0 {
		t.Fatalf("cancellation erased the unknown operation: %+v", last)
	}
	pairedUnknown := false
	for _, msg := range last[0].History {
		pairedUnknown = pairedUnknown || (msg.ToolCallID == "write-1" && msg.Content == HandoffUnknownToolResult)
	}
	if !pairedUnknown {
		t.Fatal("interrupted operation replay looked like an ordinary failed tool")
	}
}

func TestRestartBlocksNewManagerToolsAndKeepsTheirRefusalsInReplay(t *testing.T) {
	reg := NewRegistry()
	t.Cleanup(reg.Close)
	reg.BeginHandoff()
	middleware := &historyRecorder{reg: reg}
	var effects atomic.Int32
	call := &adk.ToolContext{Name: "write", CallID: "write-1"}
	inv, err := middleware.WrapInvokableToolCall(t.Context(), func(context.Context, string, ...tool.Option) (string, error) {
		effects.Add(1)
		return "written", nil
	}, call)
	if err != nil {
		t.Fatal(err)
	}
	if out, err := inv(t.Context(), "{}"); err != nil || out != handoffToolResult {
		t.Fatalf("manager tool refusal=%q error=%v", out, err)
	}
	stream, err := middleware.WrapStreamableToolCall(t.Context(), func(context.Context, string, ...tool.Option) (*schema.StreamReader[string], error) {
		effects.Add(1)
		return schema.StreamReaderFromArray([]string{"written"}), nil
	}, &adk.ToolContext{Name: "write", CallID: "write-2"})
	if err != nil {
		t.Fatal(err)
	}
	sr, err := stream(t.Context(), "{}")
	if err != nil {
		t.Fatal(err)
	}
	defer sr.Close()
	if out, err := sr.Recv(); err != nil || out != handoffToolResult || effects.Load() != 0 || len(reg.ManagerHistory()) != 2 {
		t.Fatalf("streamed manager tool refusal=%q error=%v calls=%d", out, err, effects.Load())
	}
}

func TestRestoreRestartUsesStructuredHistoryAndUnreadInbox(t *testing.T) {
	reg := NewRegistry()
	t.Cleanup(reg.Close)
	var calls atomic.Int32
	builder := func(string, string) model.BaseChatModel {
		return &chunkedModel{turns: []turnScript{{content: []string{"resumed"}, inspect: func(msgs []*schema.Message) {
			calls.Add(1)
			seen := map[string]bool{}
			for _, msg := range msgs {
				seen[msg.Content] = true
			}
			if !seen["original input"] || !seen["[steer] unread peer input"] || !seen["[steer] inspect unknown effects first"] ||
				!seen[HandoffUnknownToolResult] || seen[handoffInstruction] {
				t.Errorf("restored messages lost inputs or kept stale restart instruction: %+v", seen)
			}
		}}}}
	}
	h, err := reg.Restore(t.Context(), RestoredWorker{ID: "worker-9", Role: "worker", Task: "original input", TimeoutFailures: 2,
		History: []*schema.Message{schema.UserMessage("original input"), schema.AssistantMessage("running", []schema.ToolCall{rawCall("old-write", "write", "{}")}), schema.UserMessage(handoffInstruction)},
		Inbox:   []string{"unread peer input"}, Seed: "inspect unknown effects first"}, builder)
	if err != nil {
		t.Fatal(err)
	}
	waitAgent(t, h)
	if calls.Load() != 1 {
		t.Fatalf("restored worker model calls=%d", calls.Load())
	}
	if result, err, done := h.Result(); !done || err != nil || result != "resumed" {
		t.Fatalf("restored worker result=%q error=%v done=%v", result, err, done)
	}
}

func TestRestartCheckpointKeepsRealFailureAndHandlesEmptyRegistry(t *testing.T) {
	reg := NewRegistry()
	h := &Handle{ID: "worker-2", Role: "worker", done: make(chan struct{}), err: errors.New("tool failed"), finished: time.Now(), timeoutFailures: 2}
	h.handoff.inFlight = map[string]WorkerToolCheckpoint{"z": {CallID: "z"}, "a": {CallID: "a"}}
	reg.agents[h.ID] = h
	close(h.done)
	w := reg.PrepareRestart(t.Context())[0]
	if !w.Finished || w.Err != "tool failed" || w.TimeoutFailures != 2 || w.InFlight[0].CallID != "a" {
		t.Fatalf("handoff reset a real failure: %+v", w)
	}
	reg.Close()
	if got := reg.PrepareRestart(t.Context()); len(got) != 0 {
		t.Fatalf("closed registry kept workers: %+v", got)
	}
	reg = NewRegistry()
	if got := reg.PrepareRestart(t.Context()); len(got) != 0 {
		t.Fatalf("empty registry acquired workers: %+v", got)
	}
}

func TestRestartStopsStreamingWorkerToolsBeforeTheyStart(t *testing.T) {
	reg := NewRegistry()
	h := &Handle{ID: "worker-1", Role: "worker"}
	in := &Injector{reg: reg, handle: h}
	reg.BeginHandoff()
	var effects int
	wrapped, err := in.WrapStreamableToolCall(t.Context(), func(context.Context, string, ...tool.Option) (*schema.StreamReader[string], error) {
		effects++
		return schema.StreamReaderFromArray([]string{"changed"}), nil
	}, &adk.ToolContext{Name: "write", CallID: "write-1"})
	if err != nil {
		t.Fatal(err)
	}
	sr, err := wrapped(t.Context(), "{}")
	if err != nil {
		t.Fatal(err)
	}
	defer sr.Close()
	if out, err := sr.Recv(); err != nil || out != handoffToolResult || effects != 0 {
		t.Fatalf("streaming tool ran after restart: out=%q error=%v effects=%d", out, err, effects)
	}
}

func TestStreamingWorkerCheckpointTracksCompletionAndUnknownOutcome(t *testing.T) {
	for _, outcome := range []string{"success", "open-error", "read-error"} {
		t.Run(outcome, func(t *testing.T) {
			reg := NewRegistry()
			h := &Handle{ID: "worker-1", Role: "worker"}
			reg.agents[h.ID] = h
			in := &Injector{reg: reg, handle: h}
			wrapped, err := in.WrapStreamableToolCall(t.Context(), func(context.Context, string, ...tool.Option) (*schema.StreamReader[string], error) {
				if outcome == "open-error" {
					return nil, errors.New("connection failed")
				}
				if outcome == "read-error" {
					sr, sw := schema.Pipe[string](2)
					sw.Send("partial", nil)
					sw.Send("", errors.New("connection dropped"))
					sw.Close()
					return sr, nil
				}
				return schema.StreamReaderFromArray([]string{"complete", " result"}), nil
			}, &adk.ToolContext{Name: "write", CallID: "write-1"})
			if err != nil {
				t.Fatal(err)
			}
			sr, err := wrapped(t.Context(), "{}")
			if outcome == "open-error" {
				if err == nil {
					t.Fatal("tool open failure disappeared")
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				defer sr.Close()
				if len(reg.SnapshotWorkers()[0].InFlight) != 1 {
					t.Fatal("open stream was reported completed")
				}
				for {
					_, err = sr.Recv()
					if err != nil {
						break
					}
				}
				if (err == io.EOF) != (outcome == "success") {
					t.Fatalf("stream error=%v", err)
				}
			}
			w := reg.SnapshotWorkers()[0]
			if (len(w.InFlight) == 0) != (outcome == "success") || len(w.History) != 1 {
				t.Fatalf("stream outcome checkpoint=%+v", w)
			}
		})
	}
}

// Admission can win immediately before restart. The registered identity and
// task must be checkpointable even while its spawn notification is persisting.
func TestRestartBeforeWorkerStartupPreservesTaskAndCancelsItsLifetime(t *testing.T) {
	reg := NewRegistry()
	t.Cleanup(reg.Close)
	registered, release := make(chan struct{}), make(chan struct{})
	reg.spawnHook = func(string, string, string) {
		close(registered)
		<-release
	}
	created := make(chan *Handle, 1)
	go func() {
		h, err := reg.Spawn(t.Context(), "worker", "keep the registered task", oneShot("should not complete"))
		if err != nil {
			t.Error(err)
		}
		created <- h
	}()
	<-registered
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	workers := reg.PrepareRestart(ctx)
	if len(workers) != 1 || workers[0].Task != "keep the registered task" || workers[0].Instruction != "keep the registered task" || workers[0].Finished {
		t.Fatalf("registered task was missing from restart checkpoint: %+v", workers)
	}
	close(release)
	h := <-created
	waitAgent(t, h)
	if _, err, done := h.Result(); !done || !errors.Is(err, ErrHandoff) {
		t.Fatalf("late startup escaped restart: done=%v error=%v", done, err)
	}
}

func TestRestartRetainsAWorkerThatCouldNotStartItsModel(t *testing.T) {
	reg := NewRegistry()
	t.Cleanup(reg.Close)
	h, err := reg.Spawn(t.Context(), "worker", "inspect", func(string, string) model.BaseChatModel { return nil })
	if err != nil {
		t.Fatal(err)
	}
	waitAgent(t, h)
	w := reg.PrepareRestart(t.Context())[0]
	if !w.Finished || w.Err == "" || w.Summary != "" {
		t.Fatalf("startup failure became successful handoff: %+v", w)
	}
}

func TestResumeAdmissionCannotReplaceARunningWorker(t *testing.T) {
	reg := NewRegistry()
	t.Cleanup(reg.Close)
	h := &Handle{ID: "worker-1", Role: "worker", done: make(chan struct{})}
	reg.agents[h.ID] = h
	if next, err := reg.claimForResume(&agentPast{ID: h.ID, Role: h.Role}, true); next != nil || err == nil || !strings.Contains(err.Error(), "still running") {
		t.Fatalf("resume replaced a live identity: handle=%v error=%v", next, err)
	}
}

func TestRestartReplaySealsUnknownToolsBeforeLaterMessages(t *testing.T) {
	msgs := restoredWorkerMessages("original", []*schema.Message{
		schema.ToolMessage("older trimmed result", "trimmed-call"),
		schema.AssistantMessage("working", []schema.ToolCall{rawCall("unknown-write", "write", "{}")}),
		schema.UserMessage(handoffInstruction),
		schema.AssistantMessage("unfinished handoff", nil),
	})
	if len(msgs) != 4 || msgs[0].Role != schema.Assistant || msgs[1].Role != schema.Tool ||
		msgs[1].ToolCallID != "unknown-write" || msgs[1].Content != HandoffUnknownToolResult ||
		msgs[2].Content != "unfinished handoff" || msgs[3].Role != schema.User {
		t.Fatalf("restart replay has invalid tool pairing: %+v", msgs)
	}
	known := restoredWorkerMessages("original", []*schema.Message{
		schema.UserMessage("original"),
		schema.AssistantMessage("working", []schema.ToolCall{rawCall("known-read", "read", "{}")}),
		schema.ToolMessage("verified read", "known-read"),
	})
	if len(known) != 3 || known[2].Content != "verified read" {
		t.Fatal("restart changed a confirmed tool result")
	}
}

// exec reports cancellation in its JSON result with a nil Go error. That
// preserves its tool API, but cannot confirm whether an external commit ran.
func TestRestartEncodedToolCancellationKeepsUnknownOutcome(t *testing.T) {
	const encoded = `{"error":"command canceled","failed":true,"stdout":"partial"}`
	for _, streaming := range []bool{false, true} {
		t.Run(map[bool]string{false: "invokable", true: "streamable"}[streaming], func(t *testing.T) {
			reg := NewRegistry()
			h := &Handle{ID: "worker-1", Role: "worker", timeoutFailures: 2}
			reg.agents[h.ID] = h
			in := &Injector{reg: reg, handle: h}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			call := &adk.ToolContext{Name: "exec", CallID: "uncertain-write"}
			if streaming {
				wrapped, err := in.WrapStreamableToolCall(ctx, func(context.Context, string, ...tool.Option) (*schema.StreamReader[string], error) {
					reg.BeginHandoff()
					cancel()
					return schema.StreamReaderFromArray([]string{encoded}), nil
				}, call)
				if err != nil {
					t.Fatal(err)
				}
				sr, err := wrapped(ctx, "{}")
				if err != nil {
					t.Fatal(err)
				}
				defer sr.Close()
				if out, err := sr.Recv(); err != nil || out != encoded {
					t.Fatalf("restart changed the streaming tool API: out=%q error=%v", out, err)
				}
				if _, err := sr.Recv(); err != io.EOF {
					t.Fatalf("restart changed the streaming terminal error: %v", err)
				}
			} else {
				wrapped, err := in.WrapInvokableToolCall(ctx, func(context.Context, string, ...tool.Option) (string, error) {
					reg.BeginHandoff()
					cancel()
					return encoded, nil
				}, call)
				if err != nil {
					t.Fatal(err)
				}
				if out, err := wrapped(ctx, "{}"); err != nil || out != encoded {
					t.Fatalf("restart changed the invokable tool API: out=%q error=%v", out, err)
				}
			}
			w := reg.SnapshotWorkers()[0]
			if len(w.InFlight) != 1 || len(w.History) != 1 || w.History[0].Content != HandoffUnknownToolResult {
				t.Fatalf("encoded cancellation was treated as a confirmed external outcome: %+v", w)
			}
			rec := &recorder{}
			reg.setSink(rec.cb())
			reg.emit(Notification{Kind: NotifyToolResult, AgentID: h.ID, ToolCallID: call.CallID, Text: encoded})
			if notes := rec.ofKind(NotifyToolResult); len(notes) != 1 || notes[0].Text != HandoffUnknownToolResult {
				t.Fatalf("late tool event overwrote the checkpoint's unknown outcome: %+v", notes)
			}
			h.stopForHandoff()
			h.completeWorker("", ctx.Err(), time.Second, ctx.Err(), ctx.Err())
			if h.timeoutFailures != 2 || !errors.Is(h.err, ErrHandoff) {
				t.Fatalf("restart cancellation changed the watchdog budget: failures=%d error=%v", h.timeoutFailures, h.err)
			}
		})
	}
}

func TestRestartRetainsConfirmedToolResultNotifications(t *testing.T) {
	reg := NewRegistry()
	h := &Handle{ID: "worker-1", Role: "worker"}
	reg.agents[h.ID] = h
	reg.beginWorkerTool(h, "exec", "known-write", "{}")
	reg.BeginHandoff()
	reg.Close()
	h.finishTool("exec", "known-write", "confirmed receipt", nil)
	rec := &recorder{}
	reg.setSink(rec.cb())
	for _, id := range []string{h.ID, DefaultManagerID} {
		reg.emit(Notification{Kind: NotifyToolResult, AgentID: id, ToolCallID: "known-write", Text: "confirmed receipt"})
	}
	for _, note := range rec.ofKind(NotifyToolResult) {
		if note.Text != "confirmed receipt" {
			t.Fatalf("restart erased a confirmed effect: %+v", note)
		}
	}
}

func TestRestartManagerEncodedCancellationPreservesUnknownEvidence(t *testing.T) {
	const encoded = `{"error":"command canceled","failed":true}`
	for _, streaming := range []bool{false, true} {
		t.Run(map[bool]string{false: "invokable", true: "streamable"}[streaming], func(t *testing.T) {
			reg := NewRegistry()
			rec := &recorder{}
			reg.setSink(rec.cb())
			middleware := &historyRecorder{reg: reg}
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			call := &adk.ToolContext{Name: "exec", CallID: "manager-write"}
			if streaming {
				wrapped, err := middleware.WrapStreamableToolCall(ctx, func(context.Context, string, ...tool.Option) (*schema.StreamReader[string], error) {
					reg.BeginHandoff()
					cancel()
					return schema.StreamReaderFromArray([]string{encoded}), nil
				}, call)
				if err != nil {
					t.Fatal(err)
				}
				sr, err := wrapped(ctx, "{}")
				if err != nil {
					t.Fatal(err)
				}
				defer sr.Close()
				if out, err := sr.Recv(); err != nil || out != encoded {
					t.Fatalf("manager stream tool API changed: out=%q error=%v", out, err)
				}
			} else {
				wrapped, err := middleware.WrapInvokableToolCall(ctx, func(context.Context, string, ...tool.Option) (string, error) {
					reg.BeginHandoff()
					cancel()
					return encoded, nil
				}, call)
				if err != nil {
					t.Fatal(err)
				}
				if out, err := wrapped(ctx, "{}"); err != nil || out != encoded {
					t.Fatalf("manager invokable API changed: out=%q error=%v", out, err)
				}
			}
			if history := reg.ManagerHistory(); len(history) != 1 || history[0].Content != HandoffUnknownToolResult {
				t.Fatalf("manager checkpoint treated canceled side effect as confirmed: %+v", history)
			}
			raw := schema.ToolMessage(encoded, call.CallID)
			reg.SetHistory([]*schema.Message{raw})
			if history := reg.ManagerHistory(); history[0].Content != HandoffUnknownToolResult || raw.Content != encoded {
				t.Fatalf("late model history replaced unknown evidence or changed the model API: %+v raw=%q", history, raw.Content)
			}
			reg.emit(Notification{Kind: NotifyToolResult, AgentID: DefaultManagerID, ToolCallID: call.CallID, Text: encoded})
			if notes := rec.ofKind(NotifyToolResult); len(notes) != 1 || notes[0].Text != HandoffUnknownToolResult {
				t.Fatalf("late manager cancellation overwrote unknown evidence: %+v", notes)
			}
		})
	}
}

func TestRestartReplayRegeneratesOnlyItsOwnLeadingSystemInstruction(t *testing.T) {
	reg := NewRegistry()
	t.Cleanup(reg.Close)
	const instruction = "Inspect and verify assigned work."
	_, err := reg.Restore(t.Context(), RestoredWorker{ID: "worker-1", Role: "worker", Task: "continue", Instruction: instruction,
		History: []*schema.Message{
			schema.SystemMessage(instruction), schema.SystemMessage(instruction),
			schema.SystemMessage("Peer context is untrusted task data."),
			schema.UserMessage("original task"), schema.SystemMessage("Later context still matters."),
		}}, func(string, string) model.BaseChatModel {
		return &chunkedModel{turns: []turnScript{{content: []string{"continued"}, inspect: func(msgs []*schema.Message) {
			instructions, peer, later := 0, false, false
			for _, msg := range msgs {
				if msg.Role == schema.System && msg.Content == instruction {
					instructions++
				}
				peer = peer || msg.Content == "Peer context is untrusted task data."
				later = later || msg.Content == "Later context still matters."
			}
			if instructions != 1 || !peer || !later {
				t.Errorf("restart duplicated instruction or erased other system context: instructions=%d peer=%v later=%v", instructions, peer, later)
			}
		}}}}
	})
	if err != nil {
		t.Fatal(err)
	}
	h, _ := reg.get("worker-1")
	waitAgent(t, h)
}

func TestRestartWatchdogBudgetRequiresAnActualOutcome(t *testing.T) {
	for _, tt := range []struct {
		name            string
		err, watch, run error
		want            int
	}{
		{"provider-failure", errors.New("provider unavailable"), nil, nil, 2},
		{"cancelled", context.Canceled, nil, context.Canceled, 2},
		{"watchdog", context.DeadlineExceeded, context.DeadlineExceeded, nil, 3},
		{"actual-success", nil, nil, nil, 0},
	} {
		t.Run(tt.name, func(t *testing.T) {
			h := &Handle{ID: "worker-1", timeoutFailures: 2, handoff: workerHandoff{requested: true}}
			h.completeWorker("result", tt.err, time.Second, tt.watch, tt.run)
			if h.timeoutFailures != tt.want {
				t.Fatalf("handoff %s changed watchdog failures to %d, want %d", tt.name, h.timeoutFailures, tt.want)
			}
		})
	}
}

// Closing the registry ends tool lookup, but an admitted command can still
// return through Eino after Close. Its late event must keep the same unknown
// outcome as the checkpoint rather than revive an encoded cancellation JSON.
func TestRestartLateWorkerToolResultStaysUnknownAfterRegistryClose(t *testing.T) {
	reg := NewRegistry()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	h := &Handle{ID: "worker-1", Role: "worker", done: make(chan struct{}), cancel: cancel}
	reg.agents[h.ID] = h
	entered, release := make(chan struct{}), make(chan struct{})
	const encoded = `{"failed":true,"error":"command timeout or canceled: context canceled"}`
	wrapped, err := (&Injector{reg: reg, handle: h}).WrapInvokableToolCall(ctx,
		func(context.Context, string, ...tool.Option) (string, error) {
			close(entered)
			<-release
			return encoded, nil
		}, &adk.ToolContext{Name: "exec", CallID: "late-write"})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		out, err := wrapped(ctx, "{}")
		if err != nil || out != encoded {
			t.Errorf("closing the registry changed the admitted tool's return: out=%q error=%v", out, err)
		}
		reg.emitComplete(h.Role, h.ID, &adk.AgentEvent{Output: &adk.AgentOutput{MessageOutput: &adk.MessageVariant{
			Message: schema.ToolMessage(out, "late-write"),
		}}})
	}()
	<-entered
	reg.BeginHandoff()
	before := reg.SnapshotWorkers()
	if len(before) != 1 || len(before[0].InFlight) != 1 {
		t.Fatal("handoff did not record its admitted command")
	}
	rec := &recorder{}
	reg.setSink(rec.cb())
	reg.Close()
	reg.Close()
	if running, finished := reg.Stats(); running != 0 || finished != 0 {
		t.Fatal("closed registry retained queryable workers")
	}
	close(release)
	<-done
	if notes := rec.ofKind(NotifyToolResult); len(notes) != 1 || notes[0].Text != HandoffUnknownToolResult {
		t.Fatalf("late result overwrote the saved unknown outcome after Close: %+v", notes)
	}
}
