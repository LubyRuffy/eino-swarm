package tui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

func (m swarmTUI) onKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.interactive && !m.busy {
		return m.onComposerKey(msg)
	}
	return m.onNavKey(msg)
}

func (m swarmTUI) onComposerKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	items := m.slashItems()
	draft := slashDraft(m.input) != nil
	switch msg.String() {
	case "ctrl+c":
		m.quitting = true
		return m, tea.Quit
	case "enter":
		if len(items) > 0 {
			return m.pickSlash(items[m.clampedSlashIndex(len(items))], true)
		}
		return m, m.submit()
	case "tab":
		if len(items) > 0 {
			return m.pickSlash(items[m.clampedSlashIndex(len(items))], false)
		}
		if !draft {
			m.navigate("tab")
		}
		return m, nil
	case "up":
		if len(items) > 0 {
			m.slashIndex = nextSlashIndex(m.slashIndex, len(items), -1)
		}
		return m, nil
	case "down":
		if len(items) > 0 {
			m.slashIndex = nextSlashIndex(m.slashIndex, len(items), 1)
		}
		return m, nil
	case "esc":
		if draft {
			m.input = ""
			m.slashIndex = 0
			m.slashQuery = ""
			return m, nil
		}
		m.navigate("esc")
		return m, nil
	case "backspace", "ctrl+h":
		m.notice = ""
		m.input = trimLastRune(m.input)
		m.noteSlashQuery()
		return m, nil
	case "shift+tab":
		m.cycleReasoning()
		return m, nil
	case "right", "left":
		if !draft {
			m.navigate(msg.String())
		}
		return m, nil
	default:
		m.notice = ""
		if msg.Type == tea.KeyRunes {
			m.input += string(msg.Runes)
			m.noteSlashQuery()
		}
		return m, nil
	}
}

func (m swarmTUI) pickSlash(cmd slashCommand, run bool) (tea.Model, tea.Cmd) {
	if cmd.parent == "model" || cmd.parent == "reason" {
		arg := cmd.name
		if cmd.parent == "reason" && arg == "default" {
			arg = ""
		}
		m.input = ""
		m.slashQuery = ""
		m.slashIndex = 0
		m.notice = m.applyCommand(cmd.parent, arg)
		return m, nil
	}
	if !run {
		m.input = "/" + cmd.name
		if cmd.needsArg {
			m.input += " "
		}
		m.noteSlashQuery()
		return m, nil
	}
	switch cmd.name {
	case "model", "reason", "goal":
		m.input = "/" + cmd.name + " "
		m.noteSlashQuery()
		return m, nil
	case "quit", "exit":
		m.quitting = true
		m.input = ""
		return m, tea.Quit
	case "clear", "help":
		m.input = ""
		m.slashQuery = ""
		m.slashIndex = 0
		m.notice = m.applyCommand(cmd.name, "")
		return m, nil
	default:
		m.input = "/" + cmd.name
		return m, m.submit()
	}
}

func (m swarmTUI) onNavKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "ctrl+c", "q":
		m.quitting = true
		return m, tea.Quit
	case "tab", "right", "left", "esc":
		m.navigate(msg.String())
	case "t":
		m.toggleThinking(m.currentAgent())
	case "enter":
		m.toggleTool(m.currentAgent())
	case "shift+tab":
		m.cycleReasoning()
	}
	if len(msg.String()) == 1 && msg.String() >= "1" && msg.String() <= "9" {
		idx := int(msg.String()[0] - '1')
		if idx < len(m.agents) {
			m.selected = idx
		}
	}
	return m, nil
}

func (m *swarmTUI) navigate(key string) {
	switch key {
	case "tab", "right":
		if m.selected < len(m.agents)-1 {
			m.selected++
		} else {
			m.selected = -1
		}
	case "left":
		if m.selected == -1 {
			m.selected = len(m.agents) - 1
		} else {
			m.selected--
		}
	case "esc":
		m.selected = -1
	}
}

func (m *swarmTUI) submit() tea.Cmd {
	text := strings.TrimSpace(m.input)
	if text == "" || m.busy || m.prompts == nil {
		return nil
	}
	if name, arg, ok := parseTUICommand(text); ok {
		if name == "goal" {
			if arg == "" {
				m.input = "/goal "
				m.notice = "need an objective"
				return nil
			}
			text = arg
		} else {
			m.input = ""
			m.notice = m.applyCommand(name, arg)
			if m.quitting {
				return tea.Quit
			}
			return nil
		}
	}
	m.input = ""
	m.notice = ""
	m.busy = true
	m.manager.finished = false
	m.manager.finErr = nil
	m.manager.blocks = append(m.manager.blocks, &block{
		kind:    blockUser,
		agentID: m.manager.id,
		answer:  text,
		open:    true,
	})
	ch := m.prompts
	return func() tea.Msg {
		ch <- text
		return nil
	}
}

func (m *swarmTUI) cycleReasoning() {
	m.notice = ""
	if m.choice == nil {
		return
	}
	got := m.choice.CycleReasoning()
	m.notice = "thinking " + reasoningLabel(got)
}

func (m *swarmTUI) applyCommand(name, arg string) string {
	switch name {
	case "clear":
		m.manager.blocks = nil
		m.manager.curThink = nil
		m.manager.curAnswer = nil
		m.manager.curTool = nil
		m.manager.finished = false
		m.manager.finErr = nil
		m.agents = nil
		m.selected = -1
		return "cleared"
	case "help":
		m.manager.blocks = append(m.manager.blocks, &block{
			kind:    blockAnswer,
			agentID: m.manager.id,
			answer:  slashHelpText(),
			open:    true,
		})
		return ""
	case "quit", "exit":
		m.quitting = true
		return ""
	}
	if m.choice == nil {
		return "no model switcher"
	}
	switch name {
	case "model":
		if arg == "" {
			return "choose a model"
		}
		if err := m.choice.SetModel(arg); err != nil {
			return err.Error()
		}
		return "model " + m.choice.CurrentModel()
	case "reason", "reasoning", "think":
		if err := m.choice.SetReasoning(arg); err != nil {
			return err.Error()
		}
		return "thinking " + reasoningLabel(m.choice.CurrentReasoning())
	default:
		return ""
	}
}

func trimLastRune(s string) string {
	r := []rune(s)
	if len(r) == 0 {
		return ""
	}
	return string(r[:len(r)-1])
}
