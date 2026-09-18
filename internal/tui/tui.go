package tui

import (
	"fmt"
	"strings"
	"time"

	"github.com/LubyRuffy/eino-swarm"
	tea "github.com/charmbracelet/bubbletea"
)

// ---------- colors (shared with render.go; declared here once) ----------

// ---------- collapsible blocks (Codex/CC-style timeline) ----------

// blockKind is one transcript entry for an agent.
type blockKind int

const (
	blockThinking blockKind = iota // reasoning stream (auto-collapse on end)
	blockAnswer                    // assistant text (final or interim)
	blockTool                      // tool call + result summary
	blockUser                      // typed task in an interactive session
)

type block struct {
	kind       blockKind
	agentID    string
	thinkText  string // for blockThinking: full accumulated reasoning
	answer     string // for blockThinking/Answer: final text
	toolName   string // for blockTool
	toolArgs   string
	toolCallID string
	toolRes    string
	toolFailed bool
	open       bool // collapsed/expanded; thinking defaults open while streaming
	live       bool // still streaming (reasoning running)
}

// ---------- per-agent state ----------

type agentState struct {
	id     string
	role   string
	blocks []*block
	open   map[int]bool // per-block expanded override (nil = default)
	// live pointers during streaming:
	curThink  *block
	curAnswer *block
	curTool   *block
	finished  bool
	finErr    error
}

func (a *agentState) ensureThink() *block {
	if a.curThink == nil {
		a.curThink = &block{kind: blockThinking, agentID: a.id, open: true, live: true}
		a.blocks = append(a.blocks, a.curThink)
	}
	return a.curThink
}

func (a *agentState) ensureAnswer() *block {
	if a.curAnswer == nil {
		a.curAnswer = &block{kind: blockAnswer, agentID: a.id, open: true}
		a.blocks = append(a.blocks, a.curAnswer)
	}
	return a.curAnswer
}

// closeThinking ends the live reasoning block (auto-collapse).
func (a *agentState) closeThinking() {
	if a.curThink != nil {
		a.curThink.live = false
		a.curThink.open = false // Codex/CC behavior: collapse when thinking ends
		a.curThink = nil
	}
}

// sealAnswer ends the live answer cursor so the next delta cannot overwrite
// this block. The text stays; folding it to the first line is how a streamed
// body vanished the moment the model finished.
func (a *agentState) sealAnswer() {
	a.curAnswer = nil
}

// toolBlock finds the in-flight call this notification belongs to. Two execs
// can run at once; matching the last curTool would paint both streams onto
// one row.
func (a *agentState) toolBlock(callID string) *block {
	if callID != "" {
		for i := len(a.blocks) - 1; i >= 0; i-- {
			b := a.blocks[i]
			if b.kind == blockTool && b.toolCallID == callID {
				return b
			}
		}
	}
	return a.curTool
}

// ---------- swarm->UI state ----------

type swarmTUI struct {
	reg      *swarm.Registry
	manager  *agentState
	agents   []*agentState
	selected int // -1 manager, else agent index
	width    int
	height   int
	quitting bool
	final    string
	finErr   error
	// notifications from the swarm, bridged into Update
	notifications <-chan notificationMsg

	// Interactive session: the composer is open when idle. One-shot --task
	// runs leave these zero and keep the original keybindings.
	interactive bool
	busy        bool
	input       string
	prompts     chan string
	choice      *Switcher
	notice      string
	ime         *imeAnchor
	slashIndex  int
	slashQuery  string
	askHost     *AskHost
	ask         *askOverlay
	plan        *PlanState
}

// notificationMsg wraps a swarm.Notification as a bubbletea message.
// idle is a local signal (not a wire kind): the session loop finished a
// turn and is waiting for the next typed task.
type notificationMsg struct {
	swarm.Notification
	idle bool
}

func newModel(reg *swarm.Registry) swarmTUI {
	return swarmTUI{
		reg:      reg,
		manager:  &agentState{id: swarm.DefaultManagerID, role: "manager", open: map[int]bool{}},
		selected: -1,
	}
}

func (m *swarmTUI) byID(id string) *agentState {
	if id == swarm.DefaultManagerID {
		return m.manager
	}
	for _, a := range m.agents {
		if a.id == id {
			return a
		}
	}
	return nil
}

// apply mutates state from one notification.
func (m *swarmTUI) apply(n swarm.Notification) {
	// lifecycle first: Spawned registers a new agent so subsequent
	// notifications can find it via byID.
	switch n.Kind {
	case swarm.NotifySpawned:
		if a := m.byID(n.AgentID); a != nil {
			a.finished = false
			a.finErr = nil
			return
		}
		m.agents = append(m.agents, &agentState{
			id:   n.AgentID,
			role: n.Role,
			open: map[int]bool{},
		})
		return
	}
	a := m.byID(n.AgentID)
	if a == nil {
		return
	}
	switch n.Kind {
	case swarm.NotifyTurn:
		// turn boundary: the previous answer block is complete; the next
		// delta/answer opens a fresh block (keeps chronological order).
		a.sealAnswer()
		a.closeThinking()
	case swarm.NotifyReasoningDelta:
		blk := a.ensureThink()
		blk.thinkText = n.Text // accumulated
	case swarm.NotifyDelta:
		a.closeThinking() // answer started => collapse thinking
		blk := a.ensureAnswer()
		blk.answer = n.Text
	case swarm.NotifyAgentMessage:
		a.closeThinking()
		blk := a.ensureAnswer()
		blk.answer = n.Text
		a.sealAnswer()
	case swarm.NotifyToolCall:
		a.closeThinking()
		a.sealAnswer()
		name := toolNameOf(n.Text)
		blk := &block{
			kind:       blockTool,
			agentID:    a.id,
			open:       true, // expand while running
			toolName:   name,
			toolArgs:   summariseToolArgs(name, toolArgsOf(n.Text)),
			toolCallID: n.ToolCallID,
		}
		a.curTool = blk
		a.blocks = append(a.blocks, blk)
		if note := scheduleNotice(name, toolArgsOf(n.Text)); note != "" {
			m.notice = note
		}
	case swarm.NotifyToolDelta:
		if blk := a.toolBlock(n.ToolCallID); blk != nil {
			view := viewToolResult(blk.toolName, n.Text)
			blk.toolRes = view.display()
			blk.open = true
		}
	case swarm.NotifyToolResult:
		if blk := a.toolBlock(n.ToolCallID); blk != nil {
			view := viewToolResult(blk.toolName, n.Text)
			blk.toolRes = view.display()
			blk.toolFailed = view.failed
			blk.open = false // done: collapse to summary line
			if a.curTool == blk {
				a.curTool = nil
			}
		}
	case swarm.NotifyError:
		a.finished = true
		a.finErr = n.Err
	case swarm.NotifyFinished:
		a.finished = true
		a.finErr = n.Err
		a.closeThinking()
		a.sealAnswer()
		for _, b := range a.blocks {
			if b.kind == blockTool {
				b.open = false // run over: fold all tool blocks
			}
		}
		a.curTool = nil
	}
}

func toolNameOf(s string) string {
	if i := strings.Index(s, "("); i > 0 {
		return s[:i]
	}
	return s
}

func toolArgsOf(s string) string {
	if i := strings.Index(s, "("); i >= 0 {
		return strings.TrimSuffix(s[i+1:], ")")
	}
	return ""
}

// ---------- bubbletea model ----------

func (m swarmTUI) Init() tea.Cmd {
	if m.interactive {
		return tea.Batch(tickCmd(), tea.ShowCursor)
	}
	return tickCmd()
}

type tickMsg struct{}

func tickCmd() tea.Cmd {
	return tea.Tick(80*time.Millisecond, func(time.Time) tea.Msg { return tickMsg{} })
}

func (m swarmTUI) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tickMsg:
		return m.onTick()
	case tea.KeyMsg:
		return m.onKey(msg)
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	}
	return m, nil
}

func (m swarmTUI) onTick() (tea.Model, tea.Cmd) {
	// drain pending notifications (non-blocking) then RE-SCHEDULE the
	// tick — without this the UI freezes after the first tick and the
	// swarm goroutine blocks when the channel buffer fills.
	for {
		select {
		case n, ok := <-m.notifications:
			if !ok {
				return m, tea.Quit
			}
			if n.idle {
				m.busy = false
				continue
			}
			m.apply(n.Notification)
			continue
		default:
		}
		break
	}
	if m.ask == nil {
		if p := m.askHost.take(); p != nil {
			m.ask = newAskOverlay(p)
		}
	}
	return m, tickCmd()
}

func (m *swarmTUI) currentAgent() *agentState {
	if m.selected == -1 {
		return m.manager
	}
	if m.selected < len(m.agents) {
		return m.agents[m.selected]
	}
	return m.manager
}

// liveTail returns the agent's most recent activity text for the roster:
// last tool result > current tool call > streaming answer tail > reasoning tail.
func (a *agentState) liveTail() string {
	if a.curTool != nil {
		if a.curTool.toolRes != "" {
			return lastLine(a.curTool.toolRes)
		}
		return a.curTool.toolName + " " + trunc(a.curTool.toolArgs, 40)
	}
	if a.curAnswer != nil && a.curAnswer.answer != "" {
		return lastLine(a.curAnswer.answer)
	}
	if a.curThink != nil && a.curThink.thinkText != "" {
		return lastLine(a.curThink.thinkText)
	}
	return ""
}

// toggleThinking flips open state of every thinking block of the agent.
func (m *swarmTUI) toggleThinking(a *agentState) {
	if a == nil {
		return
	}
	anyOpen := false
	for _, b := range a.blocks {
		if b.kind == blockThinking && b.open {
			anyOpen = true
		}
	}
	for _, b := range a.blocks {
		if b.kind == blockThinking {
			b.open = !anyOpen
		}
	}
}

// toggleTool flips open state of tool blocks of the agent.
func (m *swarmTUI) toggleTool(a *agentState) {
	if a == nil {
		return
	}
	anyOpen := false
	for _, b := range a.blocks {
		if b.kind == blockTool && b.open {
			anyOpen = true
		}
	}
	for _, b := range a.blocks {
		if b.kind == blockTool {
			b.open = !anyOpen
		}
	}
}

// DumpTranscript renders the whole conversation (manager + all workers) as
// plain text, printed after the altscreen closes so the run output survives
// program exit.
func (m swarmTUI) DumpTranscript() string {
	var b strings.Builder
	fmt.Fprintln(&b, "=== manager transcript ===")
	for _, blk := range m.manager.blocks {
		dumpBlock(&b, blk)
	}
	for i, a := range m.agents {
		st := "ok"
		if a.finErr != nil {
			st = "error: " + a.finErr.Error()
		}
		fmt.Fprintf(&b, "\n--- worker %d: %s (%s) ---\n", i+1, a.id, st)
		for _, blk := range a.blocks {
			dumpBlock(&b, blk)
		}
	}
	return b.String()
}

func dumpBlock(bld *strings.Builder, blk *block) {
	switch blk.kind {
	case blockThinking:
		fmt.Fprintf(bld, "\n%s%s\n", "💭 reasoning:", trunc(blk.thinkText, 400))
	case blockTool:
		line := blk.toolName
		if blk.toolRes != "" {
			line += " → " + firstLine(blk.toolRes)
		}
		fmt.Fprintln(bld, "⚙ "+line)
		if blk.open || blk.toolRes == "" {
			if blk.toolArgs != "" {
				fmt.Fprintln(bld, "   "+blk.toolArgs)
			}
			if blk.toolRes != "" {
				fmt.Fprintln(bld, "   result: "+blk.toolRes)
			}
		}
	case blockAnswer:
		fmt.Fprintln(bld, strings.TrimSpace(blk.answer))
	case blockUser:
		fmt.Fprintln(bld, "> "+strings.TrimSpace(blk.answer))
	}
}
