// Command codex-lite is the minimal Codex-CLI-style multi-agent runner built
// on github.com/LubyRuffy/eino-swarm: a manager LLM holding four lifecycle
// tools (spawn_agent / send_message / wait_agents / close_agent), spawning
// sub-agents on demand — no pre-registered roles, no extra infra.
//
//	Live:  OPENAI_BASE_URL=http://your-endpoint/v1 OPENAI_MODEL=your-model \
//	       OPENAI_API_KEY=sk-... go run ./examples/codex-lite -task "..."
//
// Demo:  go run ./examples/codex-lite -demo   (scripted models, no network)
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/LubyRuffy/eino-swarm"
	openaimodel "github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

const managerPrompt = `You are the manager of a worker swarm.
Tools: spawn_agent(role, task[, fork_context]) starts a background worker and
returns agent_id; send_message(agent_id, text) steers a running worker at its
next turn boundary; wait_agents(agent_ids, timeout_s) blocks for results;
close_agent(agent_id) cancels one.
Spawn independent work in ONE message (multiple spawn_agent calls run in
parallel). Use fork_context:true when a worker needs this conversation so far.
Always wait_agents before answering. Stop when the goal is met.`

func main() {
	demo := flag.Bool("demo", false, "scripted models, no network")
	task := flag.String("task", "Summarize README.md and list its open questions.", "goal for the swarm")
	flag.Parse()
	ctx := context.Background()

	// 1) one ModelBuilder for the whole swarm: same shared client everywhere.
	buildModel := buildLive
	if *demo {
		buildModel = buildDemo
	}

	// 2) the registry IS the runtime: capabilities, not pre-registered agents.
	reg := swarm.NewRegistry()
	reg.MaxConcurrent = 4
	reg.AgentTimeout = 5 * time.Minute
	reg.MaxTurns = 20
	reg.ModelBuilder = buildModel
	if *demo {
		reg.SubAgentTools = []tool.BaseTool{&demoWorkTool{d: 300 * time.Millisecond}}
	}
	reg.OnEvent = func(role, agentID string, ev *adk.AgentEvent) { // progress line
		if ev.Output != nil && ev.Output.MessageOutput != nil &&
			ev.Output.MessageOutput.Message != nil &&
			ev.Output.MessageOutput.Message.Role == schema.Assistant {
			if c := strings.TrimSpace(ev.Output.MessageOutput.Message.Content); c != "" {
				fmt.Printf("[%s] %s\n", agentID, oneLine(c))
			}
		}
	}
	defer reg.Close() // any leftover agents are cancelled on exit

	// 3) manager = one config call: tools + fork_context wiring included,
	//    extra knobs as functional options (last wins).
	manager, err := adk.NewChatModelAgent(ctx, reg.ManagerConfig(
		"manager", "Codex-lite manager", buildModel("manager", "manager"),
		swarm.WithInstruction(managerPrompt),
	))
	if err != nil {
		exit(err)
	}

	// 4) run. Ctrl+C / ctx cancel propagates to every spawned agent.
	runner := adk.NewRunner(ctx, adk.RunnerConfig{Agent: manager})
	iter := runner.Run(ctx, []adk.Message{schema.UserMessage(*task)})
	final := ""
	for {
		ev, ok := iter.Next()
		if !ok {
			break
		}
		if ev == nil {
			continue
		}
		if ev.Err != nil {
			reg.Close()
			exit(ev.Err)
		}
		if ev.Output != nil && ev.Output.MessageOutput != nil &&
			!ev.Output.MessageOutput.IsStreaming && ev.Output.MessageOutput.Message != nil &&
			ev.Output.MessageOutput.Message.Role == schema.Assistant {
			final = ev.Output.MessageOutput.Message.Content
		}
	}
	fmt.Println("\nFINAL:", final)
}

func mustOpenAI(ctx context.Context) model.BaseChatModel {
	base := os.Getenv("OPENAI_BASE_URL")
	key := os.Getenv("OPENAI_API_KEY")
	id := os.Getenv("OPENAI_MODEL")
	if base == "" || key == "" || id == "" {
		fmt.Fprintln(os.Stderr, "set OPENAI_BASE_URL, OPENAI_MODEL, OPENAI_API_KEY (or use -demo)")
		os.Exit(2)
	}
	m, err := openaimodel.NewChatModel(ctx, &openaimodel.ChatModelConfig{
		BaseURL: base, APIKey: key, Model: id, Timeout: 2 * time.Minute,
	})
	if err != nil {
		panic(err)
	}
	return m
}

func buildLive(role, agentID string) model.BaseChatModel {
	return mustOpenAI(context.Background())
}

func oneLine(s string) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) > 120 {
		return s[:120] + "…"
	}
	return s
}

func exit(err error) { fmt.Fprintln(os.Stderr, "error:", err); os.Exit(1) }

// buildDemo: every role gets a scripted model so -demo runs with no network.
// Sub-agents do one turn of "work" (a sleep via the work tool) then finish.
func buildDemo(role, agentID string) model.BaseChatModel {
	if role == "manager" {
		// manager script: spawn two workers in one turn (parallel), wait, finalize.
		spawned := false
		waited := false
		return &scriptedLLM{fn: func(turn int, msgs []*schema.Message) *schema.Message {
			if !spawned {
				spawned = true
				return schema.AssistantMessage("spawning workers", []schema.ToolCall{
					{ID: "s1", Type: "function", Function: schema.FunctionCall{Name: "spawn_agent", Arguments: `{"role":"reader","task":"summarize the doc"}`}},
					{ID: "s2", Type: "function", Function: schema.FunctionCall{Name: "spawn_agent", Arguments: `{"role":"critic","task":"list open questions","fork_context":true}`}},
				})
			}
			if !waited {
				// harvest agent ids from tool messages
				var ids []string
				for _, m := range msgs {
					if m.Role == schema.Tool && strings.Contains(m.Content, "agent_id") {
						var r struct {
							AgentID string `json:"agent_id"`
						}
						if json.Unmarshal([]byte(m.Content), &r) == nil && r.AgentID != "" {
							ids = append(ids, r.AgentID)
						}
					}
				}
				if len(ids) >= 2 {
					waited = true
					args, _ := json.Marshal(map[string]any{"agent_ids": ids, "timeout_s": 10})
					return schema.AssistantMessage("waiting", []schema.ToolCall{
						{ID: "s3", Type: "function", Function: schema.FunctionCall{Name: "wait_agents", Arguments: string(args)}},
					})
				}
				return schema.AssistantMessage("FINAL-DEMO", nil)
			}
			return schema.AssistantMessage("FINAL-DEMO", nil)
		}}
	}
	// worker script: call work tool once, then answer.
	called := false
	return &scriptedLLM{fn: func(turn int, msgs []*schema.Message) *schema.Message {
		if !called {
			called = true
			return schema.AssistantMessage("working", []schema.ToolCall{
				{ID: "w1", Type: "function", Function: schema.FunctionCall{Name: "work", Arguments: "{}"}},
			})
		}
		for _, m := range msgs {
			if strings.Contains(m.Content, "inherited") {
				return schema.AssistantMessage(role+"(fork): "+oneLine(m.Content), nil)
			}
		}
		return schema.AssistantMessage(role+": done", nil)
	}}
}

// demoWorkTool simulates worker work with a sleep.
type demoWorkTool struct{ d time.Duration }

func (t *demoWorkTool) Info(ctx context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{Name: "work", Desc: "simulate work",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{})}, nil
}

func (t *demoWorkTool) InvokableRun(ctx context.Context, _ string, _ ...tool.Option) (string, error) {
	select {
	case <-time.After(t.d):
		return "work done", nil
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

// scriptedLLM adapts a per-turn function to model.BaseChatModel.
type scriptedLLM struct {
	fn   func(turn int, msgs []*schema.Message) *schema.Message
	turn int
}

func (m *scriptedLLM) Generate(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.Message, error) {
	m.turn++
	return m.fn(m.turn-1, input), nil
}

func (m *scriptedLLM) Stream(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	return nil, errors.New("demo only supports Generate")
}
