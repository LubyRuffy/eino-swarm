// The terminal shell is the fallback when something misbehaves in the app, so
// its state machine is tested the same way: feed it the notification stream a
// real run produces and check what a user would end up looking at.
package tui

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	swarm "github.com/LubyRuffy/eino-swarm"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/cloudwego/eino/schema"
)

// feed drives the model with a whole run's worth of notifications.
func feed(m *swarmTUI, notes ...swarm.Notification) {
	for _, n := range notes {
		m.apply(n)
	}
}

func TestNotificationsBecomeAgentsAndBlocks(t *testing.T) {
	m := newModel(nil)
	feed(&m,
		swarm.Notification{Kind: swarm.NotifyReasoningDelta, AgentID: swarm.DefaultManagerID, Text: "two parts"},
		swarm.Notification{Kind: swarm.NotifyDelta, AgentID: swarm.DefaultManagerID, Text: "Starting"},
		swarm.Notification{Kind: swarm.NotifySpawned, AgentID: "researcher-1", Role: "researcher"},
		swarm.Notification{Kind: swarm.NotifyToolCall, AgentID: "researcher-1", Text: "read(notes.md)"},
		swarm.Notification{Kind: swarm.NotifyToolResult, AgentID: "researcher-1", Text: "the file body"},
		swarm.Notification{Kind: swarm.NotifyAgentMessage, AgentID: "researcher-1", Text: "found three dates"},
		swarm.Notification{Kind: swarm.NotifyFinished, AgentID: "researcher-1", Text: "found three dates"},
	)

	if len(m.agents) != 1 || m.agents[0].role != "researcher" {
		t.Fatalf("the spawned worker did not register: %+v", m.agents)
	}
	worker := m.agents[0]
	if !worker.finished || worker.finErr != nil {
		t.Fatalf("worker state after finishing: finished=%v err=%v", worker.finished, worker.finErr)
	}

	// Thinking collapses once the answer starts, and a completed tool call
	// folds to its summary line: an unattended run must not bury the answer.
	think := blocksOfKind(m.manager, blockThinking)
	if len(think) != 1 || think[0].open || think[0].live {
		t.Fatalf("thinking block should be closed once the answer began: %+v", think)
	}
	tools := blocksOfKind(worker, blockTool)
	if len(tools) != 1 || tools[0].open || tools[0].toolName != "read" || tools[0].toolArgs != "notes.md" {
		t.Fatalf("tool block: %+v", tools)
	}
	if worker.curTool != nil {
		t.Fatal("a tool that returned is no longer the live one")
	}
}

func TestAResumedWorkerDoesNotDuplicateTheRoster(t *testing.T) {
	m := newModel(nil)
	feed(&m,
		swarm.Notification{Kind: swarm.NotifySpawned, AgentID: "w1", Role: "worker"},
		swarm.Notification{Kind: swarm.NotifyFinished, AgentID: "w1", Err: errors.New("timed out")},
		swarm.Notification{Kind: swarm.NotifySpawned, AgentID: "w1", Role: "worker"},
	)
	if len(m.agents) != 1 {
		t.Fatalf("resume minted a twin: %+v", m.agents)
	}
	if m.agents[0].finished || m.agents[0].finErr != nil {
		t.Fatalf("resumed worker should look alive: finished=%v err=%v", m.agents[0].finished, m.agents[0].finErr)
	}
}

// A turn boundary starts a new answer block, or the second turn's text would
// be appended to the first and the transcript would read as one long message.
func TestATurnBoundaryStartsANewAnswerBlock(t *testing.T) {
	m := newModel(nil)
	feed(&m,
		swarm.Notification{Kind: swarm.NotifyDelta, AgentID: swarm.DefaultManagerID, Text: "first"},
		swarm.Notification{Kind: swarm.NotifyTurn, AgentID: swarm.DefaultManagerID, Text: "turn 2"},
		swarm.Notification{Kind: swarm.NotifyDelta, AgentID: swarm.DefaultManagerID, Text: "second"},
	)
	answers := blocksOfKind(m.manager, blockAnswer)
	if len(answers) != 2 {
		t.Fatalf("want two answer blocks, got %d", len(answers))
	}
	if answers[0].answer != "first" || answers[1].answer != "second" {
		t.Fatalf("answers=%q,%q", answers[0].answer, answers[1].answer)
	}
}

// Deltas carry the full text so far, so the block is replaced rather than
// appended to. Doubling text is the classic bug here.
func TestDeltasReplaceRatherThanAccumulate(t *testing.T) {
	m := newModel(nil)
	feed(&m,
		swarm.Notification{Kind: swarm.NotifyDelta, AgentID: swarm.DefaultManagerID, Text: "The"},
		swarm.Notification{Kind: swarm.NotifyDelta, AgentID: swarm.DefaultManagerID, Text: "The three"},
		swarm.Notification{Kind: swarm.NotifyDelta, AgentID: swarm.DefaultManagerID, Text: "The three files"},
	)
	answers := blocksOfKind(m.manager, blockAnswer)
	if len(answers) != 1 || answers[0].answer != "The three files" {
		t.Fatalf("blocks=%d text=%q", len(answers), answers[0].answer)
	}
}

func TestNotificationsForUnknownAgentsAreIgnored(t *testing.T) {
	m := newModel(nil)
	feed(&m, swarm.Notification{Kind: swarm.NotifyDelta, AgentID: "never-spawned", Text: "hello"})
	if len(m.manager.blocks) != 0 || len(m.agents) != 0 {
		t.Fatal("an event for an agent that was never spawned must not invent one")
	}
}

func TestFailedWorkerKeepsItsError(t *testing.T) {
	m := newModel(nil)
	boom := errors.New("the endpoint refused the connection")
	feed(&m,
		swarm.Notification{Kind: swarm.NotifySpawned, AgentID: "w1", Role: "researcher"},
		swarm.Notification{Kind: swarm.NotifyFinished, AgentID: "w1", Err: boom},
	)
	if m.agents[0].finErr == nil {
		t.Fatal("the failure was dropped")
	}
	dump := m.DumpTranscript()
	if !strings.Contains(dump, boom.Error()) {
		t.Fatalf("the transcript hides why the worker failed:\n%s", dump)
	}
}

// Keys are the only way to see a worker's own transcript, so the selection
// walk has to stay inside the roster.
func TestKeysWalkTheRosterAndWrapAround(t *testing.T) {
	m := newModel(nil)
	feed(&m,
		swarm.Notification{Kind: swarm.NotifySpawned, AgentID: "w1", Role: "a"},
		swarm.Notification{Kind: swarm.NotifySpawned, AgentID: "w2", Role: "b"},
	)

	for _, tc := range []struct {
		key  string
		want int
	}{
		{"tab", 0}, {"tab", 1}, {"tab", -1}, // past the last worker is the manager
		{"left", 1}, {"left", 0}, {"left", -1},
		{"2", 1}, {"esc", -1}, {"9", -1}, // a number with no worker changes nothing
	} {
		m = press(t, m, tc.key)
		if m.selected != tc.want {
			t.Fatalf("after %q selected=%d want %d", tc.key, m.selected, tc.want)
		}
		if m.currentAgent() == nil {
			t.Fatalf("after %q there is no current agent", tc.key)
		}
	}

	if m = press(t, m, "q"); !m.quitting {
		t.Fatal("q must quit")
	}
}

func TestToggleKeysFoldAndUnfold(t *testing.T) {
	m := newModel(nil)
	feed(&m,
		swarm.Notification{Kind: swarm.NotifyReasoningDelta, AgentID: swarm.DefaultManagerID, Text: "thinking"},
		swarm.Notification{Kind: swarm.NotifyToolCall, AgentID: swarm.DefaultManagerID, Text: "ls(.)"},
		swarm.Notification{Kind: swarm.NotifyToolResult, AgentID: swarm.DefaultManagerID, Text: "one file"},
	)
	// The tool call ended the thinking block, so it is folded; t unfolds it
	// and folds it again.
	m = press(t, m, "t")
	if !blocksOfKind(m.manager, blockThinking)[0].open {
		t.Fatal("t should unfold a folded thinking block")
	}
	m = press(t, m, "t")
	if blocksOfKind(m.manager, blockThinking)[0].open {
		t.Fatal("t again should fold it")
	}
	m = press(t, m, "enter")
	if !blocksOfKind(m.manager, blockTool)[0].open {
		t.Fatal("enter should unfold a folded tool block")
	}
}

// The view is rendered on every tick, including before the first size message
// and with no agents at all. It must never panic or come back empty.
func TestViewRendersAtEverySize(t *testing.T) {
	m := newModel(nil)
	feed(&m,
		swarm.Notification{Kind: swarm.NotifyReasoningDelta, AgentID: swarm.DefaultManagerID, Text: "a\nb\nc"},
		swarm.Notification{Kind: swarm.NotifyDelta, AgentID: swarm.DefaultManagerID, Text: "answer text"},
		swarm.Notification{Kind: swarm.NotifySpawned, AgentID: "w1", Role: "researcher"},
		swarm.Notification{Kind: swarm.NotifyToolCall, AgentID: "w1", Text: "write(out.md)"},
	)
	for _, size := range []tea.WindowSizeMsg{{}, {Width: 40, Height: 12}, {Width: 200, Height: 60}} {
		next, _ := m.Update(size)
		m = next.(swarmTUI)
		for _, sel := range []int{-1, 0, 5} {
			m.selected = sel
			if strings.TrimSpace(m.View()) == "" {
				t.Fatalf("empty view at %dx%d selected=%d", size.Width, size.Height, sel)
			}
		}
		m.selected = -1
	}
}

// The transcript is printed after the altscreen closes, so it is all the user
// keeps from the run.
func TestDumpTranscriptKeepsTheWholeRun(t *testing.T) {
	m := newModel(nil)
	feed(&m,
		swarm.Notification{Kind: swarm.NotifyReasoningDelta, AgentID: swarm.DefaultManagerID, Text: "plan it"},
		swarm.Notification{Kind: swarm.NotifyAgentMessage, AgentID: swarm.DefaultManagerID, Text: "here is the summary"},
		swarm.Notification{Kind: swarm.NotifySpawned, AgentID: "w1", Role: "researcher"},
		swarm.Notification{Kind: swarm.NotifyToolCall, AgentID: "w1", Text: "read(notes.md)"},
		swarm.Notification{Kind: swarm.NotifyToolResult, AgentID: "w1", Text: "line one\nline two"},
		swarm.Notification{Kind: swarm.NotifyFinished, AgentID: "w1", Text: "done"},
	)
	dump := m.DumpTranscript()
	for _, want := range []string{
		"manager transcript", "plan it", "here is the summary",
		"worker 1: w1 (ok)", "read", "line one",
	} {
		if !strings.Contains(dump, want) {
			t.Fatalf("the transcript is missing %q:\n%s", want, dump)
		}
	}
	// The tool marker is written once; twice was a copy-paste artefact.
	if strings.Contains(dump, "⚙ ⚙") {
		t.Fatalf("doubled tool marker:\n%s", dump)
	}
}

// The roster shows what each worker is doing right now, in order of how
// specific the evidence is.
func TestLiveTailPrefersTheMostRecentActivity(t *testing.T) {
	a := &agentState{id: "w1", open: map[int]bool{}}
	if got := a.liveTail(); got != "" {
		t.Fatalf("an idle agent has no tail, got %q", got)
	}
	a.ensureThink().thinkText = "first\nthinking tail"
	if got := a.liveTail(); got != "thinking tail" {
		t.Fatalf("tail=%q", got)
	}
	a.ensureAnswer().answer = "first\nanswer tail"
	if got := a.liveTail(); got != "answer tail" {
		t.Fatalf("tail=%q", got)
	}
	a.curTool = &block{kind: blockTool, toolName: "grep", toolArgs: "deadline"}
	if got := a.liveTail(); !strings.HasPrefix(got, "grep") {
		t.Fatalf("a running tool wins: %q", got)
	}
	a.curTool.toolRes = "3 matches"
	if got := a.liveTail(); got != "3 matches" {
		t.Fatalf("its result wins once it has one: %q", got)
	}
}

func TestTextHelpers(t *testing.T) {
	// Truncation cuts runes, not bytes: a split multi-byte character renders
	// as a broken glyph.
	if got := trunc(strings.Repeat("目", 10), 4); got != "目目目…" {
		t.Fatalf("trunc=%q", got)
	}
	if got := trunc("short", 40); got != "short" {
		t.Fatalf("trunc=%q", got)
	}
	if got := lastNLines("a\nb\nc\nd\n", 2); strings.Join(got, ",") != "c,d" {
		t.Fatalf("lastNLines=%v", got)
	}
	if got := lastNLines("only", 3); strings.Join(got, ",") != "only" {
		t.Fatalf("lastNLines=%v", got)
	}
	if got := firstWords("one two three four", 2); got != "one two…" {
		t.Fatalf("firstWords=%q", got)
	}
	if got := firstWords("one two", 5); got != "one two" {
		t.Fatalf("firstWords should leave a short line alone, got %q", got)
	}
	if got := firstLine("head\ntail"); got != "head" {
		t.Fatalf("firstLine=%q", got)
	}
	if got := lastLine("head\ntail\n"); got != "tail" {
		t.Fatalf("lastLine=%q", got)
	}
	if maxInt(3, 7) != 7 || maxInt(7, 3) != 7 {
		t.Fatal("maxInt")
	}
	if toolNameOf("plain") != "plain" || toolArgsOf("plain") != "" {
		t.Fatal("a tool line without arguments still has a name")
	}
}

// The tick drains the swarm's channel; a closed channel ends the program
// instead of spinning.
func TestTickDrainsNotificationsAndStopsOnClose(t *testing.T) {
	ch := make(chan notificationMsg, 2)
	m := newModel(nil)
	m.notifications = ch

	ch <- notificationMsg{Notification: swarm.Notification{Kind: swarm.NotifySpawned, AgentID: "w1", Role: "researcher"}}
	ch <- notificationMsg{Notification: swarm.Notification{Kind: swarm.NotifyDelta, AgentID: "w1", Text: "working"}}
	next, cmd := m.Update(tickMsg{})
	m = next.(swarmTUI)
	if len(m.agents) != 1 || len(m.agents[0].blocks) != 1 {
		t.Fatalf("the tick did not drain the channel: %+v", m.agents)
	}
	if cmd == nil {
		t.Fatal("the tick must reschedule itself or the UI freezes")
	}

	close(ch)
	if _, cmd = m.Update(tickMsg{}); cmd == nil {
		t.Fatal("a closed channel should quit")
	}
}

// What the panes show is the whole point of the terminal shell: the manager's
// own bookkeeping calls are noise, and a worker that failed has to look failed.
func TestPanesShowTheConversationAndHideTheBookkeeping(t *testing.T) {
	m := newModel(nil)
	m.width, m.height = 120, 40
	feed(&m,
		swarm.Notification{Kind: swarm.NotifyReasoningDelta, AgentID: swarm.DefaultManagerID, Text: "two passes"},
		swarm.Notification{Kind: swarm.NotifyToolCall, AgentID: swarm.DefaultManagerID, Text: "wait_agents({})"},
		swarm.Notification{Kind: swarm.NotifyToolCall, AgentID: swarm.DefaultManagerID, Text: "close_agent({})"},
		swarm.Notification{Kind: swarm.NotifyToolCall, AgentID: swarm.DefaultManagerID, Text: "read(notes.md)"},
		swarm.Notification{Kind: swarm.NotifySpawned, AgentID: "w1", Role: "researcher"},
		swarm.Notification{Kind: swarm.NotifyToolCall, AgentID: "w1", Text: "grep(deadline)"},
		swarm.Notification{Kind: swarm.NotifySpawned, AgentID: "w2", Role: "reviewer"},
		swarm.Notification{Kind: swarm.NotifyFinished, AgentID: "w2", Err: errors.New("the endpoint refused the connection")},
	)

	managerPane := m.pane(m.manager, 80, 30)
	if !strings.Contains(managerPane, "MANAGER") {
		t.Fatalf("the manager pane is unlabelled:\n%s", managerPane)
	}
	if !strings.Contains(managerPane, "read") {
		t.Fatalf("a real tool call is missing:\n%s", managerPane)
	}
	for _, hidden := range []string{"wait_agents", "close_agent"} {
		if strings.Contains(managerPane, hidden) {
			t.Fatalf("%s is bookkeeping, not conversation:\n%s", hidden, managerPane)
		}
	}
	if m.pane(nil, 80, 30) != "" {
		t.Fatal("there is nothing to render without an agent")
	}
	if !strings.Contains(m.pane(m.agents[0], 60, 20), "w1") {
		t.Fatal("a worker's pane is titled with its id")
	}

	roster := m.roster(30, 20)
	for _, want := range []string{"SUB-AGENTS (2)", "w1", "w2", "error", "grep"} {
		if !strings.Contains(roster, want) {
			t.Fatalf("the roster is missing %q:\n%s", want, roster)
		}
	}

	// the whole screen renders at both the selected-agent and overview states
	if !strings.Contains(m.View(), "MANAGER") {
		t.Fatal("the overview should show the manager")
	}
	m.selected = 0
	if view := m.View(); !strings.Contains(view, "w1") {
		t.Fatalf("selecting a worker should show it:\n%s", view)
	}
	if m.Init() == nil {
		t.Fatal("the model has to start its own tick or nothing ever updates")
	}
}

func TestAJSONToolCallShowsTheCommandNotTheEnvelope(t *testing.T) {
	m := newModel(nil)
	m.width, m.height = 120, 40
	feed(&m,
		swarm.Notification{Kind: swarm.NotifyToolCall, AgentID: swarm.DefaultManagerID,
			Text: `exec({"command":"echo hi"})`},
		swarm.Notification{Kind: swarm.NotifyToolResult, AgentID: swarm.DefaultManagerID,
			Text: `{"exit_code":127,"stdout":"","stderr":"not found","failed":true,"error":"exit status 127","full_command":"echo hi"}`},
	)
	tools := blocksOfKind(m.manager, blockTool)
	if len(tools) != 1 || tools[0].toolArgs != "echo hi" || !tools[0].toolFailed {
		t.Fatalf("tool block: %+v", tools)
	}
	if strings.Contains(tools[0].toolRes, "full_command") || strings.Contains(tools[0].toolRes, "{") {
		t.Fatalf("the JSON envelope leaked into the result: %q", tools[0].toolRes)
	}
	pane := m.pane(m.manager, 80, 30)
	if strings.Contains(pane, `"command"`) || strings.Contains(pane, "full_command") {
		t.Fatalf("JSON leaked into the pane:\n%s", pane)
	}
	if !strings.Contains(pane, "echo hi") {
		t.Fatalf("the command is missing:\n%s", pane)
	}
	if !strings.Contains(pane, "exit status 127") {
		t.Fatalf("the error is missing:\n%s", pane)
	}
}

func TestALiveToolDeltaUpdatesTheOpenRow(t *testing.T) {
	m := newModel(nil)
	m.width, m.height = 120, 40
	feed(&m,
		swarm.Notification{
			Kind: swarm.NotifyToolCall, AgentID: swarm.DefaultManagerID,
			ToolCallID: "c1", Text: `exec({"command":"printf x"})`,
		},
		swarm.Notification{
			Kind: swarm.NotifyToolDelta, AgentID: swarm.DefaultManagerID,
			ToolCallID: "c1", Text: `{"stdout":"chunk-one","stderr":""}`,
		},
	)
	tools := blocksOfKind(m.manager, blockTool)
	if len(tools) != 1 || !tools[0].open || tools[0].toolRes != "chunk-one" {
		t.Fatalf("live tool row: %+v", tools)
	}
	if m.manager.curTool == nil {
		t.Fatal("a running call is still the live one")
	}
	pane := m.pane(m.manager, 80, 30)
	if !strings.Contains(pane, "chunk-one") {
		t.Fatalf("live stdout missing from the pane:\n%s", pane)
	}
}

func TestParallelToolDeltasStayOnTheirOwnRows(t *testing.T) {
	m := newModel(nil)
	feed(&m,
		swarm.Notification{
			Kind: swarm.NotifyToolCall, AgentID: swarm.DefaultManagerID,
			ToolCallID: "c1", Text: `exec({"command":"printf a"})`,
		},
		swarm.Notification{
			Kind: swarm.NotifyToolCall, AgentID: swarm.DefaultManagerID,
			ToolCallID: "c2", Text: `exec({"command":"printf b"})`,
		},
		swarm.Notification{
			Kind: swarm.NotifyToolDelta, AgentID: swarm.DefaultManagerID,
			ToolCallID: "c1", Text: `{"stdout":"alpha","stderr":""}`,
		},
		swarm.Notification{
			Kind: swarm.NotifyToolDelta, AgentID: swarm.DefaultManagerID,
			ToolCallID: "c2", Text: `{"stdout":"beta","stderr":""}`,
		},
	)
	tools := blocksOfKind(m.manager, blockTool)
	if len(tools) != 2 {
		t.Fatalf("want two tool rows, got %+v", tools)
	}
	if tools[0].toolRes != "alpha" || tools[1].toolRes != "beta" {
		t.Fatalf("live streams mixed: %+v", tools)
	}
	if !tools[0].open || !tools[1].open {
		t.Fatalf("pending rows should stay open: %+v", tools)
	}
}

// Every block has a folded and an expanded form, and the pane also carries the
// live tail of whatever is streaming right now.
func TestPanesRenderFoldedAndExpandedBlocks(t *testing.T) {
	m := newModel(nil)
	m.width, m.height = 120, 40
	feed(&m,
		swarm.Notification{Kind: swarm.NotifyReasoningDelta, AgentID: swarm.DefaultManagerID,
			Text: "a long stretch of thinking that goes on\nover several lines"},
		swarm.Notification{Kind: swarm.NotifyToolCall, AgentID: swarm.DefaultManagerID, Text: "read(notes.md)"},
		swarm.Notification{Kind: swarm.NotifyToolResult, AgentID: swarm.DefaultManagerID, Text: "the file body"},
		swarm.Notification{Kind: swarm.NotifyToolCall, AgentID: swarm.DefaultManagerID, Text: "wait_agents({})"},
		swarm.Notification{Kind: swarm.NotifyDelta, AgentID: swarm.DefaultManagerID,
			Text: "first line of the answer\nsecond line"},
	)

	// while a turn runs, the pane ends with what is streaming
	live := m.pane(m.manager, 80, 30)
	if !strings.Contains(live, "waiting for sub-agents") {
		t.Fatalf("a pending wait has to be visible:\n%s", live)
	}
	if !strings.Contains(live, "second line") {
		t.Fatalf("the streamed tail is missing:\n%s", live)
	}
	// a thought that has ended folds to a preview with its size
	if !strings.Contains(live, "thought (") {
		t.Fatalf("a folded thought should say how much it hid:\n%s", live)
	}
	// a returned tool folds to its summary
	if !strings.Contains(live, "→ the file body") {
		t.Fatalf("a folded tool should preview its result:\n%s", live)
	}

	// unfolding shows the arguments and the result in full. The running
	// wait_agents block is already open, so the first enter folds everything
	// and the second opens it — one key, one state for the whole pane.
	m = press(t, m, "t")
	m = press(t, m, "enter")
	if strings.Contains(m.pane(m.manager, 80, 30), "enter to fold") {
		t.Fatal("the first enter should have folded the open tool block")
	}
	m = press(t, m, "enter")
	open := m.pane(m.manager, 80, 30)
	for _, want := range []string{"enter to fold", "notes.md", "← the file body"} {
		if !strings.Contains(open, want) {
			t.Fatalf("an expanded block is missing %q:\n%s", want, open)
		}
	}
}

func blocksOfKind(a *agentState, kind blockKind) []*block {
	var out []*block
	for _, b := range a.blocks {
		if b.kind == kind {
			out = append(out, b)
		}
	}
	return out
}

func press(t *testing.T, m swarmTUI, key string) swarmTUI {
	t.Helper()
	next, _ := m.Update(keyMsg(key))
	return next.(swarmTUI)
}

func typeKeys(t *testing.T, m swarmTUI, text string) swarmTUI {
	t.Helper()
	for _, r := range text {
		m = press(t, m, string(r))
	}
	return m
}

// keyMsg builds the tea.KeyMsg whose String() is key.
func keyMsg(key string) tea.KeyMsg {
	switch key {
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	case "left":
		return tea.KeyMsg{Type: tea.KeyLeft}
	case "right":
		return tea.KeyMsg{Type: tea.KeyRight}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "backspace":
		return tea.KeyMsg{Type: tea.KeyBackspace}
	case "ctrl+c":
		return tea.KeyMsg{Type: tea.KeyCtrlC}
	case "ctrl+h":
		return tea.KeyMsg{Type: tea.KeyCtrlH}
	case "shift+tab":
		return tea.KeyMsg{Type: tea.KeyShiftTab}
	case "up":
		return tea.KeyMsg{Type: tea.KeyUp}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	default:
		return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)}
	}
}

// bubbletea hands Update a copy of the model, so the workers spawned during a
// run only exist on the model it returns. Printing the one we started with
// would drop every worker from the transcript.
func TestTheTranscriptComesFromTheModelBubbleteaReturns(t *testing.T) {
	started := newModel(nil)
	ended := newModel(nil)
	ended.apply(swarm.Notification{Kind: swarm.NotifySpawned, AgentID: "w1", Role: "researcher"})

	if got := finalOf(started, ended).DumpTranscript(); !strings.Contains(got, "w1") {
		t.Fatalf("the worker is missing from the transcript:\n%s", got)
	}
	// A program that failed before producing a model still prints something.
	if got := finalOf(started, nil).DumpTranscript(); !strings.Contains(got, "manager transcript") {
		t.Fatalf("no fallback transcript:\n%s", got)
	}
}

func TestRunConfigPrefixesTheHostEnvironment(t *testing.T) {
	cfg := runConfig(nil, "do the thing")
	if cfg.Instruction != "do the thing" || cfg.Task != "do the thing" {
		t.Fatalf("empty preamble should leave the task alone: %+v", cfg)
	}
	reg := swarm.NewRegistry()
	reg.WorkerPreamble = "OS: testhost"
	cfg = runConfig(reg, "do the thing")
	if cfg.Task != "do the thing" {
		t.Fatalf("task=%q", cfg.Task)
	}
	if cfg.Instruction != "OS: testhost\n\ndo the thing" {
		t.Fatalf("instruction=%q", cfg.Instruction)
	}
}

func TestSessionConfigPrefixesTheGoalAndKeepsTools(t *testing.T) {
	reg := swarm.NewRegistry()
	reg.WorkerPreamble = "OS: testhost"
	s := Session{
		Registry:      reg,
		Extra:         "## Goal\n\nkeep going",
		ManagerTools:  nil,
		MaxIterations: 40,
	}
	cfg := sessionConfig(s, "do the thing", nil)
	if !strings.HasPrefix(cfg.Instruction, "## Goal") {
		t.Fatalf("goal must lead the instruction:\n%s", cfg.Instruction)
	}
	if cfg.MaxIterations != 40 {
		t.Fatalf("session iteration cap=%d", cfg.MaxIterations)
	}
	if !strings.Contains(cfg.Instruction, "do the thing") {
		t.Fatal("task vanished")
	}
	msgs := dropSystem([]*schema.Message{
		schema.SystemMessage("old"),
		schema.UserMessage("keep"),
	})
	if len(msgs) != 1 || msgs[0].Content != "keep" {
		t.Fatalf("dropSystem=%+v", msgs)
	}
}

func TestSessionConfigKeepsAStableInstructionOffTheTask(t *testing.T) {
	prompt := "You are the manager.\nBesides the delegation tools you have: web_search."
	s := Session{
		Instruction:  prompt,
		Extra:        "## Goal\n\nkeep going",
		ManagerTools: nil,
	}
	cfg := sessionConfig(s, "look into the thing", nil)
	if cfg.Instruction != prompt {
		t.Fatalf("instruction must stay the app prompt, not the typed task:\n%s", cfg.Instruction)
	}
	if cfg.Task != "look into the thing" {
		t.Fatalf("task=%q", cfg.Task)
	}
	if strings.Contains(cfg.Instruction, "## Goal") {
		t.Fatal("Extra must not be prepended twice when Instruction is already complete")
	}
}

func TestInteractiveComposerSendsTheTypedTask(t *testing.T) {
	prompts := make(chan string, 1)
	m := newModel(nil)
	m.interactive = true
	m.prompts = prompts

	m = typeKeys(t, m, "look into it")
	if m.input != "look into it" {
		t.Fatalf("input=%q", m.input)
	}
	next, cmd := m.Update(keyMsg("enter"))
	m = next.(swarmTUI)
	if m.input != "" || !m.busy {
		t.Fatalf("after enter input=%q busy=%v", m.input, m.busy)
	}
	if cmd == nil {
		t.Fatal("enter must hand the text to the session loop")
	}
	if msg := cmd(); msg != nil {
		t.Fatalf("submit cmd should be silent, got %T", msg)
	}
	if got := <-prompts; got != "look into it" {
		t.Fatalf("prompt=%q", got)
	}
	users := blocksOfKind(m.manager, blockUser)
	if len(users) != 1 || users[0].answer != "look into it" {
		t.Fatalf("the typed task never landed in the transcript: %+v", users)
	}
	dump := m.DumpTranscript()
	if !strings.Contains(dump, "> look into it") {
		t.Fatalf("the durable transcript hid the human line:\n%s", dump)
	}
}

func TestEmptyEnterDoesNotStartATurn(t *testing.T) {
	prompts := make(chan string, 1)
	m := newModel(nil)
	m.interactive = true
	m.prompts = prompts
	next, cmd := m.Update(keyMsg("enter"))
	m = next.(swarmTUI)
	if m.busy || cmd != nil {
		t.Fatal("a blank enter started a turn")
	}
	select {
	case got := <-prompts:
		t.Fatalf("blank enter sent %q", got)
	default:
	}
}

func TestInteractiveQIsALetterAndCtrlCQuits(t *testing.T) {
	m := newModel(nil)
	m.interactive = true
	m.prompts = make(chan string, 1)
	m = press(t, m, "q")
	if m.quitting || m.input != "q" {
		t.Fatalf("q should be typed while the composer is open, quitting=%v input=%q", m.quitting, m.input)
	}
	m = press(t, m, "ctrl+c")
	if !m.quitting {
		t.Fatal("ctrl+c must quit")
	}
}

func TestInteractiveBackspaceEditsTheComposer(t *testing.T) {
	m := newModel(nil)
	m.interactive = true
	m.prompts = make(chan string, 1)
	m = press(t, m, "backspace")
	if m.input != "" {
		t.Fatalf("empty backspace=%q", m.input)
	}
	m = typeKeys(t, m, "ab")
	m = press(t, m, "ctrl+h")
	if m.input != "a" {
		t.Fatalf("input=%q", m.input)
	}
}

func TestInteractiveSlashSwitchesModelWithoutStartingATurn(t *testing.T) {
	prompts := make(chan string, 1)
	m := newModel(nil)
	m.interactive = true
	m.prompts = prompts
	m.choice = &Switcher{
		Model:   "alpha",
		Catalog: []string{"alpha", "beta"},
		Levels:  reasoningCycle([]string{"low", "medium", "high"}),
	}
	m = typeKeys(t, m, "/model beta")
	next, cmd := m.Update(keyMsg("enter"))
	m = next.(swarmTUI)
	if cmd != nil || m.busy {
		t.Fatal("/model must not start a swarm turn")
	}
	if m.choice.CurrentModel() != "beta" {
		t.Fatalf("model=%q", m.choice.CurrentModel())
	}
	if !strings.Contains(m.notice, "beta") {
		t.Fatalf("notice=%q", m.notice)
	}
	select {
	case got := <-prompts:
		t.Fatalf("slash leaked a prompt %q", got)
	default:
	}
	m = typeKeys(t, m, "/model missing")
	m = press(t, m, "enter")
	if m.choice.CurrentModel() != "beta" || !strings.Contains(m.notice, "missing") {
		t.Fatalf("unknown model notice=%q model=%q", m.notice, m.choice.CurrentModel())
	}
}

func TestShiftTabCyclesReasoningOnTheStatusLine(t *testing.T) {
	m := newModel(nil)
	m.interactive = true
	m.prompts = make(chan string, 1)
	m.choice = &Switcher{
		Model:     "alpha",
		Reasoning: "",
		Catalog:   []string{"alpha"},
		Levels:    reasoningCycle([]string{"low", "medium", "high"}),
	}
	m.width, m.height = 80, 24
	if view := m.View(); !strings.Contains(view, "alpha") || !strings.Contains(view, "default") {
		t.Fatalf("status missing from idle view:\n%s", view)
	}
	m = press(t, m, "shift+tab")
	if m.choice.CurrentReasoning() != "low" {
		t.Fatalf("reason=%q", m.choice.CurrentReasoning())
	}
	if !strings.Contains(m.View(), "low") {
		t.Fatalf("cycled level missing:\n%s", m.View())
	}
	m = typeKeys(t, m, "/reason high")
	m = press(t, m, "enter")
	if m.choice.CurrentReasoning() != "high" || m.busy {
		t.Fatalf("reason=%q busy=%v", m.choice.CurrentReasoning(), m.busy)
	}
}

func TestSlashBareModelAndReasonOpenAPicker(t *testing.T) {
	m := newModel(nil)
	m.interactive = true
	m.prompts = make(chan string, 1)
	m.width, m.height = 80, 24
	m.choice = &Switcher{
		Model:     "alpha",
		Reasoning: "",
		Catalog:   []string{"alpha", "beta"},
		Levels:    reasoningCycle([]string{"low", "medium", "high"}),
	}
	m = typeKeys(t, m, "/model")
	m = press(t, m, "enter")
	if m.input != "/model " || m.choice.CurrentModel() != "alpha" {
		t.Fatalf("bare /model should open the picker, input=%q model=%q", m.input, m.choice.CurrentModel())
	}
	m = press(t, m, "down")
	m = press(t, m, "enter")
	if m.choice.CurrentModel() != "beta" {
		t.Fatalf("picker select model=%q", m.choice.CurrentModel())
	}
	m = typeKeys(t, m, "/think")
	m = press(t, m, "enter")
	if m.input != "/reason " {
		t.Fatalf("bare /think should open the reason picker, input=%q", m.input)
	}
	m = press(t, m, "esc")
	m = typeKeys(t, m, "/reasoning default")
	m = press(t, m, "enter")
	if m.choice.CurrentReasoning() != "" {
		t.Fatalf("default reason=%q", m.choice.CurrentReasoning())
	}
	m = typeKeys(t, m, "/reason bogus")
	m = press(t, m, "enter")
	if !strings.Contains(m.notice, "bogus") {
		t.Fatalf("unknown reason notice=%q", m.notice)
	}
	m.busy = true
	m = press(t, m, "shift+tab")
	if m.choice.CurrentReasoning() != "low" {
		t.Fatalf("shift+tab while running should still queue the next level, got %q", m.choice.CurrentReasoning())
	}
	m.busy = false
	m.choice = nil
	m = press(t, m, "shift+tab")
	m = press(t, m, "esc")
	m = typeKeys(t, m, "/model")
	m = press(t, m, "enter")
	if !strings.Contains(m.notice, "no model switcher") && m.input != "/model " {
		t.Fatalf("nil switcher notice=%q input=%q", m.notice, m.input)
	}
}

func TestInteractiveComposerKeepsAgentKeys(t *testing.T) {
	m := newModel(nil)
	m.interactive = true
	m.prompts = make(chan string, 1)
	feed(&m, swarm.Notification{Kind: swarm.NotifySpawned, AgentID: "w1", Role: "a"})
	m = press(t, m, "tab")
	if m.selected != 0 {
		t.Fatalf("tab should still walk the roster while typing, selected=%d", m.selected)
	}
	m = press(t, m, "esc")
	if m.selected != -1 {
		t.Fatalf("esc should return to the manager, selected=%d", m.selected)
	}
}

func TestInteractiveBusyViewSaysTheTurnIsRunning(t *testing.T) {
	m := newModel(nil)
	m.interactive = true
	m.busy = true
	m.width, m.height = 80, 24
	if view := m.View(); !strings.Contains(view, "running") {
		t.Fatalf("busy composer is missing:\n%s", view)
	}
}

func TestSubmitWithoutAPromptChannelIsANoop(t *testing.T) {
	m := newModel(nil)
	m.interactive = true
	m.input = "look into it"
	next, cmd := m.Update(keyMsg("enter"))
	m = next.(swarmTUI)
	if m.busy || cmd != nil {
		t.Fatal("a composer with nowhere to send must not start a turn")
	}
}

func TestInteractiveBusyEnterStillTogglesTools(t *testing.T) {
	m := newModel(nil)
	m.interactive = true
	m.busy = true
	feed(&m,
		swarm.Notification{Kind: swarm.NotifyToolCall, AgentID: swarm.DefaultManagerID, Text: "ls(.)"},
		swarm.Notification{Kind: swarm.NotifyToolResult, AgentID: swarm.DefaultManagerID, Text: "one file"},
	)
	m = press(t, m, "enter")
	if !blocksOfKind(m.manager, blockTool)[0].open {
		t.Fatal("enter during a run should still unfold tools, not try to send")
	}
}

func TestInteractiveViewShowsTheComposer(t *testing.T) {
	m := newModel(nil)
	m.interactive = true
	m.width, m.height = 80, 24
	view := m.View()
	if !strings.Contains(view, "type a task") {
		t.Fatalf("idle composer is missing:\n%s", view)
	}
	if strings.Contains(view, "q quit") {
		t.Fatal("q must not quit while the composer is capturing letters")
	}
	m.input = "hello"
	if view = m.View(); !strings.Contains(view, "hello") {
		t.Fatalf("typed text is missing:\n%s", view)
	}
}

func TestOpenComposerWithATaskLooksBusy(t *testing.T) {
	m := newModel(nil)
	m.width, m.height = 80, 24
	ch := m.openComposer("keep the standing objective")
	t.Cleanup(func() { close(ch) })
	if !m.interactive || !m.busy || m.prompts == nil {
		t.Fatalf("interactive=%v busy=%v prompts=%v", m.interactive, m.busy, m.prompts != nil)
	}
	view := m.View()
	if strings.Contains(view, "Waiting for a task") {
		t.Fatalf("a launched goal must not idle at the composer:\n%s", view)
	}
	if !strings.Contains(view, "keep the standing objective") {
		t.Fatalf("the first user message must show:\n%s", view)
	}
	if !strings.Contains(view, "running") {
		t.Fatalf("busy composer is missing:\n%s", view)
	}
}

func TestOpenComposerWithoutATaskWaits(t *testing.T) {
	m := newModel(nil)
	m.width, m.height = 80, 24
	ch := m.openComposer("  ")
	t.Cleanup(func() { close(ch) })
	if m.busy {
		t.Fatal("an empty launch must wait at the composer")
	}
	if !strings.Contains(m.View(), "Waiting for a task") {
		t.Fatalf("idle pane is missing:\n%s", m.View())
	}
}

func TestTickIdleClearsBusySoTheComposerReturns(t *testing.T) {
	ch := make(chan notificationMsg, 1)
	m := newModel(nil)
	m.interactive = true
	m.busy = true
	m.notifications = ch
	ch <- notificationMsg{idle: true}
	next, cmd := m.Update(tickMsg{})
	m = next.(swarmTUI)
	if m.busy {
		t.Fatal("idle must re-enable the composer")
	}
	if cmd == nil {
		t.Fatal("the tick must reschedule itself or the UI freezes")
	}
}

func TestInteractiveErrorShowsInThePane(t *testing.T) {
	m := newModel(nil)
	m.interactive = true
	m.width, m.height = 80, 24
	m.apply(swarm.Notification{
		Kind: swarm.NotifyError, AgentID: swarm.DefaultManagerID,
		Err: errors.New("the endpoint refused"),
	})
	pane := m.pane(m.manager, 80, 30)
	if !strings.Contains(pane, "the endpoint refused") {
		t.Fatalf("a failed turn must stay visible in the composer session:\n%s", pane)
	}
}

func TestSessionHitCapContinuesInsteadOfFailing(t *testing.T) {
	parent, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	run, stop := context.WithCancel(parent)
	stop()
	if !sessionHitCap(parent, run, errors.New("context canceled"), time.Second) {
		t.Fatal("a session timeout must be a yield, not a fatal error")
	}
	if sessionHitCap(parent, parent, errors.New("exceed max iteration"), 0) {
		t.Fatal("eino's ReAct slice is not a session time yield")
	}
	if !sessionHitIterationCap(parent, errors.New("exceed max iteration")) {
		t.Fatal("a ReAct slice must be recognized so --goal can extend in place")
	}
	if sessionHitIterationCap(parent, errors.New("the endpoint refused")) {
		t.Fatal("a real error is not a ReAct slice")
	}
	if sessionHitCap(parent, parent, errors.New("the endpoint refused"), time.Second) {
		t.Fatal("a real error must not look like a session yield")
	}
	dead, stopParent := context.WithCancel(context.Background())
	stopParent()
	if sessionHitCap(dead, dead, errors.New("context canceled"), time.Second) {
		t.Fatal("an interrupt of the parent is not a session yield")
	}
}
