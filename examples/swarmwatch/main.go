// Command swarmwatch demonstrates observing parallel sub-agent execution from
// the command line: a manager spawns four workers in one turn, each worker
// "works" for a different duration, and every lifecycle event is printed with
// a wall-clock timestamp. Concurrency is visible as overlapping time windows.
//
// go run ./examples/swarmwatch
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/LubyRuffy/eino-swarm"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

var (
	printMu sync.Mutex
	t0      = time.Now()
)

func logf(format string, args ...any) {
	printMu.Lock()
	defer printMu.Unlock()
	fmt.Printf("[%7.2fs] %s\n", time.Since(t0).Seconds(), fmt.Sprintf(format, args...))
}

// workTool simulates real work; records its exact [start,end] window.
type workTool struct {
	mu      sync.Mutex
	windows [][2]float64
}

func (t *workTool) Info(ctx context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{Name: "work", Desc: "simulate work for the agent's assigned duration; pass {\"seconds\": N}",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"seconds": {Type: schema.Number, Required: true, Desc: "how long to work"},
		})}, nil
}

func (t *workTool) InvokableRun(ctx context.Context, args string, _ ...tool.Option) (string, error) {
	var a struct {
		Seconds float64 `json:"seconds"`
	}
	_ = json.Unmarshal([]byte(args), &a)
	if a.Seconds <= 0 {
		a.Seconds = 1
	}
	start := time.Since(t0)
	select {
	case <-time.After(time.Duration(a.Seconds * float64(time.Second))):
	case <-ctx.Done():
		return "", ctx.Err()
	}
	end := time.Since(t0)
	t.mu.Lock()
	t.windows = append(t.windows, [2]float64{start.Seconds(), end.Seconds()})
	t.mu.Unlock()
	logf("      work window: %.2fs → %.2fs", start.Seconds(), end.Seconds())
	return "work done", nil
}

// worker model: call work with its assigned duration, then finish.
type workerModel struct {
	role string
	turn int
}

func (m *workerModel) Generate(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.Message, error) {
	m.turn++
	if m.turn == 1 {
		d := roleDurations[m.role].Seconds()
		return schema.AssistantMessage("starting work", []schema.ToolCall{
			{ID: "w1", Type: "function", Function: schema.FunctionCall{
				Name: "work", Arguments: fmt.Sprintf(`{"seconds":%.1f}`, d)}},
		}), nil
	}
	return schema.AssistantMessage("worker result ready", nil), nil
}

func (m *workerModel) Stream(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	return nil, fmt.Errorf("stream unused")
}

// manager model: one turn spawns 4 workers in parallel, then waits, then final.
type managerModel struct {
	turn int
}

var roleDurations = map[string]time.Duration{
	"scout":  1 * time.Second,
	"miner":  2 * time.Second,
	"auditor": 3 * time.Second,
	"writer": 4 * time.Second,
}

func (m *managerModel) Generate(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.Message, error) {
	m.turn++
	switch m.turn {
	case 1:
		logf("manager: spawning 4 workers in ONE message (parallel spawn)")
		i := 0
		calls := make([]schema.ToolCall, 0, 4)
		for role := range roleDurations {
			i++
			calls = append(calls, schema.ToolCall{
				ID: fmt.Sprintf("s%d", i), Type: "function",
				Function: schema.FunctionCall{
					Name: "spawn_agent",
					Arguments: fmt.Sprintf(`{"role":%q,"task":"work for a while"}`, role),
				},
			})
		}
		return schema.AssistantMessage("spawning", calls), nil
	case 2:
		var ids []string
		for _, msg := range input {
			if msg.Role == schema.Tool {
				var r struct {
					AgentID string `json:"agent_id"`
				}
				if err := json.Unmarshal([]byte(msg.Content), &r); err == nil && r.AgentID != "" {
					ids = append(ids, r.AgentID)
				}
			}
		}
		b, _ := json.Marshal(map[string]any{"agent_ids": ids, "timeout_s": 30})
		logf("manager: wait_agents(%v)", ids)
		return schema.AssistantMessage("waiting", []schema.ToolCall{
			{ID: "w", Type: "function", Function: schema.FunctionCall{Name: "wait_agents", Arguments: string(b)}},
		}), nil
	default:
		return schema.AssistantMessage("all workers done — swarm finished", nil), nil
	}
}

func (m *managerModel) Stream(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	return nil, fmt.Errorf("stream unused")
}

func main() {
	t0 = time.Now()
	reg := swarm.NewRegistry()
	reg.MaxConcurrent = 8

	work := &workTool{}
	reg.SubAgentTools = []tool.BaseTool{work}
	reg.ModelBuilder = func(role, agentID string) model.BaseChatModel {
		if role == "manager" {
			return &managerModel{}
		}
		return &workerModel{role: role}
	}

	fmt.Println("expect: 4 'spawned' lines back-to-back, overlapping work windows, max_concurrency=4")
	final, err := reg.Run(context.Background(), "fan out to four workers", func(n swarm.Notification) {
		switch n.Kind {
		case swarm.NotifySpawned:
			logf("spawned  %-12s", n.AgentID)
		case swarm.NotifyFinished:
			if n.Err != nil {
				logf("finished %-12s err=%v", n.AgentID, n.Err)
			} else {
				logf("finished %-12s", n.AgentID)
			}
		case swarm.NotifyAgentMessage:
			if n.AgentID == swarm.DefaultManagerID {
				logf("manager says: %s", n.Text)
			}
		case swarm.NotifyDone:
			logf("DONE: %s", n.Text)
		case swarm.NotifyError:
			logf("ERROR: %v", n.Err)
		}
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	_ = final

	// verdict: overlap analysis of work windows
	windows := work.windows
	if len(windows) < 2 {
		return
	}
	var maxConc int
	events := make([]float64, 0, 2*len(windows))
	for _, w := range windows {
		events = append(events, w[0], w[1])
	}
	for _, s := range windows {
		c := 0
		for _, w := range windows {
			if w[0] <= s[0] && s[0] < w[1] {
				c++
			}
		}
		if c > maxConc {
			maxConc = c
		}
	}
	fmt.Printf("\n%d workers, max observed concurrency = %d → %s\n",
		len(windows), maxConc, map[bool]string{true: "PARALLEL ✔", false: "sequential ✘"}[maxConc > 1])
	var _ = adk.NewRunner
}
