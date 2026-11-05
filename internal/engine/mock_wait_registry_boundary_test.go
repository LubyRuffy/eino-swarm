package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/provider"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

// This gate delays one actual worker model; it does not replace its responses,
// tools, Registry lifecycle, or manager policy.
type boundaryGatedModel struct {
	model.BaseChatModel
	gate <-chan struct{}
}

func (m *boundaryGatedModel) Generate(ctx context.Context, in []*schema.Message, opts ...model.Option) (*schema.Message, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-m.gate:
	}
	return m.BaseChatModel.Generate(ctx, in, opts...)
}
func (m *boundaryGatedModel) Stream(ctx context.Context, in []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-m.gate:
	}
	return m.BaseChatModel.Stream(ctx, in, opts...)
}

// Partial waits must not spend the manager budget polling collected workers,
// and a later subset result must not erase an earlier independent result.
func TestOfflineManagerCollectsRealRegistryPartialResults(t *testing.T) {
	for _, mode := range []string{"mixed-done-running", "terminal-subset"} {
		t.Run(mode, func(t *testing.T) {
			e := newTestEngine(t)
			th, err := e.CreateThread("offline partial collection", "", "")
			if err != nil {
				t.Fatal(err)
			}
			set := buildTestToolset(t, e, th.ID)
			ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
			defer cancel()
			pool := provider.NewMockWithStreamDelay(e.Config(), 0)
			base, err := pool.ModelBuilder(ctx, th.ProviderID, "", "", nil)
			if err != nil {
				t.Fatal(err)
			}
			gate := make(chan struct{})
			var gateOnce sync.Once
			release := func() { gateOnce.Do(func() { close(gate) }) }
			defer release()
			if mode == "terminal-subset" {
				release()
			}
			builder := func(role, id string) model.BaseChatModel {
				m := base(role, id)
				if role == "reviewer" {
					return &boundaryGatedModel{m, gate}
				}
				return m
			}
			turn := mustStoredTurn(t, e, th.ID, store.Turn{})
			reg := e.newTurnRegistry(builder, set, nil, turn)
			defer reg.Close()
			ctx, releaseOwner := ownManagerTestFixture(ctx, e.runtimeFor(th.ID), turn.ID, reg)
			defer releaseOwner()
			invoke := func(name, args string) string {
				t.Helper()
				for _, tl := range reg.Tools() {
					info, er := tl.Info(ctx)
					if er != nil {
						t.Fatal(er)
					}
					if info.Name == name {
						out, er := tl.(tool.InvokableTool).InvokableRun(ctx, args)
						if er != nil {
							t.Fatal(er)
						}
						return out
					}
				}
				t.Fatalf("missing actual Registry tool %s", name)
				return ""
			}
			task := "Coordinate the independent work and report all results."
			var msgs []adk.Message
			msgs = append(msgs, schema.UserMessage(task))
			ids := make([]string, 0, 2)
			spawns := make([]string, 0, 2)
			for i, role := range []string{"researcher", "reviewer"} {
				args, _ := json.Marshal(map[string]any{"role": role, "task": "Inspect the assigned material and report findings."})
				receipt := invoke("spawn_agent", string(args))
				spawns = append(spawns, receipt)
				var result struct {
					AgentID string `json:"agent_id"`
				}
				if json.Unmarshal([]byte(receipt), &result) != nil || result.AgentID == "" {
					t.Fatalf("invalid real spawn receipt %s", receipt)
				}
				ids = append(ids, result.AgentID)
				callID := fmt.Sprintf("seed-spawn-%d", i)
				msgs = append(msgs, schema.AssistantMessage("", []schema.ToolCall{{ID: callID, Type: "function", Function: schema.FunctionCall{Name: "spawn_agent", Arguments: string(args)}}}), schema.ToolMessage(receipt, callID))
			}
			// Observe actual worker completion without consuming its result.
			awaitDone := func(wanted []string) {
				t.Helper()
				tick := time.NewTicker(time.Millisecond)
				defer tick.Stop()
				for {
					all := true
					pending := reg.PendingAgents()
					for _, id := range wanted {
						found := false
						for _, p := range pending {
							if p.AgentID == id {
								found = true
								if p.Running {
									all = false
								}
							}
						}
						if !found {
							t.Fatalf("worker disappeared before collection: %s", id)
						}
					}
					if all {
						return
					}
					select {
					case <-ctx.Done():
						t.Fatal(ctx.Err())
					case <-tick.C:
					}
				}
			}
			awaitDone(ids[:1])
			if mode == "terminal-subset" {
				awaitDone(ids)
			}
			targets := ids
			if mode == "terminal-subset" {
				targets = ids[:1]
			}
			args, _ := json.Marshal(map[string]any{"agent_ids": targets, "timeout_s": 60})
			first := invoke("wait_agents", string(args))
			var firstReport struct {
				Agents []struct {
					AgentID string `json:"agent_id"`
					Status  string `json:"status"`
					Result  string `json:"result"`
				}
			}
			if err := json.Unmarshal([]byte(first), &firstReport); err != nil {
				t.Fatal(err)
			}
			if len(firstReport.Agents) != len(targets) || firstReport.Agents[0].Status != "done" {
				t.Fatalf("first actual wait was not expected terminal collection: %s", first)
			}
			if mode == "mixed-done-running" && (len(firstReport.Agents) != 2 || firstReport.Agents[1].Status != "running") {
				t.Fatalf("slow worker was not running: %s", first)
			}
			msgs = append(msgs, schema.AssistantMessage("", []schema.ToolCall{{ID: "seed-wait", Type: "function", Function: schema.FunctionCall{Name: "wait_agents", Arguments: string(args)}}}), schema.ToolMessage(first, "seed-wait"))
			type observation struct {
				Kind, CallID, Text string
				BeforeRelease      bool
			}
			var mu sync.Mutex
			var notes []observation
			var schedule sync.Once
			var timerDone chan struct{}
			if mode == "mixed-done-running" {
				timerDone = make(chan struct{})
			}
			cb := func(n swarm.Notification) {
				before := false
				select {
				case <-gate:
				default:
					before = true
				}
				if n.AgentID == swarm.DefaultManagerID && (n.Kind == swarm.NotifyToolCall || n.Kind == swarm.NotifyToolResult) {
					mu.Lock()
					notes = append(notes, observation{n.Kind.String(), n.ToolCallID, n.Text, before})
					mu.Unlock()
				}
				if mode == "mixed-done-running" && n.AgentID == swarm.DefaultManagerID && n.Kind == swarm.NotifyToolCall && strings.HasPrefix(n.Text, "wait_agents(") {
					schedule.Do(func() {
						go func() {
							defer close(timerDone)
							timer := time.NewTimer(150 * time.Millisecond)
							defer timer.Stop()
							select {
							case <-ctx.Done():
							case <-timer.C:
							}
							release()
						}()
					})
				}
			}
			res, runErr := reg.RunWith(ctx, swarm.RunConfig{Instruction: ManagerPrompt(set, ""), Messages: msgs, MaxIterations: e.Config().SwarmSnapshot().ManagerIterations()}, cb)
			release()
			// Join the timer actor even if the manager reached its real iteration guard.
			if mode == "mixed-done-running" {
				schedule.Do(func() { close(timerDone) })
				<-timerDone
			}
			// Both actual workers must finish normally before observing final obligation state.
			deadline := time.NewTicker(time.Millisecond)
			defer deadline.Stop()
			for {
				running := false
				for _, p := range reg.PendingAgents() {
					running = running || p.Running
				}
				if !running {
					break
				}
				select {
				case <-ctx.Done():
					t.Fatal(ctx.Err())
				case <-deadline.C:
				}
			}
			mu.Lock()
			captured := append([]observation(nil), notes...)
			mu.Unlock()
			pending := reg.PendingAgents()
			if runErr != nil {
				t.Errorf("actual manager did not finish: %v", runErr)
			}
			calls := 0
			earlyWaitReceipts := 0
			for _, n := range captured {
				if n.Kind == swarm.NotifyToolCall.String() && strings.HasPrefix(n.Text, "wait_agents(") {
					calls++
					if strings.Contains(n.Text, ids[0]) {
						t.Errorf("collected worker was selected again by actual ADK manager: %s", n.Text)
					}
				}
				if n.Kind == swarm.NotifyToolResult.String() && n.BeforeRelease && strings.Contains(n.Text, "\"agents\"") {
					earlyWaitReceipts++
				}
			}
			t.Logf("real spawn receipts=%v initial wait=%s wait_calls=%d early_wait_results=%d pending=%v", spawns, first, calls, earlyWaitReceipts, pending)
			if calls == 0 {
				t.Error("manager finalized without collecting the other real worker")
			}
			if mode == "mixed-done-running" && earlyWaitReceipts > 0 {
				t.Errorf("actual mixed wait returned %d times before pending worker was released", earlyWaitReceipts)
			}
			if len(pending) != 0 {
				t.Errorf("manager returned with uncollected real results: %+v", pending)
			}
			for _, role := range []string{"researcher", "reviewer"} {
				if !strings.Contains(res.Final, role+" finished") {
					t.Errorf("final lost %s result: %s", role, res.Final)
				}
			}
		})
	}
}
