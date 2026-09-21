// Package wakeup holds a system sleep assertion so a phone can still
// reach this PC after the human walks away. Pairing without this is a
// dead socket the moment the lid timer fires.
package wakeup

import (
	"sync"
	"testing"
)

// Holder is the sleep assertion. Set is idempotent: the same state must
// not release and re-acquire. That bounce is how a host loses the race
// with idle sleep.
type Holder interface {
	Set(on bool) error
	On() bool
	Close()
}

// Wanted is pairing on and the keep-awake switch on. Hub reachability
// does not belong here — a reconnect must not drop the assertion.
func Wanted(enabled, keepAwake bool) bool {
	return enabled && keepAwake
}

// ForProcess is what the host installs. Tests get a no-op so `go test`
// does not caffeinate the machine running the suite.
func ForProcess() Holder {
	if testing.Testing() {
		return Nop()
	}
	return New()
}

// New uses the platform inhibitor (caffeinate / systemd-inhibit /
// SetThreadExecutionState). Tests that need a real starter mock startPlatform.
func New() Holder {
	return newMachine(startPlatform)
}

func newMachine(start func() (func(), error)) *machine {
	return &machine{start: start}
}

type machine struct {
	mu    sync.Mutex
	on    bool
	stop  func()
	start func() (func(), error)
}

func (m *machine) Set(on bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if on == m.on {
		return nil
	}
	if !on {
		m.dropLocked()
		return nil
	}
	stop, err := m.start()
	if err != nil {
		return err
	}
	m.stop = stop
	m.on = true
	return nil
}

func (m *machine) On() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.on
}

func (m *machine) Close() {
	_ = m.Set(false)
}

func (m *machine) dropLocked() {
	if m.stop != nil {
		m.stop()
		m.stop = nil
	}
	m.on = false
}

type nop struct {
	mu sync.Mutex
	on bool
}

// Nop records Set but never talks to the OS.
func Nop() Holder { return &nop{} }

func (n *nop) Set(on bool) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.on = on
	return nil
}

func (n *nop) On() bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.on
}

func (n *nop) Close() { _ = n.Set(false) }

// Recorder is a test double that keeps every Set call.
type Recorder struct {
	mu   sync.Mutex
	on   bool
	sets []bool
}

func NewRecorder() *Recorder { return &Recorder{} }

func (r *Recorder) Set(on bool) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sets = append(r.sets, on)
	r.on = on
	return nil
}

func (r *Recorder) On() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.on
}

func (r *Recorder) Close() { _ = r.Set(false) }

func (r *Recorder) Sets() []bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]bool, len(r.sets))
	copy(out, r.sets)
	return out
}

func (r *Recorder) Reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sets = nil
}
