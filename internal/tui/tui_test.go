// The terminal shell is the fallback when something misbehaves in the app, so
// its state machine is tested the same way: feed it the notification stream a
// real run produces and check what a user would end up looking at.
package tui

import (
	"errors"
	"strings"
	"testing"

	swarm "github.com/LubyRuffy/eino-swarm"
	tea "github.com/charmbracelet/bubbletea"
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

	ch <- notificationMsg{swarm.Notification{Kind: swarm.NotifySpawned, AgentID: "w1", Role: "researcher"}}
	ch <- notificationMsg{swarm.Notification{Kind: swarm.NotifyDelta, AgentID: "w1", Text: "working"}}
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
