package remote

import (
	"errors"
	"testing"

	"github.com/LubyRuffy/eino-swarm/internal/wakeup"
)

type errWake struct{}

func (errWake) Set(bool) error { return errors.New("denied") }
func (errWake) On() bool       { return false }
func (errWake) Close()         {}

func TestHostKeepAwakeFollowsPairingAndSurvivesReload(t *testing.T) {
	e := testEngine(t)
	cfg := e.Config()
	rec := wakeup.NewRecorder()
	h := New(e, cfg, nil)
	h.wake = rec

	cfg.Remote.Enabled = true
	cfg.Remote.KeepAwake = true
	h.Start()
	if !rec.On() || !h.Status().Awake {
		t.Fatal("pairing on must hold the assertion even when the hub is missing")
	}
	if !h.Status().KeepAwake {
		t.Fatal("status must echo the switch")
	}

	rec.Reset()
	h.Reload()
	for i, on := range rec.Sets() {
		if !on {
			t.Fatalf("reload released the assertion at set %d: %v", i, rec.Sets())
		}
	}
	if !rec.On() {
		t.Fatal("reload must still be holding")
	}

	cfg.Remote.KeepAwake = false
	h.Reload()
	if rec.On() || h.Status().Awake || h.Status().KeepAwake {
		t.Fatal("switch off must release")
	}

	cfg.Remote.KeepAwake = true
	cfg.Remote.Enabled = false
	h.Start()
	if rec.On() {
		t.Fatal("pairing off must not hold")
	}

	h.Stop()
	if rec.On() {
		t.Fatal("stop must release")
	}
}

func TestHostKeepAwakeOffByDefaultIsStillOff(t *testing.T) {
	e := testEngine(t)
	cfg := e.Config()
	if !cfg.Remote.KeepAwake {
		t.Fatal("a fresh config defaults keep-awake on")
	}
	rec := wakeup.NewRecorder()
	h := New(e, cfg, nil)
	h.wake = rec
	h.Start()
	if rec.On() {
		t.Fatal("pairing off must not caffeinate")
	}
	h.Stop()
}

func TestHostKeepAwakeNilHolderAndFailedSetDoNotPanic(t *testing.T) {
	e := testEngine(t)
	cfg := e.Config()
	cfg.Remote.Enabled = true
	cfg.Remote.KeepAwake = true
	h := New(e, cfg, nil)
	h.wake = nil
	h.Start()
	if h.Status().Awake {
		t.Fatal("nil holder is not held")
	}
	h.wake = errWake{}
	h.Start()
	if h.Status().Awake {
		t.Fatal("a failed hold must not look held")
	}
	h.Stop()
}
