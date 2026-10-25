package engine

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/LubyRuffy/eino-swarm/internal/tools"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

var errManagerFixtureOwnerMismatch = errors.New("manager fixture does not own the executing registry and turn")

// Direct adapter tests still need the real runtime claim that production's
// StartTurn installs. This owns a Registry and releases it through the actual
// lifecycle rather than weakening publication's running/turn identity check.
func ownManagerTestFixture(ctx context.Context, rt *runtime, turnID string, reg *swarm.Registry) (context.Context, func()) {
	rt.mu.Lock()
	running := rt.running
	rt.mu.Unlock()
	if running {
		return ctx, func() {}
	}
	ctx, cancel := context.WithCancel(ctx)
	idle := make(chan struct{})
	rt.occupy(reg, cancel, turnID, idle)
	return ctx, func() { rt.release(cancel, idle, turnID) }
}

func (rt *runtime) runOwnedTestManager(ctx context.Context, turn *store.Turn, reg *swarm.Registry, acc *accumulator, set *tools.Set, pc *projectContext, messages []adk.Message) (swarm.RunResult, error) {
	rt.mu.Lock()
	ownerTurn, sameRegistry := rt.turnID, rt.reg == reg
	mismatch := rt.running && (ownerTurn != turn.ID || !sameRegistry)
	rt.mu.Unlock()
	if mismatch {
		return swarm.RunResult{}, fmt.Errorf("%w: running_turn=%q requested_turn=%q registry_matches=%t", errManagerFixtureOwnerMismatch, ownerTurn, turn.ID, sameRegistry)
	}
	ctx, release := ownManagerTestFixture(ctx, rt, turn.ID, reg)
	defer release()
	return rt.runManager(ctx, turn, reg, acc, set, pc, messages)
}

func (e *Engine) reportOwnedTestScheduleJSON(threadID, turnID, args string) (string, error) {
	reg := swarm.NewRegistry()
	defer reg.Close()
	ctx, release := ownManagerTestFixture(context.Background(), e.runtimeFor(threadID), turnID, reg)
	defer release()
	return e.reportScheduleContextJSON(ctx, threadID, turnID, args)
}

// A stale fixture owner must fail before the real graph can call a model or
// leave a notice in an inbox that the executing Registry cannot drain.
func TestManagerFixtureRejectsAnExistingDifferentOwnerBeforeModel(t *testing.T) {
	for _, mismatch := range []string{"registry", "turn"} {
		t.Run(mismatch, func(t *testing.T) {
			e := newTestEngine(t)
			th, err := e.CreateThread("manager fixture ownership", "", "")
			if err != nil {
				t.Fatal(err)
			}
			turn := mustStoredTurn(t, e, th.ID, store.Turn{})
			set := buildTestToolset(t, e, th.ID)
			cm := &coordinationModel{steps: []func([]*schema.Message) *schema.Message{
				func([]*schema.Message) *schema.Message { return schema.AssistantMessage("Work completed.", nil) },
			}}
			reg := e.newTurnRegistry(func(string, string) model.BaseChatModel { return cm }, set, nil)
			t.Cleanup(reg.Close)
			owner, ownerTurn := reg, turn.ID
			if mismatch == "registry" {
				owner = swarm.NewRegistry()
				t.Cleanup(owner.Close)
			} else {
				ownerTurn = "another-owned-turn"
			}
			rt := e.runtimeFor(th.ID)
			idle := make(chan struct{})
			if !rt.occupy(owner, func() {}, ownerTurn, idle) {
				t.Fatal("fixture owner was not established")
			}
			defer rt.release(func() {}, idle, ownerTurn)
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			acc := newAccumulator(e, th.ID, turn.ID, 0)
			_, err = rt.runOwnedTestManager(ctx, turn, reg, acc, set, nil, []adk.Message{schema.UserMessage("finish assigned work")})
			if !errors.Is(err, errManagerFixtureOwnerMismatch) {
				t.Fatalf("different owner was not explicitly refused: %v", err)
			}
			if cm.next != 0 {
				t.Fatalf("ownership refusal reached the model: calls=%d", cm.next)
			}
			rt.mu.Lock()
			unchanged := rt.running && rt.reg == owner && rt.turnID == ownerTurn
			rt.mu.Unlock()
			if !unchanged {
				t.Fatal("refusal changed the existing runtime owner")
			}
		})
	}
}
