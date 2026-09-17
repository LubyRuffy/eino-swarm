package provider

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

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
// fans work out and collects it, the reviewer curates a project's memory, and
// every other role is a worker that does one tool call and reports back.
func newMockModel(role string) model.BaseChatModel {
	switch role {
	case managerRole, "":
		return &mockModel{script: managerScript}
	case reviewerRole:
		return &mockModel{script: reviewerScript}
	case titleRole:
		return &mockModel{script: titleNamerScript}
	case compactRole, sessionMemoryRole:
		// Briefings are not a chat stream. Pacing them like the manager
		// would add seconds of fake tokens to every /compact and session
		// refresh in --mock and the test suite.
		return &mockModel{script: compactSummarizerScript, instant: true}
	default:
		return &mockModel{script: workerScript(role)}
	}
}

const (
	managerRole = "manager"
	// reviewerRole must match the engine's review agent id, or a mock run
	// exercises the swarm and quietly skips the memory review.
	reviewerRole = "memory-reviewer"
	// titleRole must match the engine's conversation namer, or a mock run
	// would treat the namer as a worker and try to write files.
	titleRole = "title-namer"
	// compactRole must match the engine's conversation summarizer, or a mock
	// compact would be treated as a worker and try to write files.
	compactRole = "compact-summarizer"
	// sessionMemoryRole must match the engine's rolling session briefing, or
	// a mock run would treat that summarizer as a worker and try to write files.
	sessionMemoryRole = "session-memory"
	// completeGoalToolName must match the engine's manager-only tool, or a
	// mock run with a standing objective would never mark it done and the
	// runtime would auto-continue until the cap.
	completeGoalToolName = "complete_goal"
)

// mockScript maps a turn number and the conversation so far to the message the
// model "generates".
type mockScript func(turn int, msgs []*schema.Message) *schema.Message

type mockModel struct {
	script  mockScript
	instant bool

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
	if err := mockCallFailure(); err != nil {
		return nil, err
	}
	out := m.script(m.nextTurn(), in)
	attachMockUsage(in, out)
	return out, nil
}

func (m *mockModel) Stream(ctx context.Context, in []*schema.Message, _ ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := mockCallFailure(); err != nil {
		return nil, err
	}
	out := m.script(m.nextTurn(), in)
	sr, sw := schema.Pipe[*schema.Message](8)
	go func() {
		defer sw.Close()
		send := func(msg *schema.Message) bool {
			if m.instant {
				if ctx.Err() != nil {
					return false
				}
			} else {
				select {
				case <-ctx.Done():
					return false
				case <-time.After(mockChunkDelay):
				}
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
		send(&schema.Message{Role: schema.Assistant, ResponseMeta: usageMeta(in, out)})
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
	ids := spawnedIDs(msgs)
	// Spawn only when this conversation has no workers yet. A continued run
	// starts a new model instance whose turn counter is 1 again; re-spawning
	// would fan out forever instead of finishing the work already in flight.
	if len(ids) == 0 {
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
	}
	// wait_agents returns as soon as one sub-agent finishes, so the manager
	// waits in a loop: it answers once everyone is done and otherwise
	// reports what just came back before waiting for the rest.
	report := latestWaitReport(msgs)
	if report != nil && allFinished(report) {
		if mockShouldCompleteGoal(msgs) {
			args, _ := json.Marshal(map[string]any{
				"summary": "the standing objective is satisfied",
			})
			return &schema.Message{
				Role:    schema.Assistant,
				Content: "The standing objective is satisfied.\n",
				ToolCalls: []schema.ToolCall{
					call(fmt.Sprintf("mock-complete-%d", turn), completeGoalToolName, string(args)),
				},
			}
		}
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
		ToolCalls: []schema.ToolCall{call(nextWaitCallID(msgs), "wait_agents", string(args))},
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
// wired to the right directory.
// mockWorkerPause is how E2E keeps wait_agents pending long enough to Steer.
// Unit tests leave the env unset so they stay fast.
func mockWorkerPause() time.Duration {
	raw := strings.TrimSpace(os.Getenv("ZWAI_MOCK_WORKER_DELAY_MS"))
	if raw == "" {
		return 0
	}
	ms, err := strconv.Atoi(raw)
	if err != nil || ms <= 0 {
		return 0
	}
	return time.Duration(ms) * time.Millisecond
}

func workerScript(role string) mockScript {
	pause := mockWorkerPause()
	return func(turn int, msgs []*schema.Message) *schema.Message {
		if turn == 1 && pause > 0 {
			time.Sleep(pause)
		}
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

// ---------- reviewer script ----------

// reviewerScript is the post-turn memory review: store one note, record one
// skill, then report in a line. It is what makes `--mock` and the end-to-end
// tests exercise the whole memory path — the tools, the files, the event and
// the Memory panel — without an endpoint.
//
// Everything it writes is derived from the conversation it was handed. Nothing
// here may encode a particular task: the same text would otherwise show up in
// screenshots and test expectations as if the product had decided it.
func reviewerScript(turn int, msgs []*schema.Message) *schema.Message {
	task := oneLine(firstUserText(msgs))
	tag := conversationTag(task)
	clip := clipRunes(task, 96)
	switch turn {
	case 1:
		note, _ := json.Marshal(map[string]any{
			"action":  "add",
			"content": "Covered [" + tag + "]: " + clip,
		})
		skill, _ := json.Marshal(map[string]any{
			"action":      "create",
			"name":        "recorded-" + tag,
			"description": "After conversation " + tag,
			// The note already carries the clip. Putting it in the body
			// too is the exact twin the overlap check exists to stop, and
			// three concurrent reviews would then refuse each other's
			// creates. The tag is enough to keep the writes derived.
			"content": "For " + tag + ":\n1. Inspect the workspace.\n2. Collect results.\n3. Report first.\n",
		})
		return &schema.Message{
			Role:             schema.Assistant,
			ReasoningContent: "Worth keeping: what this project was asked for, and the shape of the work.\n",
			ToolCalls: []schema.ToolCall{
				call("mock-memory-1", "memory", string(note)),
				call("mock-skill-1", "skill_manage", string(skill)),
			},
		}
	default:
		return schema.AssistantMessage("Stored one note and one skill from this conversation.", nil)
	}
}

// ---------- title namer script ----------

// mockTitleMaxRunes is shorter than the engine's placeholder cap, so a
// generated mock title is visibly not the truncated request — which is the
// whole point of generating one.
const mockTitleMaxRunes = 24

// titleNamerScript returns a short sidebar label derived from the request.
// Nothing here may encode a particular task: the same text would otherwise
// show up in screenshots and test expectations as if the product had decided it.
func titleNamerScript(_ int, msgs []*schema.Message) *schema.Message {
	return schema.AssistantMessage(mockTitle(titleRequest(msgs)), nil)
}

const mockBriefingMaxRunes = 160

// compactSummarizerScript returns a short briefing derived from the messages
// it was handed. Same rule as the namer: nothing here may encode a particular
// task, or it would leak into tests as if the product had decided it.
func compactSummarizerScript(_ int, msgs []*schema.Message) *schema.Message {
	return schema.AssistantMessage(mockBriefing(lastUserText(msgs)), nil)
}

func mockBriefing(src string) string {
	flat := strings.Join(strings.Fields(src), " ")
	if flat == "" {
		return "Prior conversation, folded."
	}
	const prefix = "Prior work: "
	srcN := utf8.RuneCountInString(src)
	maxOut := mockBriefingMaxRunes
	if srcN > 0 && srcN-1 < maxOut {
		maxOut = srcN - 1
	}
	if maxOut < 1 {
		return ""
	}
	prefixN := utf8.RuneCountInString(prefix)
	if maxOut <= prefixN {
		out := "ok"
		if utf8.RuneCountInString(out) > maxOut {
			return string([]rune(out)[:maxOut])
		}
		return out
	}
	bodyN := maxOut - prefixN
	r := []rune(flat)
	if len(r) > bodyN {
		flat = strings.TrimSpace(string(r[:bodyN]))
	}
	return prefix + flat
}

func titleRequest(msgs []*schema.Message) string {
	text := lastUserText(msgs)
	for _, line := range strings.Split(text, "\n") {
		if rest, ok := strings.CutPrefix(line, "User: "); ok {
			if t := strings.TrimSpace(rest); t != "" {
				return t
			}
		}
	}
	return text
}

func mockTitle(task string) string {
	flat := strings.Join(strings.Fields(task), " ")
	if flat == "" {
		return "Conversation"
	}
	r := []rune(flat)
	if len(r) > mockTitleMaxRunes {
		return strings.TrimSpace(string(r[:mockTitleMaxRunes]))
	}
	return flat
}

// ---------- helpers ----------

func call(id, name, args string) schema.ToolCall {
	return schema.ToolCall{ID: id, Type: "function",
		Function: schema.FunctionCall{Name: name, Arguments: args}}
}

// completeOpenGoal is whether the scripted manager calls complete_goal when
// the prompt still has an open standing objective. Tests that need to
// observe auto-continue turn it off; --mock otherwise would spin until the cap.
var (
	completeOpenGoalMu sync.Mutex
	completeOpenGoal   = true
)

// SetCompleteOpenGoal controls whether the scripted manager marks an open
// standing objective done. Tests that need auto-continue call this with false
// and restore true in Cleanup.
func SetCompleteOpenGoal(v bool) {
	completeOpenGoalMu.Lock()
	completeOpenGoal = v
	completeOpenGoalMu.Unlock()
}

func completeOpenGoalEnabled() bool {
	completeOpenGoalMu.Lock()
	defer completeOpenGoalMu.Unlock()
	return completeOpenGoal
}

var (
	mockFailMu  sync.Mutex
	mockFailErr error
)

// SetMockFailure makes every scripted model call return err. Tests that
// need a crashed turn call this and restore nil in Cleanup.
func SetMockFailure(err error) {
	mockFailMu.Lock()
	mockFailErr = err
	mockFailMu.Unlock()
}

func mockCallFailure() error {
	mockFailMu.Lock()
	defer mockFailMu.Unlock()
	return mockFailErr
}

func mockShouldCompleteGoal(msgs []*schema.Message) bool {
	if !completeOpenGoalEnabled() || alreadyCalledCompleteGoal(msgs) {
		return false
	}
	for _, m := range msgs {
		if m == nil || m.Role != schema.System {
			continue
		}
		if strings.Contains(m.Content, completeGoalToolName) &&
			!strings.Contains(m.Content, "was completed") {
			return true
		}
	}
	return false
}

func alreadyCalledCompleteGoal(msgs []*schema.Message) bool {
	for _, m := range msgs {
		if m == nil {
			continue
		}
		for _, c := range m.ToolCalls {
			if c.Function.Name == completeGoalToolName {
				return true
			}
		}
	}
	return false
}

func attachMockUsage(in []*schema.Message, out *schema.Message) {
	if out == nil {
		return
	}
	if out.ResponseMeta == nil {
		out.ResponseMeta = &schema.ResponseMeta{}
	}
	out.ResponseMeta.Usage = mockTokenUsage(in, out)
}

func usageMeta(in []*schema.Message, out *schema.Message) *schema.ResponseMeta {
	return &schema.ResponseMeta{Usage: mockTokenUsage(in, out)}
}

func mockTokenUsage(in []*schema.Message, out *schema.Message) *schema.TokenUsage {
	prompt := 0
	for _, m := range in {
		prompt += messageRunes(m)
	}
	completion := messageRunes(out)
	return &schema.TokenUsage{
		PromptTokens:     prompt,
		CompletionTokens: completion,
		TotalTokens:      prompt + completion,
	}
}

func messageRunes(m *schema.Message) int {
	if m == nil {
		return 0
	}
	n := utf8.RuneCountInString(m.Content) + utf8.RuneCountInString(m.ReasoningContent)
	for _, c := range m.ToolCalls {
		n += utf8.RuneCountInString(c.Function.Arguments)
	}
	return n
}

func lastUserText(msgs []*schema.Message) string {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i] != nil && msgs[i].Role == schema.User {
			if t := strings.TrimSpace(strings.TrimPrefix(plainUserText(msgs[i]), "[steer]")); t != "" {
				return t
			}
		}
	}
	return "(no request)"
}

func firstUserText(msgs []*schema.Message) string {
	for _, m := range msgs {
		if m != nil && m.Role == schema.User {
			if t := strings.TrimSpace(plainUserText(m)); t != "" {
				return t
			}
		}
	}
	return "(no task)"
}

func plainUserText(m *schema.Message) string {
	if t := strings.TrimSpace(m.Content); t != "" {
		return t
	}
	for _, p := range m.UserInputMultiContent {
		if p.Type == schema.ChatMessagePartTypeText {
			if t := strings.TrimSpace(p.Text); t != "" {
				return t
			}
		}
	}
	return ""
}

// spawnedIDs harvests the agent ids the spawn tool handed back, exactly as a
// real manager has to read them out of its own tool results.
func nextWaitCallID(msgs []*schema.Message) string {
	n := 0
	for _, m := range msgs {
		if m == nil || m.Role != schema.Assistant {
			continue
		}
		for _, tc := range m.ToolCalls {
			if tc.Function.Name == "wait_agents" {
				n++
			}
		}
	}
	return fmt.Sprintf("mock-wait-%d", n+1)
}

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

func clipRunes(s string, max int) string {
	r := []rune(s)
	if max <= 0 || len(r) <= max {
		return s
	}
	return string(r[:max])
}

// conversationTag distinguishes two reviews of similar transcripts so the
// memory tools do not treat them as one subject. The clip still carries
// words from the conversation; the tag is what keeps the writes unique.
func conversationTag(s string) string {
	h := fnv.New32a()
	_, _ = h.Write([]byte(s))
	return fmt.Sprintf("%08x", h.Sum32())
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
