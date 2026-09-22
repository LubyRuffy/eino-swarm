package tui

import (
	"fmt"
	"strings"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/charmbracelet/lipgloss"
)

var (
	cTitle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("39"))
	cDim    = lipgloss.NewStyle().Foreground(lipgloss.Color("243"))
	cThink  = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
	cTool   = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	cToolRe = lipgloss.NewStyle().Foreground(lipgloss.Color("114"))
	cMsg    = lipgloss.NewStyle().Foreground(lipgloss.Color("252"))
	cOK     = lipgloss.NewStyle().Foreground(lipgloss.Color("42"))
	cErr    = lipgloss.NewStyle().Foreground(lipgloss.Color("203"))
	cLive   = lipgloss.NewStyle().Foreground(lipgloss.Color("51"))
	cSelBg  = lipgloss.NewStyle().Background(lipgloss.Color("236"))
)

func trunc(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

func lastNLines(s string, n int) []string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) <= n {
		return lines
	}
	return lines[len(lines)-n:]
}

// View: two panes — left = current agent transcript (collapsible blocks),
// right = sub-agent roster with live status.
func (m swarmTUI) View() string {
	w, h := m.width, m.height
	if w == 0 {
		w, h = 110, 34
	}
	menu := ""
	if m.interactive && (!m.busy || m.remote) && m.ask == nil {
		menu = m.slashMenu(w)
	}
	leftW := w*3/5 - 3
	rightW := w - leftW - 4
	chrome := 2
	if m.ask != nil {
		chrome = 8
	} else if m.interactive {
		chrome = 5
		if menu != "" {
			chrome += strings.Count(menu, "\n") + 1
		}
	}
	bodyH := h - chrome

	var left, right string
	if m.selected == -1 {
		left = m.pane(m.manager, leftW, bodyH)
	} else if m.selected < len(m.agents) {
		left = m.pane(m.agents[m.selected], leftW, bodyH)
	} else {
		left = m.pane(m.manager, leftW, bodyH)
	}
	right = m.roster(rightW, bodyH)

	bar := cTitle.Render(" swarm-tui ") + cDim.Render("←/→ agent · t thinking · enter tools · esc manager · q quit")
	if m.interactive && (m.remote || !m.busy) {
		hint := "enter send · / commands · shift+tab reason · ctrl+c quit"
		if m.remote {
			hint = "enter send · alt+enter steer · ctrl+x stop · ctrl+c quit"
		}
		bar = cTitle.Render(" swarm-tui ") + cDim.Render(hint)
	}
	sep := cDim.Render(strings.Repeat("─", maxInt(0, w)))

	parts := []string{
		bar,
		lipgloss.JoinHorizontal(lipgloss.Top,
			lipgloss.NewStyle().Width(leftW).Render(left),
			cDim.Render("│"),
			lipgloss.NewStyle().Width(rightW).Render(right),
		),
		sep,
	}
	if m.ask != nil {
		parts = append(parts, m.askView(w))
	} else if m.interactive {
		if menu != "" {
			parts = append(parts, menu)
		}
		parts = append(parts, m.composer(w))
		if sb := m.switchBar(w); sb != "" {
			parts = append(parts, sb)
		}
	}
	out := lipgloss.JoinVertical(lipgloss.Left, parts...)
	m.publishIME(out)
	return out
}

func (m swarmTUI) composer(w int) string {
	// A remote turn still takes a follow-up, a steer, or a stop. The
	// in-process screen has nowhere to put that line, so it only shows status.
	if m.busy && !m.remote {
		return cDim.Render(trunc("running…", maxInt(1, w)))
	}
	prefix := cTitle.Render(composerPrompt)
	if strings.TrimSpace(m.input) == "" {
		return prefix + cDim.Render("type a task, then enter")
	}
	return prefix + cMsg.Render(m.composerShown())
}

func (m swarmTUI) switchBar(w int) string {
	line := ""
	if m.choice != nil {
		line = m.choice.Line()
	}
	if strings.TrimSpace(m.notice) != "" {
		line = m.notice
	}
	if line == "" {
		return ""
	}
	return cDim.Render(trunc(line, maxInt(1, w)))
}

// pane renders one agent's transcript with collapsible blocks.
func (m swarmTUI) pane(a *agentState, w, h int) string {
	if a == nil {
		return ""
	}
	var b strings.Builder
	name := a.id
	if a.id == swarm.DefaultManagerID {
		name = cTitle.Render("MANAGER")
	} else {
		name = cTitle.Render(name)
	}
	fmt.Fprintln(&b, name+"  "+cDim.Render(strings.Repeat("─", maxInt(0, w-lipgloss.Width(name)-6))))

	if a.finErr != nil {
		fmt.Fprintln(&b, cErr.Render("error: "+trunc(a.finErr.Error(), maxInt(8, w-8))))
	}

	if len(a.blocks) == 0 && a.finErr == nil && m.interactive && !m.busy && a.id == swarm.DefaultManagerID {
		fmt.Fprintln(&b, cDim.Render("Waiting for a task."))
	}

	for _, blk := range a.blocks {
		// internal control tools are not part of the visible conversation
		if blk.kind == blockTool && (blk.toolName == "wait_agents" || blk.toolName == "close_agent") {
			continue
		}
		switch blk.kind {
		case blockThinking:
			if blk.open {
				// expanded: show tail lines of the accumulated reasoning
				head := "💭 thinking"
				if blk.live {
					head += "…"
				}
				fmt.Fprintln(&b, cTitle.Render("▾ "+head+"  (t to fold)"))
				for _, line := range lastNLines(blk.thinkText, maxInt(2, h/4)) {
					fmt.Fprintln(&b, cThink.Render("│ "+trunc(line, w-4)))
				}
			} else {
				// Collapsed preview is the tail they were watching. The start
				// of a long thought is usually setup; using it made the live
				// conclusion vanish into a one-line fold.
				preview := lastLine(blk.thinkText)
				fmt.Fprintln(&b, cThink.Render("▸ 💭 thought ("+fmt.Sprint(len([]rune(blk.thinkText)))+") "+trunc(preview, w-10)))
			}
		case blockTool:
			style := cTool
			if blk.toolFailed {
				style = cErr
			}
			if blk.open {
				fmt.Fprintln(&b, style.Render("▾ ⚙ "+blk.toolName+"  (enter to fold)"))
				if blk.toolArgs != "" {
					fmt.Fprintln(&b, cDim.Render("│ "+trunc(blk.toolArgs, w-8)))
				}
				if blk.toolRes != "" {
					resStyle := cToolRe
					if blk.toolFailed {
						resStyle = cErr
					}
					for _, line := range lastNLines(blk.toolRes, maxInt(2, h/4)) {
						fmt.Fprintln(&b, resStyle.Render("│ ← "+trunc(line, w-8)))
					}
				}
			} else {
				line := "▸ ⚙ " + blk.toolName
				if blk.toolArgs != "" {
					line += " " + trunc(blk.toolArgs, 40)
				}
				if blk.toolRes != "" {
					line += " → " + firstWords(blk.toolRes, 10)
				}
				fmt.Fprintln(&b, style.Render(trunc(line, w-2)))
			}
		case blockAnswer:
			if a.curAnswer == blk {
				// live: follow the tail so arriving tokens stay on screen
				lines := lastNLines(blk.answer, maxInt(3, h/3))
				for _, l := range lines {
					fmt.Fprintln(&b, cMsg.Render(trunc(l, w-2)))
				}
			} else {
				for _, l := range strings.Split(strings.TrimRight(blk.answer, "\n"), "\n") {
					fmt.Fprintln(&b, cMsg.Render(trunc(l, w-2)))
				}
			}
		case blockUser:
			fmt.Fprintln(&b, cTitle.Render("> "+trunc(blk.answer, maxInt(1, w-2))))
		}
	}
	// streaming live line at bottom
	if a.curTool != nil && a.curTool.toolName == "wait_agents" {
		fmt.Fprintln(&b, cLive.Render("⏳ waiting for sub-agents…"))
	}
	if a.curThink != nil && a.curThink.thinkText != "" {
		fmt.Fprintln(&b, cLive.Render("💭 "+trunc(lastLine(a.curThink.thinkText), w-4)))
	}
	body := strings.TrimRight(b.String(), "\n")
	// lipgloss Height crops the bottom. Live tokens arrive at the bottom, so
	// a full pane used to swallow the line the user was watching.
	return lipgloss.NewStyle().Width(w).MaxHeight(h).Render(pinBottom(body, h))
}

// rosters the right column.
func (m swarmTUI) roster(w, h int) string {
	var b strings.Builder
	fmt.Fprintln(&b, cTitle.Render(fmt.Sprintf("SUB-AGENTS (%d)", len(m.agents))))
	fmt.Fprintln(&b, cDim.Render(strings.Repeat("─", maxInt(0, w))))
	for i, a := range m.agents {
		marker := "  "
		if i == m.selected {
			marker = cTitle.Render("▶ ")
		}
		icon, status, st := "●", "streaming", cOK
		if a.finished {
			if a.finErr != nil {
				icon, status, st = "✗", "error", cErr
			} else {
				icon, status, st = "✓", "done", cOK
			}
		} else if a.curThink != nil {
			icon, status, st = "💭", "thinking", cThink
		} else if a.curTool != nil {
			icon, status, st = "⚙", "running "+a.curTool.toolName, cTool
		}
		fmt.Fprintln(&b, cSelRender(i == m.selected, fmt.Sprintf("%s%d. %s", marker, i+1, trunc(a.id, maxInt(8, w-6))), st))
		fmt.Fprintln(&b, cDim.Render(fmt.Sprintf("     %s %s", icon, status)))
		// live activity tail: latest reasoning/answer line, or last tool result
		if act := a.liveTail(); act != "" {
			fmt.Fprintln(&b, cDim.Render("     └ "+trunc(lastLine(act), maxInt(8, w-8))))
		}
	}
	return lipgloss.NewStyle().Width(w).Height(h).MaxHeight(h).Render(b.String())
}

func cSelRender(sel bool, s string, st lipgloss.Style) string {
	if sel {
		return cSelBg.Render(s)
	}
	return st.Render(s)
}

func firstWords(s string, n int) string {
	f := strings.Fields(s)
	if len(f) <= n {
		return s
	}
	return strings.Join(f[:n], " ") + "…"
}

func firstLine(s string) string {
	if i := strings.Index(s, "\n"); i >= 0 {
		return s[:i]
	}
	return s
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func lastLine(s string) string {
	// Streamed text often ends on a newline. Taking what follows it would show
	// the roster an empty activity line at exactly the moments it matters.
	s = strings.TrimRight(s, "\n")
	if i := strings.LastIndex(s, "\n"); i >= 0 {
		return s[i+1:]
	}
	return s
}

// pinBottom keeps the newest lines when the pane is full. Cropping from the
// top used to hide the live answer the moment thinking filled the window.
func pinBottom(s string, h int) string {
	s = strings.TrimRight(s, "\n")
	if h <= 0 || s == "" {
		return s
	}
	lines := strings.Split(s, "\n")
	if len(lines) <= h {
		return s
	}
	return strings.Join(lines[len(lines)-h:], "\n")
}
