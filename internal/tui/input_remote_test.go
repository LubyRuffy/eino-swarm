package tui

import (
	"strings"
	"testing"
)

// A running turn still accepts a follow-up and an answer. The in-process
// screen does not: it has no engine queue to put a second line on.
func TestBusyTurnStillTypesAnAnswer(t *testing.T) {
	local := newModel(nil)
	local.interactive = true
	local.prompts = make(chan string, 1)
	local.busy = true
	local.input = "later"
	if _, cmd := local.Update(keyMsg("enter")); cmd != nil {
		t.Fatal("the in-process screen must not take a line while a turn runs")
	}

	m := newModel(nil)
	m.remote = true
	m.interactive = true
	m.prompts = make(chan string, 1)
	m.steers = make(chan string, 1)
	m.stops = make(chan struct{}, 1)
	m.input = "x"
	if _, cmd := m.Update(keyMsg("ctrl+x")); cmd != nil {
		t.Fatal("ctrl+x while idle must not stop a turn")
	}
	m.busy = true
	local.input = "hidden"
	if strings.Contains(local.View(), "hidden") {
		t.Fatal("the in-process screen hides the composer while a turn runs")
	}
	m.input = "queued"
	view := m.View()
	if !strings.Contains(view, "queued") || !strings.Contains(view, "alt+enter") {
		t.Fatal("a remote turn must keep the composer and name steer")
	}

	m.input = "later"
	_, cmd := m.Update(keyMsg("enter"))
	if cmd == nil {
		t.Fatal("a remote turn must accept a follow-up")
	}
	cmd()
	select {
	case got := <-m.prompts:
		if got != "later" {
			t.Fatalf("follow-up=%q", got)
		}
	default:
		t.Fatal("the follow-up never left the composer")
	}

	m.input = "   "
	if _, cmd := m.Update(keyMsg("alt+enter")); cmd != nil {
		t.Fatal("a blank steer is not a steer")
	}
	m.input = "now"
	_, cmd = m.Update(keyMsg("alt+enter"))
	if cmd == nil {
		t.Fatal("alt+enter must steer")
	}
	cmd()
	select {
	case got := <-m.steers:
		if got != "now" {
			t.Fatalf("steer=%q", got)
		}
	default:
		t.Fatal("the steer never left the composer")
	}
	select {
	case got := <-m.prompts:
		t.Fatalf("a steer was also queued as a line: %q", got)
	default:
	}

	m.input = "nope"
	_, cmd = m.Update(keyMsg("ctrl+x"))
	if cmd == nil || m.quitting {
		t.Fatal("ctrl+x must stop the turn and stay on the screen")
	}
	cmd()
	select {
	case <-m.stops:
	default:
		t.Fatal("the stop never left the composer")
	}
	if m.input != "nope" {
		t.Fatal("stopping must not send the draft")
	}

	m.setAsking(true)
	m.input = "the reply"
	_, cmd = m.Update(keyMsg("enter"))
	if cmd == nil {
		t.Fatal("an open question must accept an answer")
	}
	cmd()
	select {
	case got := <-m.prompts:
		if got != "the reply" {
			t.Fatalf("answer=%q", got)
		}
	default:
		t.Fatal("the answer never left the composer")
	}
	m = press(t, m, "ctrl+c")
	if !m.quitting {
		t.Fatal("ctrl+c must leave the screen")
	}
}
