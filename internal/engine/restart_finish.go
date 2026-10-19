package engine

import (
	"context"
	"errors"
	"fmt"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

// ErrRestartUnpersistedTurn means a released run has no durable terminal state.
var ErrRestartUnpersistedTurn = errors.New("engine: a completed turn has no saved terminal result")

// lockFinish orders normal teardown and its durable terminal receipt against
// handoff. Waiting never holds rt.mu, so a deadline can reject replacement
// while a completed turn is still writing its last records.
func (rt *runtime) lockFinish(ctx context.Context) (func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	rt.mu.Lock()
	if rt.finishGate == nil {
		rt.finishGate = make(chan struct{}, 1)
	}
	gate := rt.finishGate
	rt.mu.Unlock()
	select {
	case gate <- struct{}{}:
		return func() { <-gate }, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func lockRestartFinishes(ctx context.Context, runtimes []*runtime) (func(), error) {
	var unlocks []func()
	unlock := func() {
		for i := len(unlocks) - 1; i >= 0; i-- {
			unlocks[i]()
		}
	}
	for _, rt := range runtimes {
		release, err := rt.lockFinish(ctx)
		if err != nil {
			unlock()
			return nil, fmt.Errorf("wait for task completion before restart: %w", err)
		}
		unlocks = append(unlocks, release)
	}
	return unlock, nil
}

// A normal run can release its registry after storage rejected both terminal
// writes. Such a turn has no checkpoint owner and must not be silently replayed
// as unfinished work by a replacement process.
func (e *Engine) verifyReleasedTurn(rt *runtime) error {
	rt.mu.Lock()
	turnID := rt.turnID
	owned := rt.running || rt.reg != nil || rt.parked != nil
	rt.mu.Unlock()
	if turnID == "" || owned {
		return nil
	}
	turn, err := e.store.GetTurn(turnID)
	if errors.Is(err, store.ErrNotFound) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("verify completed turn before restart: %w", err)
	}
	if turn.Status != store.TurnRunning {
		return nil
	}
	events, err := e.store.ListTurnEvents(turnID)
	if err != nil {
		return fmt.Errorf("verify completed turn receipt before restart: %w", err)
	}
	for _, ev := range events {
		if !isManagerAgent(ev.AgentID) {
			continue
		}
		if ev.Kind == swarm.NotifyDone.String() || ev.Kind == swarm.NotifyError.String() || ev.Kind == KindUserStopRequested {
			return nil
		}
	}
	return fmt.Errorf("%w: %s", ErrRestartUnpersistedTurn, turnID)
}
