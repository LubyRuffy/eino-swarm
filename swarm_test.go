package swarm

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

// scriptedModel plays a fixed per-turn script; used for both workers and
// (implicitly) proof-of-delivery assertions.
type scriptedModel struct {
	mu    sync.Mutex
	turns []func(turn int, msgs []*schema.Message) *schema.Message
	seen  int
}

func (m *scriptedModel) Generate(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.Message, error) {
	m.mu.Lock()
	i := m.seen
	m.seen++
	m.mu.Unlock()
	if i >= len(m.turns) {
		i = len(m.turns) - 1
	}
	return m.turns[i](i, input), nil
}

func (m *scriptedModel) Stream(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	// degrade to Generate so streaming-mode runs still work in tests
	msg, err := m.Generate(ctx, input, opts...)
	if err != nil {
		return nil, err
	}
	sr, sw := schema.Pipe[*schema.Message](1)
	sw.Send(msg, nil)
	sw.Close()
	return sr, nil
}

func rawCall(id, name, args string) schema.ToolCall {
	return schema.ToolCall{ID: id, Type: "function", Function: schema.FunctionCall{Name: name, Arguments: args}}
}

// workTool simulates sub-agent work with a sleep; honors cancellation.
type workTool struct{ d time.Duration }

func (t *workTool) Info(ctx context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{Name: "work", Desc: "simulate work",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{})}, nil
}

func (t *workTool) InvokableRun(ctx context.Context, _ string, _ ...tool.Option) (string, error) {
	select {
	case <-time.After(t.d):
		return "work done", nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

func invokable(t *testing.T, bt tool.BaseTool) tool.InvokableTool {
	t.Helper()
	it, ok := bt.(tool.InvokableTool)
	if !ok {
		t.Fatalf("tool %T is not invokable", bt)
	}
	return it
}

func assertCtlRefuse(t *testing.T, out string, err error, want string) {
	t.Helper()
	if err != nil {
		t.Fatalf("a caller mistake must not be a tool error (that kills the graph): %v", err)
	}
	var got map[string]any
	if json.Unmarshal([]byte(out), &got) != nil {
		t.Fatalf("refuse must be JSON, got %q", out)
	}
	if _, ok := got["agent_id"]; ok {
		t.Fatalf("must not start or resume a worker: %s", out)
	}
	msg, _ := got["error"].(string)
	if !strings.Contains(msg, want) {
		t.Fatalf("error %q want %q in %s", msg, want, out)
	}
}

func workerTurns(deliverSteer *[]string) []func(int, []*schema.Message) *schema.Message {
	return []func(int, []*schema.Message) *schema.Message{
		func(turn int, msgs []*schema.Message) *schema.Message {
			return schema.AssistantMessage("starting work", []schema.ToolCall{rawCall("w1", "work", "{}")})
		},
		func(turn int, msgs []*schema.Message) *schema.Message {
			var got []string
			for _, m := range msgs {
				if m.Role == schema.User && strings.HasPrefix(m.Content, "[steer] ") {
					got = append(got, m.Content)
				}
			}
			if deliverSteer != nil {
				*deliverSteer = got
			}
			if len(got) > 0 {
				return schema.AssistantMessage(fmt.Sprintf("STEERED(%d)", len(got)), nil)
			}
			return schema.AssistantMessage("DONE", nil)
		},
	}
}

var deliverSteer *[]string

// ---------- core behavior ----------

func TestSpawnParallelAndSteer(t *testing.T) {
	steered := []string{}
	deliverSteer = &steered
	defer func() { deliverSteer = nil }()

	reg := NewRegistry()
	reg.SubAgentTools = []tool.BaseTool{&workTool{d: 400 * time.Millisecond}}
	reg.ModelBuilder = func(role, agentID string) model.BaseChatModel {
		return &scriptedModel{turns: []func(int, []*schema.Message) *schema.Message{
			func(_ int, msgs []*schema.Message) *schema.Message {
				return schema.AssistantMessage("starting work", []schema.ToolCall{rawCall("w1", "work", "{}")})
			},
			func(_ int, msgs []*schema.Message) *schema.Message {
				for _, m := range msgs {
					t.Logf("[%s] msg role=%s content=%.60q", agentID, m.Role, m.Content)
				}
				var got []string
				for _, m := range msgs {
					if m.Role == schema.User && strings.HasPrefix(m.Content, "[steer] ") {
						got = append(got, m.Content)
					}
				}
				if len(got) > 0 {
					return schema.AssistantMessage(fmt.Sprintf("STEERED(%d)", len(got)), nil)
				}
				return schema.AssistantMessage("DONE", nil)
			},
		}}
	}

	tools := reg.Tools()
	spawnT, sendT, waitT := invokable(t, tools[0]), invokable(t, tools[1]), invokable(t, tools[2])

	// spawn two sub-agents concurrently, exactly as ToolsNode would
	ids := make([]string, 2)
	var wg sync.WaitGroup
	for i, role := range []string{"alpha", "beta"} {
		wg.Add(1)
		go func(i int, role string) {
			defer wg.Done()
			out, err := spawnT.InvokableRun(context.Background(),
				fmt.Sprintf(`{"role":%q,"task":"work on %s"}`, role, role))
			if err != nil {
				t.Errorf("spawn %s: %v", role, err)
				return
			}
			var resp struct {
				AgentID string `json:"agent_id"`
			}
			if err := json.Unmarshal([]byte(out), &resp); err != nil {
				t.Errorf("spawn %s: bad json %q", role, out)
				return
			}
			ids[i] = resp.AgentID
		}(i, role)
	}
	wg.Wait()
	if ids[0] == "" || ids[1] == "" {
		t.Fatalf("spawn failed: ids=%v", ids)
	}

	// steer alpha while it is mid-work
	out, err := sendT.InvokableRun(context.Background(),
		fmt.Sprintf(`{"agent_id":%q,"text":"priority: fast path"}`, ids[0]))
	if err != nil || !strings.Contains(out, `"delivered":true`) {
		t.Fatalf("send_message: out=%q err=%v", out, err)
	}

	// wait_agents returns the instant the first sub-agent finishes, so a
	// manager collecting a whole batch waits in a loop. Both workers run in
	// parallel, so the loop still clears well inside one work interval rather
	// than the sum of two.
	start := time.Now()
	var res string
	for time.Since(start) < 4*time.Second {
		r, err := waitT.InvokableRun(context.Background(),
			fmt.Sprintf(`{"agent_ids":[%q,%q],"timeout_s":5}`, ids[0], ids[1]))
		if err != nil {
			t.Fatalf("wait_agents: %v", err)
		}
		res += r
		if !strings.Contains(r, `"status":"running"`) {
			break
		}
	}
	wall := time.Since(start)
	if wall > 1500*time.Millisecond {
		t.Errorf("sub-agents appear sequential: wall=%v res=%s", wall, res)
	}
	if !strings.Contains(res, "STEERED(1)") {
		t.Errorf("steering not delivered: %s", res)
	}
}

func TestCloseCancels(t *testing.T) {
	reg := NewRegistry()
	reg.ModelBuilder = func(role, agentID string) model.BaseChatModel {
		return &scriptedModel{turns: []func(int, []*schema.Message) *schema.Message{
			func(turn int, msgs []*schema.Message) *schema.Message {
				return schema.AssistantMessage("starting", []schema.ToolCall{rawCall("w1", "work", "{}")})
			},
		}}
	}
	tools := reg.Tools()
	closeT := invokable(t, tools[3])

	out, err := closeT.InvokableRun(context.Background(), `{"agent_id":"nope"}`)
	assertCtlRefuse(t, out, err, "unknown agent")
	out, err = closeT.InvokableRun(context.Background(), `{"agent_id":""}`)
	assertCtlRefuse(t, out, err, "agent_id is required")
	out, err = closeT.InvokableRun(context.Background(), `{`)
	assertCtlRefuse(t, out, err, "could not read the arguments")

	h, err := reg.Spawn(context.Background(), "slow", "long work",
		func(role, id string) model.BaseChatModel {
			return &scriptedModel{turns: []func(int, []*schema.Message) *schema.Message{
				func(turn int, msgs []*schema.Message) *schema.Message {
					return schema.AssistantMessage("starting", []schema.ToolCall{rawCall("w1", "work", "{}")})
				},
			}}
		}, &workTool{d: 10 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	if _, err := closeT.InvokableRun(context.Background(), `{"agent_id":"`+h.ID+`"}`); err != nil {
		t.Fatalf("close_agent: %v", err)
	}
	select {
	case <-h.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("close_agent did not cancel the running agent")
	}
	_, err, finished := h.Result()
	if !finished || err == nil {
		t.Fatalf("cancelled agent should be finished with an error (finished=%v err=%v)", finished, err)
	}
}

// Wait and Cleanup are what a host uses to end a turn: wait for the workers it
// asked for, then make sure nothing it forgot is left running.
func TestWaitAndCleanupEndATurn(t *testing.T) {
	reg := NewRegistry()
	reg.ModelBuilder = func(role, agentID string) model.BaseChatModel {
		return &scriptedModel{turns: []func(int, []*schema.Message) *schema.Message{
			func(turn int, msgs []*schema.Message) *schema.Message {
				return schema.AssistantMessage("starting", []schema.ToolCall{rawCall("w1", "work", "{}")})
			},
			func(turn int, msgs []*schema.Message) *schema.Message {
				return schema.AssistantMessage("DONE", nil)
			},
		}}
	}

	// nothing spawned yet: waiting must return at once rather than poll
	if err := reg.Wait(context.Background(), time.Second); err != nil {
		t.Fatalf("Wait with no agents: %v", err)
	}

	quick, err := reg.Spawn(context.Background(), "quick", "short work",
		reg.ModelBuilder, &workTool{d: 20 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	if err := reg.Wait(context.Background(), 5*time.Second); err != nil {
		t.Fatalf("Wait: %v", err)
	}
	if _, _, finished := quick.Result(); !finished {
		t.Fatal("Wait returned while an agent was still running")
	}

	// a worker that outlives the turn: Wait reports the timeout rather than
	// blocking for as long as the worker feels like taking
	slow, err := reg.Spawn(context.Background(), "slow", "long work",
		reg.ModelBuilder, &workTool{d: 30 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	err = reg.Wait(context.Background(), 50*time.Millisecond)
	if err == nil || !strings.Contains(err.Error(), "still running") {
		t.Fatalf("Wait should report the agents it gave up on, got %v", err)
	}

	// a cancelled caller is not a timeout: it is the caller's own error
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := reg.Wait(ctx, time.Second); err == nil {
		t.Fatal("Wait should return the caller's cancellation")
	}

	if killed := reg.Cleanup(); killed != 1 {
		t.Fatalf("Cleanup killed %d agents, want 1", killed)
	}
	select {
	case <-slow.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("Cleanup left an agent running")
	}
	// the registry stays usable: cleanup ends a turn, it does not end the swarm
	if _, err := reg.Spawn(context.Background(), "after", "more work",
		reg.ModelBuilder, &workTool{d: time.Millisecond}); err != nil {
		t.Fatalf("Cleanup closed the registry: %v", err)
	}
	if killed := reg.Cleanup(); killed < 0 {
		t.Fatal("Cleanup must not report a negative count")
	}
	reg.Close()
}

// The manager is a language model reading these results, so every failure has
// to come back as something it can act on: which agent, and what went wrong.
func TestLifecycleToolsReportFailuresToTheManager(t *testing.T) {
	reg := NewRegistry()
	reg.ModelBuilder = func(role, agentID string) model.BaseChatModel {
		return &scriptedModel{turns: []func(int, []*schema.Message) *schema.Message{
			func(turn int, msgs []*schema.Message) *schema.Message {
				return schema.AssistantMessage("starting", []schema.ToolCall{rawCall("w1", "work", "{}")})
			},
			func(turn int, msgs []*schema.Message) *schema.Message {
				return schema.AssistantMessage("DONE", nil)
			},
		}}
	}
	tools := reg.Tools()
	spawnT, sendT, waitT, closeT, resumeT := invokable(t, tools[0]), invokable(t, tools[1]),
		invokable(t, tools[2]), invokable(t, tools[3]), invokable(t, tools[4])
	ctx := context.Background()

	// malformed arguments are the model's mistake: tell it which tool, do not
	// kill the ReAct graph
	for name, it := range map[string]tool.InvokableTool{
		"spawn_agent": spawnT, "send_message": sendT,
		"wait_agents": waitT, "close_agent": closeT, "resume_agent": resumeT,
	} {
		out, err := it.InvokableRun(ctx, "{not json")
		if err != nil {
			t.Fatalf("%s malformed args must not kill the graph: %v", name, err)
		}
		if !strings.Contains(out, name) {
			t.Fatalf("%s should name itself in the result, got %s", name, out)
		}
	}

	// an id that does not exist: wait reports it per agent rather than failing
	// the whole call, because the other ids in the same call are still useful
	out, err := waitT.InvokableRun(ctx, `{"agent_ids":["ghost"],"timeout_s":1}`)
	if err != nil {
		t.Fatalf("wait_agents: %v", err)
	}
	if !strings.Contains(out, "unknown agent") {
		t.Fatalf("wait_agents=%s", out)
	}
	out, err = sendT.InvokableRun(ctx, `{"agent_id":"ghost","text":"hello"}`)
	if err != nil {
		t.Fatalf("send_message to an unknown agent must not fail the caller: %v", err)
	}
	if !strings.Contains(out, `"delivered":false`) || !strings.Contains(out, "unknown agent") {
		t.Fatalf("send_message=%s", out)
	}

	// a worker that outlasts the wait: reported as still running, with whatever
	// it was last doing, so the manager can decide to wait again or give up
	slow, err := reg.Spawn(ctx, "slow", "long work", reg.ModelBuilder, &workTool{d: 30 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	out, err = waitT.InvokableRun(ctx, `{"agent_ids":["`+slow.ID+`"],"timeout_s":1}`)
	if err != nil {
		t.Fatalf("wait_agents: %v", err)
	}
	if !strings.Contains(out, `"status":"running"`) {
		t.Fatalf("a slow agent should be reported as running, got %s", out)
	}
	// nothing finished before the deadline, so the manager is told it timed out
	// rather than being handed a fake final result
	if !strings.Contains(out, `"timed_out":true`) {
		t.Fatalf("a wait that finished nothing should report timed_out, got %s", out)
	}

	// steering an agent that has already answered is not an error, but the
	// manager must not believe the message was delivered
	quick, err := reg.Spawn(ctx, "quick", "short work", reg.ModelBuilder, &workTool{d: time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-quick.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("the quick agent never finished")
	}
	out, err = sendT.InvokableRun(ctx, `{"agent_id":"`+quick.ID+`","text":"one more thing"}`)
	if err != nil {
		t.Fatalf("send_message: %v", err)
	}
	if !strings.Contains(out, `"delivered":false`) || !strings.Contains(out, "already finished") {
		t.Fatalf("send_message=%s", out)
	}

	// a cancelled manager stops waiting instead of holding the turn open
	cancelled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := waitT.InvokableRun(cancelled, `{"agent_ids":["`+slow.ID+`"]}`); err == nil {
		t.Fatal("wait_agents should return when the caller is cancelled")
	}
	reg.Close()
}

// A dead-waiting manager is what makes a running swarm look frozen, so
// wait_agents must come back the moment the first sub-agent finishes and hand
// the manager a mix of a finished result and the still-running ones — never
// run the timeout down waiting for the whole batch.
func TestWaitReturnsWhenTheFirstOfManyFinishes(t *testing.T) {
	reg := NewRegistry()
	reg.ModelBuilder = func(role, agentID string) model.BaseChatModel {
		return &scriptedModel{turns: []func(int, []*schema.Message) *schema.Message{
			func(int, []*schema.Message) *schema.Message {
				return schema.AssistantMessage("starting", []schema.ToolCall{rawCall("w1", "work", "{}")})
			},
			func(int, []*schema.Message) *schema.Message {
				return schema.AssistantMessage("DONE", nil)
			},
		}}
	}
	waitT := invokable(t, reg.Tools()[2])
	ctx := context.Background()

	fast, err := reg.Spawn(ctx, "fast", "quick", reg.ModelBuilder, &workTool{d: 10 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	slow, err := reg.Spawn(ctx, "slow", "long", reg.ModelBuilder, &workTool{d: 30 * time.Second})
	if err != nil {
		t.Fatal(err)
	}

	// A generous timeout: the point is that wait returns on the first finish,
	// not that it eventually gives up.
	start := time.Now()
	out, err := waitT.InvokableRun(ctx,
		fmt.Sprintf(`{"agent_ids":[%q,%q],"timeout_s":10}`, fast.ID, slow.ID))
	if err != nil {
		t.Fatalf("wait_agents: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("wait blocked for the whole batch instead of the first finish: %v", elapsed)
	}

	var rep struct {
		Agents []struct {
			AgentID string `json:"agent_id"`
			Status  string `json:"status"`
		} `json:"agents"`
		TimedOut bool `json:"timed_out"`
	}
	if err := json.Unmarshal([]byte(out), &rep); err != nil {
		t.Fatalf("wait_agents output is not JSON: %v (%s)", err, out)
	}
	if rep.TimedOut {
		t.Fatalf("returning on a finish must not be reported as a timeout: %s", out)
	}
	status := map[string]string{}
	for _, a := range rep.Agents {
		status[a.AgentID] = a.Status
	}
	if status[fast.ID] != "done" {
		t.Fatalf("the finished agent should be done, got %q (%s)", status[fast.ID], out)
	}
	if status[slow.ID] != "running" {
		t.Fatalf("the unfinished agent should still be running, got %q (%s)", status[slow.ID], out)
	}

	slow.Cancel()
	reg.Close()
}

// Activity is the progress hint the UI shows next to a running worker, so it
// has to be the tail of what the worker just said.
func TestActivityReportsTheLatestTail(t *testing.T) {
	reg := NewRegistry()
	h := &Handle{ID: "worker-1", Role: "worker", done: make(chan struct{})}
	if got := reg.liveTail("worker-1"); got != "" {
		t.Fatalf("an unknown agent has no activity, got %q", got)
	}
	reg.mu.Lock()
	reg.agents["worker-1"] = h
	reg.mu.Unlock()

	if got := h.Activity(); got != "" {
		t.Fatalf("a fresh agent has no activity, got %q", got)
	}
	h.setActivity("reading the notes")
	if got := reg.liveTail("worker-1"); got != "reading the notes" {
		t.Fatalf("liveTail=%q", got)
	}
	h.setActivity("writing the summary")
	if got := reg.liveTail("worker-1"); got != "writing the summary" {
		t.Fatalf("activity should be the latest tail, got %q", got)
	}
}

func TestTailHelpers(t *testing.T) {
	// truncation counts runes, so a multi-byte tail is never cut in half
	if got := truncStr("目标目标", 2); got != "目标…" {
		t.Fatalf("truncStr=%q", got)
	}
	if got := truncStr("short", 10); got != "short" {
		t.Fatalf("truncStr should leave short text alone, got %q", got)
	}
	if got := lastLine("first\nsecond"); got != "second" {
		t.Fatalf("lastLine=%q", got)
	}
	if got := lastLine("only"); got != "only" {
		t.Fatalf("lastLine=%q", got)
	}
}

// ---------- resource release: caller failure paths ----------

// 1) Caller ctx canceled (manager session yield) must not kill workers.
// Close still does — otherwise a host that forgot Cleanup would leak them.
func TestCallerContextCancelDoesNotReleaseAgents(t *testing.T) {
	reg := NewRegistry()
	reg.ModelBuilder = func(role, agentID string) model.BaseChatModel {
		return &scriptedModel{turns: workerTurns(nil)}
	}
	callerCtx, cancelCaller := context.WithCancel(context.Background())
	h, err := reg.Spawn(callerCtx, "worker", "long task",
		reg.ModelBuilder, &workTool{d: 30 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	if running, _ := reg.Stats(); running != 1 {
		t.Fatalf("expected 1 running agent, got %d", running)
	}
	cancelCaller()

	select {
	case <-h.Done():
		t.Fatal("cancelling Spawn's context must not kill the worker")
	case <-time.After(150 * time.Millisecond):
	}
	reg.Close()
	select {
	case <-h.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("Close must still stop the worker")
	}
}

// 2) Watchdog: agent that never finishes is force-terminated at AgentTimeout.
func TestWatchdogTimeoutReleasesAgent(t *testing.T) {
	reg := NewRegistry()
	reg.AgentTimeout = 200 * time.Millisecond
	reg.ModelBuilder = func(role, agentID string) model.BaseChatModel {
		return &scriptedModel{turns: workerTurns(nil)}
	}
	h, err := reg.Spawn(context.Background(), "zombie", "never ends",
		reg.ModelBuilder, &workTool{d: 10 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	select {
	case <-h.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("watchdog did not fire")
	}
	if wall := time.Since(start); wall > 1500*time.Millisecond {
		t.Errorf("watchdog fired too late: %v", wall)
	}
	_, err, _ = h.Result()
	if err == nil || !strings.Contains(err.Error(), "timeout") {
		t.Fatalf("expected timeout error, got %v", err)
	}
	// Stats prunes the dead handle
	running, finished := reg.Stats()
	if running != 0 || finished != 1 {
		t.Fatalf("expected pruned registry: running=%d finished=%d", running, finished)
	}
}

// 3) MaxTurns: a model that keeps calling tools ends with eino's
// ErrExceedMaxIterations instead of looping forever.
func TestMaxTurnsEndsBrokenModel(t *testing.T) {
	reg := NewRegistry()
	reg.MaxTurns = 3
	reg.ModelBuilder = func(role, agentID string) model.BaseChatModel {
		// pathological model: always issues a work call, never finishes
		return &scriptedModel{turns: []func(int, []*schema.Message) *schema.Message{
			func(turn int, msgs []*schema.Message) *schema.Message {
				return schema.AssistantMessage("more work", []schema.ToolCall{rawCall("w", "work", "{}")})
			},
		}}
	}
	h, err := reg.Spawn(context.Background(), "looper", "loop forever",
		reg.ModelBuilder, &workTool{d: 10 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-h.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("broken model never terminated")
	}
	_, err, finished := h.Result()
	if !finished || err == nil {
		t.Fatalf("expected finished-with-error (finished=%v err=%v)", finished, err)
	}
}

// 4) Registry.Close cancels everything and rejects new spawns.
func TestRegistryClose(t *testing.T) {
	reg := NewRegistry()
	reg.ModelBuilder = func(role, agentID string) model.BaseChatModel {
		return &scriptedModel{turns: workerTurns(nil)}
	}
	h1, err := reg.Spawn(context.Background(), "a", "t1", reg.ModelBuilder, &workTool{d: 30 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	h2, err := reg.Spawn(context.Background(), "b", "t2", reg.ModelBuilder, &workTool{d: 30 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	reg.Close()
	select {
	case <-h1.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("h1 not cancelled by Close")
	}
	select {
	case <-h2.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("h2 not cancelled by Close")
	}
	if _, err := reg.Spawn(context.Background(), "c", "t3", reg.ModelBuilder); err == nil {
		t.Fatal("spawn after Close should error")
	}
}

// 5) fork_context: spawned agent receives manager history snapshot.
func TestForkContextInheritsHistory(t *testing.T) {
	var mu sync.Mutex
	seenInherited := ""
	reg := NewRegistry()
	reg.SetHistory([]adk.Message{
		schema.UserMessage("the target is example.com"),
		schema.AssistantMessage("understood, target locked", nil),
	})
	reg.ModelBuilder = func(role, agentID string) model.BaseChatModel {
		return &scriptedModel{turns: []func(int, []*schema.Message) *schema.Message{
			func(turn int, msgs []*schema.Message) *schema.Message {
				for _, m := range msgs {
					if strings.Contains(m.Content, "context inherited from manager") {
						mu.Lock()
						seenInherited = m.Content
						mu.Unlock()
					}
				}
				return schema.AssistantMessage("FORK-FINAL", nil)
			},
		}}
	}
	tools := reg.Tools()
	spawnT := invokable(t, tools[0])
	out, err := spawnT.InvokableRun(context.Background(),
		`{"role":"forked-worker","task":"do the thing","fork_context":true}`)
	if err != nil {
		t.Fatalf("spawn forked: %v", out)
	}
	if !strings.Contains(out, `"forked":"true"`) {
		t.Fatalf("expected forked flag: %s", out)
	}
	h, ok := reg.get("forked-worker-1")
	if !ok {
		t.Fatalf("agent not registered: %s", out)
	}
	select {
	case <-h.Done():
	case <-time.After(3 * time.Second):
		t.Fatal("forked agent did not finish")
	}
	mu.Lock()
	defer mu.Unlock()
	if !strings.Contains(seenInherited, "the target is example.com") {
		t.Fatalf("inherited history not delivered: %q", seenInherited)
	}
}

// ManagerConfig must wire tools AND the fork_context middleware so users
// cannot forget Handlers.
func TestManagerConfigWiring(t *testing.T) {
	reg := NewRegistry()
	cfg := reg.ManagerConfig("mgr", "test manager", nil)
	if cfg.Name != "mgr" || cfg.Description != "test manager" {
		t.Fatalf("identity fields not applied: %+v", cfg)
	}
	if len(cfg.ToolsConfig.ToolsNodeConfig.Tools) != 5 {
		t.Fatalf("expected 5 lifecycle tools, got %d", len(cfg.ToolsConfig.ToolsNodeConfig.Tools))
	}
	found := false
	for _, h := range cfg.Handlers {
		if _, ok := h.(*historyRecorder); ok {
			found = true
		}
	}
	if !found {
		t.Fatal("fork_context history middleware missing from Handlers")
	}

	// options: applied left-to-right, last wins; extra tools/handlers append.
	mgrTool := &workTool{d: time.Millisecond}
	extraHandler := &historyRecorder{reg: reg}
	cfg2 := reg.ManagerConfig("m2", "d", nil,
		WithInstruction("custom prompt"),
		WithMaxIterations(40),
		WithManagerTool(mgrTool),
		WithManagerHandler(extraHandler),
	)
	if cfg2.Instruction != "custom prompt" || cfg2.MaxIterations != 40 {
		t.Fatalf("options not applied: instruction=%q iters=%d", cfg2.Instruction, cfg2.MaxIterations)
	}
	if len(cfg2.ToolsConfig.ToolsNodeConfig.Tools) != 6 {
		t.Fatalf("expected 6 tools (5+1), got %d", len(cfg2.ToolsConfig.ToolsNodeConfig.Tools))
	}
	if len(cfg2.Handlers) != 2 {
		t.Fatalf("expected 2 handlers, got %d", len(cfg2.Handlers))
	}
	_ = extraHandler

	// end-to-end: manager built from ManagerConfig records history for
	// fork_context without the caller touching Handlers.
	var mu sync.Mutex
	seen := ""
	reg.SetHistory([]adk.Message{schema.UserMessage("target is example.com")})
	reg.ModelBuilder = func(role, agentID string) model.BaseChatModel {
		return &scriptedModel{turns: []func(int, []*schema.Message) *schema.Message{
			func(turn int, msgs []*schema.Message) *schema.Message {
				for _, m := range msgs {
					if strings.Contains(m.Content, "target is example.com") {
						mu.Lock()
						seen = m.Content
						mu.Unlock()
					}
				}
				return schema.AssistantMessage("FORK-FINAL", nil)
			},
		}}
	}
	// simulate what ManagerMiddleware records on the manager side each turn
	rec := reg.ManagerMiddleware().(*historyRecorder)
	st := &adk.ChatModelAgentState{Messages: []adk.Message{
		schema.UserMessage("target is example.com"),
		schema.AssistantMessage("noted", nil),
		schema.UserMessage("second turn fact"),
	}}
	if _, _, err := rec.BeforeModelRewriteState(context.Background(), st, nil); err != nil {
		t.Fatal(err)
	}
	h, err := reg.SpawnForked(context.Background(), "w", "do",
		reg.ModelBuilder, reg.historySnapshot(), &workTool{d: 10 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-h.Done():
	case <-time.After(3 * time.Second):
		t.Fatal("forked agent did not finish")
	}
	mu.Lock()
	defer mu.Unlock()
	if !strings.Contains(seen, "second turn fact") {
		t.Fatalf("inherited history not delivered to forked agent: %q", seen)
	}
}

// Run must be the entire integration surface: callback receives worker
// messages + final, no explicit Runner/event-loop in user code.
func TestRunCallbackSurface(t *testing.T) {
	var mu sync.Mutex
	notes := []Notification{}
	reg := NewRegistry()
	reg.ModelBuilder = func(role, agentID string) model.BaseChatModel {
		if role == "manager" {
			return &scriptedModel{turns: []func(int, []*schema.Message) *schema.Message{
				func(_ int, msgs []*schema.Message) *schema.Message {
					return schema.AssistantMessage("MANAGER-SAYS-HI", nil) // no spawn needed
				},
			}}
		}
		return &scriptedModel{turns: workerTurns(nil)}
	}
	final, err := reg.Run(context.Background(), "say hi", func(n Notification) {
		mu.Lock()
		defer mu.Unlock()
		notes = append(notes, n)
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	t.Logf("final=%q notes=%d", final, len(notes))
	for _, n := range notes {
		t.Logf("  kind=%v agent=%s text=%.40q", n.Kind, n.AgentID, n.Text)
	}
	if final != "MANAGER-SAYS-HI" {
		t.Fatalf("final=%q", final)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(notes) == 0 || notes[len(notes)-1].Kind != NotifyDone {
		t.Fatalf("missing NotifyDone: %+v", notes)
	}
	// streaming order: the turn boundary opens the turn, then accumulated
	// delta(s), then the sealed agent_message, then exactly one done.
	if notes[0].Kind != NotifyTurn || notes[0].AgentID != DefaultManagerID {
		t.Fatalf("first notification should be a manager turn: %+v", notes[0])
	}
	if notes[1].Kind != NotifyDelta || notes[1].AgentID != DefaultManagerID {
		t.Fatalf("second notification should be a manager delta: %+v", notes[1])
	}
	var sawMsg, dones int
	for _, n := range notes {
		if n.Kind == NotifyAgentMessage && n.AgentID == "manager" && n.Text == "MANAGER-SAYS-HI" {
			sawMsg++
		}
		if n.Kind == NotifyDone {
			dones++
		}
	}
	if sawMsg != 1 {
		t.Fatalf("want exactly one final agent_message, got %d: %+v", sawMsg, notes)
	}
	if dones != 1 {
		t.Fatalf("want exactly one NotifyDone, got %d: %+v", dones, notes)
	}
}

func TestWorkerSurvivesParentContextCancel(t *testing.T) {
	reg := NewRegistry()
	parent, cancel := context.WithCancel(context.Background())
	h, err := reg.Spawn(parent, "slow", "long work",
		func(role, id string) model.BaseChatModel {
			return &scriptedModel{turns: []func(int, []*schema.Message) *schema.Message{
				func(turn int, msgs []*schema.Message) *schema.Message {
					return schema.AssistantMessage("starting", []schema.ToolCall{rawCall("w1", "work", "{}")})
				},
			}}
		}, &workTool{d: 2 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(80 * time.Millisecond)
	cancel()
	select {
	case <-h.Done():
		t.Fatal("cancelling the spawn context must not kill the worker")
	case <-time.After(150 * time.Millisecond):
	}
	h.Cancel()
	select {
	case <-h.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("Handle.Cancel must still stop the worker")
	}
}

func TestSetHistoryKeepsDroppedToolResults(t *testing.T) {
	reg := NewRegistry()
	reg.appendHistoryToolResult(&adk.ToolContext{Name: "wait_agents", CallID: "wait-1"}, `{"agents":[]}`, nil)
	reg.SetHistory([]adk.Message{
		schema.UserMessage("start the work"),
		schema.AssistantMessage("waiting", nil),
	})
	got := reg.historySnapshot()
	if len(got) != 3 || got[2].Role != schema.Tool || got[2].ToolCallID != "wait-1" {
		t.Fatalf("a later snapshot must not drop the wrap-appended result: %+v", got)
	}
	reg.SetHistory([]adk.Message{
		schema.UserMessage("start the work"),
		schema.ToolMessage(`{"agents":[{"status":"done"}]}`, "wait-1"),
	})
	got = reg.historySnapshot()
	if len(got) != 2 || got[1].Content != `{"agents":[{"status":"done"}]}` {
		t.Fatalf("an already-present result must not duplicate: %+v", got)
	}
}

func TestAppendHistoryToolResult(t *testing.T) {
	reg := NewRegistry()
	reg.appendHistoryToolResult(nil, "x", nil)
	reg.appendHistoryToolResult(&adk.ToolContext{Name: "work", CallID: ""}, "x", nil)
	if len(reg.historySnapshot()) != 0 {
		t.Fatal("nil or empty call id must not invent a tool result")
	}
	tc := &adk.ToolContext{Name: "work", CallID: "tc-1"}
	reg.appendHistoryToolResult(tc, "done", nil)
	reg.appendHistoryToolResult(tc, "again", nil)
	got := reg.historySnapshot()
	if len(got) != 1 || got[0].Role != schema.Tool || got[0].Content != "done" || got[0].ToolCallID != "tc-1" {
		t.Fatalf("want one result, got %+v", got)
	}
	reg.appendHistoryToolResult(&adk.ToolContext{Name: "work", CallID: "tc-2"}, "", fmt.Errorf("tool refused"))
	got = reg.historySnapshot()
	if len(got) != 2 || got[1].Content != "tool refused" {
		t.Fatalf("an empty error result must keep the error text: %+v", got)
	}
}

func TestWrapStreamableToolCallRecordsTheResult(t *testing.T) {
	reg := NewRegistry()
	mw := &historyRecorder{reg: reg}
	wrapped, err := mw.WrapStreamableToolCall(context.Background(),
		func(context.Context, string, ...tool.Option) (*schema.StreamReader[string], error) {
			return schema.StreamReaderFromArray([]string{"hel", "lo"}), nil
		}, &adk.ToolContext{Name: "work", CallID: "tc-s"})
	if err != nil {
		t.Fatal(err)
	}
	sr, err := wrapped(context.Background(), "{}")
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	for {
		chunk, recvErr := sr.Recv()
		if recvErr != nil {
			break
		}
		b.WriteString(chunk)
	}
	if b.String() != "hello" {
		t.Fatalf("replayed stream=%q", b.String())
	}
	got := reg.historySnapshot()
	if len(got) != 1 || got[0].Content != "hello" || got[0].ToolCallID != "tc-s" {
		t.Fatalf("missing streamed tool result: %+v", got)
	}
	fail, err := mw.WrapStreamableToolCall(context.Background(),
		func(context.Context, string, ...tool.Option) (*schema.StreamReader[string], error) {
			return nil, fmt.Errorf("no stream")
		}, &adk.ToolContext{Name: "work", CallID: "tc-fail"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fail(context.Background(), "{}"); err == nil {
		t.Fatal("want the endpoint error")
	}
}
