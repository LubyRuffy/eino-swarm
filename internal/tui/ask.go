package tui

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/LubyRuffy/eino-swarm/internal/engine"
	tea "github.com/charmbracelet/bubbletea"
)

// AskHost is how ask_user reaches a human in the terminal. When Attached is
// false (piped stdin, tests without a TTY) the tool fails immediately instead
// of hanging on a prompt nobody will see.
type AskHost struct {
	Attached bool
	incoming chan *AskPrompt
}

// AskPrompt is one in-flight questionnaire.
type AskPrompt struct {
	Questions []engine.AskQuestion
	Reply     chan askReply
}

type askReply struct {
	answers engine.AskAnswers
	err     error
}

func NewAskHost(attached bool) *AskHost {
	return &AskHost{Attached: attached, incoming: make(chan *AskPrompt, 1)}
}

// Wait blocks until the human answers or ctx is cancelled.
func (h *AskHost) Wait(ctx context.Context, questions []engine.AskQuestion) (engine.AskAnswers, error) {
	if h == nil || !h.Attached {
		return nil, fmt.Errorf("ask_user: no human is attached to answer")
	}
	p := &AskPrompt{Questions: questions, Reply: make(chan askReply, 1)}
	select {
	case h.incoming <- p:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	select {
	case r := <-p.Reply:
		return r.answers, r.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (h *AskHost) take() *AskPrompt {
	if h == nil {
		return nil
	}
	select {
	case p := <-h.incoming:
		return p
	default:
		return nil
	}
}

type askOverlay struct {
	prompt *AskPrompt
	q      int
	picks  map[string]string
	other  string
}

func newAskOverlay(p *AskPrompt) *askOverlay {
	return &askOverlay{prompt: p, picks: map[string]string{}}
}

func (o *askOverlay) current() (engine.AskQuestion, bool) {
	if o == nil || o.prompt == nil || o.q < 0 || o.q >= len(o.prompt.Questions) {
		return engine.AskQuestion{}, false
	}
	return o.prompt.Questions[o.q], true
}

func (o *askOverlay) choices() []engine.AskOption {
	q, ok := o.current()
	if !ok {
		return nil
	}
	var out []engine.AskOption
	for _, opt := range q.Options {
		if strings.EqualFold(opt.ID, "other") {
			continue
		}
		out = append(out, opt)
	}
	return out
}

func (m swarmTUI) onAskKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.ask == nil {
		return m, nil
	}
	switch msg.String() {
	case "ctrl+c":
		m.ask.finish(nil, context.Canceled)
		m.ask = nil
		m.quitting = true
		return m, tea.Quit
	case "enter":
		m.finishAskOther()
		return m, nil
	case "backspace", "ctrl+h":
		m.ask.other = trimLastRune(m.ask.other)
		return m, nil
	default:
		if n, err := strconv.Atoi(msg.String()); err == nil && n >= 1 {
			if m.pickAsk(n - 1) {
				return m, nil
			}
		}
		if msg.Type == tea.KeyRunes {
			m.ask.other += string(msg.Runes)
		}
		return m, nil
	}
}

func (m *swarmTUI) pickAsk(idx int) bool {
	if m.ask == nil {
		return false
	}
	choices := m.ask.choices()
	if idx < 0 || idx >= len(choices) {
		return false
	}
	q, ok := m.ask.current()
	if !ok {
		return false
	}
	m.ask.picks[q.ID] = choices[idx].Label
	m.ask.other = ""
	m.ask.q++
	if m.ask.q >= len(m.ask.prompt.Questions) {
		m.finishAskPicks()
	}
	return true
}

func (m *swarmTUI) finishAskOther() {
	if m.ask == nil {
		return
	}
	text := strings.TrimSpace(m.ask.other)
	if text == "" {
		return
	}
	q, ok := m.ask.current()
	if !ok {
		return
	}
	m.ask.picks[q.ID] = text
	m.ask.other = ""
	m.ask.q++
	if m.ask.q >= len(m.ask.prompt.Questions) {
		m.finishAskPicks()
	}
}

func (m *swarmTUI) finishAskPicks() {
	if m.ask == nil || m.ask.prompt == nil {
		return
	}
	answers := engine.AskAnswers{}
	for _, q := range m.ask.prompt.Questions {
		text := strings.TrimSpace(m.ask.picks[q.ID])
		if text == "" {
			return
		}
		answers[q.ID] = engine.AskAnswer{Answers: []string{text}}
	}
	m.ask.finish(answers, nil)
	m.notice = "answered"
	m.ask = nil
}

func (o *askOverlay) finish(answers engine.AskAnswers, err error) {
	if o == nil || o.prompt == nil {
		return
	}
	select {
	case o.prompt.Reply <- askReply{answers: answers, err: err}:
	default:
	}
}

func (m swarmTUI) askView(w int) string {
	if m.ask == nil {
		return ""
	}
	var b strings.Builder
	fmt.Fprintln(&b, cTitle.Render("question"))
	for i, q := range m.ask.prompt.Questions {
		mark := " "
		if i == m.ask.q {
			mark = ">"
		}
		fmt.Fprintf(&b, "%s %s\n", mark, trunc(q.Prompt, maxInt(8, w-4)))
		if i != m.ask.q {
			if got := m.ask.picks[q.ID]; got != "" {
				fmt.Fprintln(&b, cDim.Render("    "+trunc(got, maxInt(8, w-6))))
			}
			continue
		}
		for n, opt := range m.ask.choices() {
			fmt.Fprintf(&b, "    %d. %s\n", n+1, trunc(opt.Label, maxInt(8, w-8)))
		}
		if m.ask.other != "" {
			fmt.Fprintln(&b, cMsg.Render("    other: "+trunc(m.ask.other, maxInt(8, w-12))))
		} else {
			fmt.Fprintln(&b, cDim.Render("    type for Other, or 1–n"))
		}
	}
	return cMsg.Render(trunc(strings.TrimRight(b.String(), "\n"), maxInt(1, w*20)))
}
