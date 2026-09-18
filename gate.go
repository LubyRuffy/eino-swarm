package swarm

import "sync"

// defaultMaxConcurrent is what a library caller gets when they leave
// MaxConcurrent unset. The app config default is separate (6) and is
// written onto the registry before the first spawn.
const defaultMaxConcurrent = 8

// slotGate is a resizable counting semaphore.
//
// A buffered channel plus sync.Once cannot grow: Settings raising
// max_concurrent used to leave queued workers blocked until a restart,
// because the first spawn minted the only channel the registry would
// ever have. Waiters sit on cond; resize broadcasts so a higher cap
// lets them in without anyone finishing. A lower cap does not kill
// in-flight workers — new acquires wait until held drops under it.
type slotGate struct {
	mu   sync.Mutex
	cond *sync.Cond
	cap  int
	held int
}

func newSlotGate(n int) *slotGate {
	if n <= 0 {
		n = defaultMaxConcurrent
	}
	g := &slotGate{cap: n}
	g.cond = sync.NewCond(&g.mu)
	return g
}

func (g *slotGate) acquire() {
	g.mu.Lock()
	for g.held >= g.cap {
		g.cond.Wait()
	}
	g.held++
	g.mu.Unlock()
}

func (g *slotGate) release() {
	g.mu.Lock()
	if g.held > 0 {
		g.held--
	}
	g.cond.Signal()
	g.mu.Unlock()
}

func (g *slotGate) resize(n int) {
	if n <= 0 {
		n = defaultMaxConcurrent
	}
	g.mu.Lock()
	g.cap = n
	g.cond.Broadcast()
	g.mu.Unlock()
}
