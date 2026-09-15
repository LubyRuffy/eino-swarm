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
	leftW := w*3/5 - 3
	rightW := w - leftW - 4
	bodyH := h - 2

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
	sep := cDim.Render(strings.Repeat("─", maxInt(0, w)))

	return lipgloss.JoinVertical(lipgloss.Left,
		bar,
		lipgloss.JoinHorizontal(lipgloss.Top,
			lipgloss.NewStyle().Width(leftW).Render(left),
			cDim.Render("│"),
			lipgloss.NewStyle().Width(rightW).Render(right),
		),
		sep,
	)
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
				// collapsed: one dim line with a preview
				preview := firstWords(blk.thinkText, 12)
				fmt.Fprintln(&b, cThink.Render("▸ 💭 thought ("+fmt.Sprint(len([]rune(blk.thinkText)))+") "+trunc(preview, w-10)))
			}
		case blockTool:
			if blk.open {
				fmt.Fprintln(&b, cTool.Render("▾ ⚙ "+blk.toolName+"  (enter to fold)"))
				if blk.toolArgs != "" {
					fmt.Fprintln(&b, cDim.Render("│ args: "+trunc(blk.toolArgs, w-8)))
				}
				if blk.toolRes != "" {
					fmt.Fprintln(&b, cTool.Render("│ ← "+trunc(blk.toolRes, w-8)))
				}
			} else {
				line := "▸ ⚙ " + blk.toolName
				if blk.toolRes != "" {
					line += " → " + firstWords(blk.toolRes, 10)
				} else if blk.toolArgs != "" {
					line += "(" + trunc(blk.toolArgs, 40) + ")"
				}
				fmt.Fprintln(&b, cTool.Render(trunc(line, w-2)))
			}
		case blockAnswer:
			if blk.open {
				lines := lastNLines(blk.answer, maxInt(3, h/3))
				for _, l := range lines {
					fmt.Fprintln(&b, cMsg.Render(trunc(l, w-2)))
				}
			} else {
				fmt.Fprintln(&b, cOK.Render("▸ "+trunc(firstLine(blk.answer), w-2)))
			}
		}
	}
	// streaming live line at bottom
	if a.curTool != nil && a.curTool.toolName == "wait_agents" {
		fmt.Fprintln(&b, cLive.Render("⏳ waiting for sub-agents…"))
	}
	if a.curThink != nil && a.curThink.thinkText != "" {
		fmt.Fprintln(&b, cLive.Render("💭 "+trunc(lastLine(a.curThink.thinkText), w-4)))
	}
	if a.curAnswer != nil && a.curAnswer.answer != "" {
		fmt.Fprintln(&b, cLive.Render("▌ "+trunc(lastLine(a.curAnswer.answer), w-4)))
	}
	return lipgloss.NewStyle().Width(w).Height(h).MaxHeight(h).Render(b.String())
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
