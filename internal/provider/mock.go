package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

// The mock provider exists so the product can be developed, demoed and tested
// without an endpoint. It is a real swarm run — the manager genuinely spawns
// workers, the workers genuinely call tools and write files into the
// workspace — only the token generation is scripted.
//
// Its text is deliberately generic and derived from whatever the user asked:
// nothing here may encode a particular example task, because that text would
// leak into screenshots, tests and expectations as if it were product
// behavior.

// mockChunkDelay paces the scripted stream so the UI's streaming, spinners and
// progressive rendering are actually exercised rather than completing in one
// frame.
const mockChunkDelay = 18 * time.Millisecond

// newMockModel returns a scripted model for one agent. The manager script
// fans work out and collects it; every other role is a worker that does one
// tool call and reports back.
func newMockModel(role string) model.BaseChatModel {
	if role == managerRole || role == "" {
		return &mockModel{script: managerScript}
	}
	return &mockModel{script: workerScript(role)}
}

const managerRole = "manager"

// mockScript maps a turn number and the conversation so far to the message the
// model "generates".
type mockScript func(turn int, msgs []*schema.Message) *schema.Message

type mockModel struct {
	script mockScript

	mu   sync.Mutex
	turn int
}

func (m *mockModel) nextTurn() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.turn++
	return m.turn
}

func (m *mockModel) Generate(ctx context.Context, in []*schema.Message, _ ...model.Option) (*schema.Message, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return m.script(m.nextTurn(), in), nil
}

func (m *mockModel) Stream(ctx context.Context, in []*schema.Message, _ ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	out := m.script(m.nextTurn(), in)
	sr, sw := schema.Pipe[*schema.Message](8)
	go func() {
		defer sw.Close()
		send := func(msg *schema.Message) bool {
			select {
			case <-ctx.Done():
				return false
			case <-time.After(mockChunkDelay):
			}
			return !sw.Send(msg, nil)
		}
		for _, part := range splitWords(out.ReasoningContent) {
			if !send(&schema.Message{Role: schema.Assistant, ReasoningContent: part}) {
				return
			}
		}
		for _, part := range splitWords(out.Content) {
			if !send(&schema.Message{Role: schema.Assistant, Content: part}) {
				return
			}
		}
		if len(out.ToolCalls) > 0 {
			send(&schema.Message{Role: schema.Assistant, ToolCalls: out.ToolCalls})
		}
	}()
	return sr, nil
}

// splitWords cuts text into small chunks that keep their trailing space, so
// reassembling them reproduces the original exactly — the property the UI's
// accumulated deltas rely on.
func splitWords(s string) []string {
	if s == "" {
		return nil
	}
	var out []string
	cur := strings.Builder{}
	for _, r := range s {
		cur.WriteRune(r)
		if r == ' ' || r == '\n' {
			out = append(out, cur.String())
			cur.Reset()
		}
	}
	if cur.Len() > 0 {
		out = append(out, cur.String())
	}
	return out
}

// ---------- manager script ----------

// managerScript runs the same shape a real manager does: think, fan out two
// workers in one message (one of them inheriting the conversation), wait for
// them, then answer in markdown quoting what came back.
func managerScript(turn int, msgs []*schema.Message) *schema.Message {
	task := lastUserText(msgs)
	switch turn {
	case 1:
		plan, _ := json.Marshal(map[string]any{
			"role": "researcher",
			"task": "Collect what is needed for: " + task,
		})
		review, _ := json.Marshal(map[string]any{
			"role":         "reviewer",
			"task":         "Review the collected material for: " + task,
			"fork_context": true,
		})
		return &schema.Message{
			Role:             schema.Assistant,
			ReasoningContent: "This needs two independent passes, so I will run them in parallel rather than one after the other.\n",
			Content:          "Splitting this into a research pass and a review pass.\n",
			ToolCalls: []schema.ToolCall{
				call("mock-spawn-1", "spawn_agent", string(plan)),
				call("mock-spawn-2", "spawn_agent", string(review)),
			},
		}
	default:
		ids := spawnedIDs(msgs)
		if len(ids) == 0 {
			return schema.AssistantMessage(mockAnswer(task, nil), nil)
		}
		// wait_agents returns as soon as one sub-agent finishes, so the manager
		// waits in a loop: it answers once everyone is done and otherwise
		// reports what just came back before waiting for the rest.
		report := latestWaitReport(msgs)
		if report != nil && allFinished(report) {
			return schema.AssistantMessage(mockAnswer(task, collectResults(report)), nil)
		}
		args, _ := json.Marshal(map[string]any{"agent_ids": ids, "timeout_s": 60})
		content := "Both workers are running; I will report back as each finishes.\n"
		if report != nil {
			content = waitProgressLine(report)
		}
		return &schema.Message{
			Role:      schema.Assistant,
			Content:   content,
			ToolCalls: []schema.ToolCall{call(fmt.Sprintf("mock-wait-%d", turn), "wait_agents", string(args))},
		}
	}
}

// mockAnswer is the manager's final markdown. It reports the request and what
// the workers returned, so an end-to-end test can prove the whole path ran.
func mockAnswer(task string, results []string) string {
	var b strings.Builder
	b.WriteString("## Result\n\n")
	b.WriteString("Request: ")
	b.WriteString(task)
	b.WriteString("\n\n")
	if len(results) == 0 {
		b.WriteString("No sub-agent results were collected.\n")
		return b.String()
	}
	b.WriteString("Two sub-agents ran in parallel and reported back:\n\n")
	for i, r := range results {
		fmt.Fprintf(&b, "%d. %s\n", i+1, oneLine(r))
	}
	b.WriteString("\nTheir notes were written to the workspace as `notes/` files, ")
	b.WriteString("which you can open from the Files panel.\n\n")
	b.WriteString("> Generated by the offline scripted provider: no model was called.\n")
	return b.String()
}

// ---------- worker script ----------

// workerScript makes each worker do real work: one write into the conversation
// workspace, then a short report. The file is what proves the tool layer is
// wired to the right sandbox.
func workerScript(role string) mockScript {
	return func(turn int, msgs []*schema.Message) *schema.Message {
		task := firstUserText(msgs)
		switch turn {
		case 1:
			args, _ := json.Marshal(map[string]any{
				"file_path": "notes/" + safeName(role) + ".md",
				"content": "# " + role + " notes\n\n" +
					"Task: " + oneLine(task) + "\n\n" +
					"- collected the inputs this task needs\n" +
					"- recorded them here for the manager to read\n",
			})
			return &schema.Message{
				Role:             schema.Assistant,
				ReasoningContent: "I should write my findings to the workspace so the manager can read them.\n",
				ToolCalls:        []schema.ToolCall{call("mock-write-"+safeName(role), "write", string(args))},
			}
		default:
			return schema.AssistantMessage(
				role+" finished: notes written to notes/"+safeName(role)+".md for \""+oneLine(task)+"\".", nil)
		}
	}
}

// ---------- helpers ----------

func call(id, name, args string) schema.ToolCall {
	return schema.ToolCall{ID: id, Type: "function",
		Function: schema.FunctionCall{Name: name, Arguments: args}}
}

func lastUserText(msgs []*schema.Message) string {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i] != nil && msgs[i].Role == schema.User {
			if t := strings.TrimSpace(strings.TrimPrefix(msgs[i].Content, "[steer] ")); t != "" {
				return t
			}
		}
	}
	return "(no request)"
}

func firstUserText(msgs []*schema.Message) string {
	for _, m := range msgs {
		if m != nil && m.Role == schema.User && strings.TrimSpace(m.Content) != "" {
			return strings.TrimSpace(m.Content)
		}
	}
	return "(no task)"
}

// spawnedIDs harvests the agent ids the spawn tool handed back, exactly as a
// real manager has to read them out of its own tool results.
func spawnedIDs(msgs []*schema.Message) []string {
	var ids []string
	for _, m := range msgs {
		if m == nil || m.Role != schema.Tool || !strings.Contains(m.Content, "agent_id") {
			continue
		}
		var r struct {
			AgentID string `json:"agent_id"`
		}
		if json.Unmarshal([]byte(m.Content), &r) == nil && r.AgentID != "" {
			ids = append(ids, r.AgentID)
		}
	}
	return ids
}

// mockWaitReport mirrors the wait_agents result wire shape the manager reads.
type mockWaitReport struct {
	Agents []struct {
		AgentID string `json:"agent_id"`
		Status  string `json:"status"`
		Result  string `json:"result"`
		Err     string `json:"error"`
	} `json:"agents"`
	TimedOut bool `json:"timed_out"`
}

// latestWaitReport returns the most recent wait_agents tool result, or nil if
// the manager has not waited yet.
func latestWaitReport(msgs []*schema.Message) *mockWaitReport {
	for i := len(msgs) - 1; i >= 0; i-- {
		m := msgs[i]
		if m == nil || m.Role != schema.Tool || !strings.Contains(m.Content, `"agents"`) {
			continue
		}
		var r mockWaitReport
		if json.Unmarshal([]byte(m.Content), &r) == nil && r.Agents != nil {
			return &r
		}
	}
	return nil
}

// allFinished reports whether every sub-agent in the last wait has left the
// running state, which is the manager's cue to stop waiting and answer.
func allFinished(r *mockWaitReport) bool {
	for _, e := range r.Agents {
		if e.Status == "running" {
			return false
		}
	}
	return len(r.Agents) > 0
}

// collectResults pulls each finished worker's answer (or error) out of a wait
// report, in the order the agents were listed.
func collectResults(r *mockWaitReport) []string {
	var out []string
	for _, e := range r.Agents {
		switch {
		case e.Err != "":
			out = append(out, e.AgentID+": "+e.Err)
		case e.Result != "":
			out = append(out, e.Result)
		}
	}
	return out
}

// waitProgressLine is the one-line status the manager shows between waits.
func waitProgressLine(r *mockWaitReport) string {
	done, running := 0, 0
	for _, e := range r.Agents {
		if e.Status == "running" {
			running++
		} else {
			done++
		}
	}
	return fmt.Sprintf("%d finished, %d still working; waiting for the rest.\n", done, running)
}

func oneLine(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	r := []rune(s)
	if len(r) > 160 {
		return string(r[:160]) + "…"
	}
	return s
}

// safeName reduces a model-invented role to something usable as a file name.
func safeName(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-' || r == '_':
			b.WriteRune('-')
		default:
			if b.Len() > 0 && !strings.HasSuffix(b.String(), "-") {
				b.WriteRune('-')
			}
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "worker"
	}
	return out
}
