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

	start := time.Now()
	res, err := waitT.InvokableRun(context.Background(),
		fmt.Sprintf(`{"agent_ids":[%q,%q],"timeout_s":5}`, ids[0], ids[1]))
	wall := time.Since(start)
	if err != nil {
		t.Fatalf("wait_agents: %v", err)
	}
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

	if _, err := closeT.InvokableRun(context.Background(), `{"agent_id":"nope"}`); err == nil {
		t.Fatal("close unknown agent should error")
	}

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

// ---------- resource release: caller failure paths ----------

// 1) Caller ctx canceled (model call failed / SIGINT) -> agents die with it.
func TestCallerContextCancelReleasesAgents(t *testing.T) {
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
	cancelCaller() // host dies without close_agent

	select {
	case <-h.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("agent not released after caller context cancel")
	}
	_, err, finished := h.Result()
	if !finished || err == nil {
		t.Fatalf("expected finished-with-error after caller cancel (finished=%v err=%v)", finished, err)
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
	if len(cfg.ToolsConfig.ToolsNodeConfig.Tools) != 4 {
		t.Fatalf("expected 4 lifecycle tools, got %d", len(cfg.ToolsConfig.ToolsNodeConfig.Tools))
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
	if len(cfg2.ToolsConfig.ToolsNodeConfig.Tools) != 5 {
		t.Fatalf("expected 5 tools (4+1), got %d", len(cfg2.ToolsConfig.ToolsNodeConfig.Tools))
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
	// streaming order: delta(s) first (accumulated text), then the final
	// agent_message, then done.
	if notes[0].Kind != NotifyDelta || notes[0].AgentID != DefaultManagerID {
		t.Fatalf("first notification should be a manager delta: %+v", notes[0])
	}
	var sawMsg bool
	for _, n := range notes {
		if n.Kind == NotifyAgentMessage && n.AgentID == "manager" && n.Text == "MANAGER-SAYS-HI" {
			sawMsg = true
		}
	}
	if !sawMsg {
		t.Fatalf("missing final agent_message: %+v", notes)
	}
}
