package tui

import (
	"context"
	"strings"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/engine"
	tea "github.com/charmbracelet/bubbletea"
)

func TestAskHostWithoutAHumanFailsClosed(t *testing.T) {
	h := NewAskHost(false)
	_, err := h.Wait(context.Background(), nil)
	if err == nil || !strings.Contains(err.Error(), "no human") {
		t.Fatalf("piped stdin must not hang, got %v", err)
	}
}

func TestAskOverlayNumberPicksTheOption(t *testing.T) {
	reply := make(chan askReply, 1)
	m := newModel(nil)
	m.busy = true
	m.ask = newAskOverlay(&AskPrompt{
		Questions: []engine.AskQuestion{{
			ID:     "approach",
			Prompt: "Which approach should this work take?",
			Options: []engine.AskOption{
				{ID: "safer", Label: "Prefer the safer path"},
				{ID: "faster", Label: "Prefer the faster path"},
			},
		}},
		Reply: reply,
	})
	got, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("1")})
	fm, ok := got.(swarmTUI)
	if !ok {
		t.Fatalf("model type %T", got)
	}
	if fm.ask != nil {
		t.Fatal("the overlay must close after the last question")
	}
	select {
	case r := <-reply:
		if r.err != nil || r.answers["approach"].Answers[0] != "Prefer the safer path" {
			t.Fatalf("reply=%+v err=%v", r.answers, r.err)
		}
	default:
		t.Fatal("the tool is still waiting")
	}
}

func TestAskOverlayTypedTextIsOther(t *testing.T) {
	reply := make(chan askReply, 1)
	m := newModel(nil)
	m.ask = newAskOverlay(&AskPrompt{
		Questions: []engine.AskQuestion{{
			ID:      "approach",
			Prompt:  "Which approach?",
			Options: []engine.AskOption{{ID: "a", Label: "A"}, {ID: "b", Label: "B"}},
		}},
		Reply: reply,
	})
	next, _ := m.onAskKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("typed")})
	m = next.(swarmTUI)
	next, _ = m.onAskKey(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(swarmTUI)
	select {
	case r := <-reply:
		if r.answers["approach"].Answers[0] != "typed" {
			t.Fatalf("got %+v", r.answers)
		}
	default:
		t.Fatal("typed Other did not land")
	}
}
