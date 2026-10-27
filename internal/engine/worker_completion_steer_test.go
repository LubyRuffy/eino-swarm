package engine

import (
	"context"
	"errors"
	"testing"
	"time"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/cloudwego/eino/schema"
)

// Steer acknowledges its durable human message before a later collection
// waiter exists. That admission must consume the same single attempt as an
// already-installed waiter's Continue, without losing the queued direction.
func TestQueuedHumanSteerAdmitsOneDurableCollectionAttempt(t *testing.T) {
	for _, failAtomic := range []bool{false, true} {
		t.Run(map[bool]string{false: "queued-before-install", true: "atomic-decision-fails"}[failAtomic], func(t *testing.T) {
			e := newTestEngine(t)
			th, _ := e.CreateThread("queued collection direction", "", "")
			turn := plantUnfinishedTurn(t, e, th.ID, "Collect the assigned result.")
			reg := swarm.NewRegistry()
			defer reg.Close()
			if err := reg.PlantFinished(swarm.FinishedWorker{ID: "original-worker", Generation: 1, Result: "Verified terminal result."}); err != nil {
				t.Fatal(err)
			}
			rt := e.runtimeFor(th.ID)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			idle := make(chan struct{})
			if !rt.occupy(reg, cancel, turn.ID, idle) {
				t.Fatal("runtime was not claimed")
			}
			defer rt.release(cancel, idle, turn.ID)
			if err := rt.recordWorkerCollectionRetry(turn, []string{"original-worker"}); err != nil {
				t.Fatal(err)
			}
			if err := e.Steer(th.ID, "Collect the result using this new direction."); err != nil {
				t.Fatal(err)
			}
			if !reg.HasPendingSteers() {
				t.Fatal("acknowledged human input never reached the Registry")
			}
			pause, err := rt.newWorkerCollectionPause(turn, reg.PendingAgents(), 3, 4)
			if err != nil {
				t.Fatal(err)
			}
			if failAtomic {
				if err := e.store.DB().Exec("CREATE TRIGGER refuse_queued_consumption BEFORE INSERT ON events WHEN NEW.kind='model_retry' BEGIN SELECT RAISE(ABORT,'queued consumption unavailable'); END").Error; err != nil {
					t.Fatal(err)
				}
			}
			waitCtx, endWait := context.WithTimeout(ctx, time.Second)
			defer endWait()
			proceed, automatic, err := rt.waitForWorkerCollection(waitCtx, turn, reg, pause)
			if failAtomic {
				if err == nil || proceed || automatic || errors.Is(err, context.DeadlineExceeded) {
					t.Fatalf("failed queued decision admitted work: proceed=%v automatic=%v err=%v", proceed, automatic, err)
				}
			} else if err != nil || !proceed || automatic {
				t.Fatalf("acknowledged queued direction was lost: proceed=%v automatic=%v err=%v", proceed, automatic, err)
			}
			events, _ := e.store.ListTurnEvents(turn.ID)
			continued, retries := 0, 0
			for _, event := range events {
				if event.Kind == KindMaxIterationsContinued {
					continued++
				}
				if event.Kind == KindModelRetry {
					retries++
				}
				if event.Kind == KindWorkersCollectionResumed {
					t.Fatal("human input was mislabeled as an automatic worker completion")
				}
			}
			wantContinue, wantRetries := 1, 2
			if failAtomic {
				wantContinue, wantRetries = 0, 1
			}
			if continued != wantContinue || retries != wantRetries {
				t.Fatalf("queued decision lost atomic consumption: continued=%d retries=%d", continued, retries)
			}
			if used, err := e.workerCollectionContinued(turn); err != nil || !used {
				t.Fatalf("queued decision renewed an unconsumed allowance: %v %v", used, err)
			}
		})
	}
}

func TestQueuedWorkerOrRuntimeMessagesCannotGrantHumanCollectionAllowance(t *testing.T) {
	for _, source := range []string{"worker", store.MessageSourceRuntime, store.MessageSourceHuman} {
		t.Run(source, func(t *testing.T) {
			e := newTestEngine(t)
			th, _ := e.CreateThread("nonhuman queued message", "", "")
			turn := plantUnfinishedTurn(t, e, th.ID, "Collect the assigned result.")
			reg := swarm.NewRegistry()
			defer reg.Close()
			if err := reg.PlantFinished(swarm.FinishedWorker{ID: "original-worker", Generation: 1, Result: "Verified terminal result."}); err != nil {
				t.Fatal(err)
			}
			msg := schema.UserMessage("A queued update about the assigned work.")
			if source != "worker" {
				withMessageSource(msg, source)
			}
			if source == store.MessageSourceRuntime {
				swarm.SetSteerSeq(msg, 1)
			}
			// A source-only message without a durable steer identity is no
			// stronger than a worker notification. Actual human input has both.
			if !reg.SteerManagerMessage(msg) {
				t.Fatal("message was not queued")
			}
			rt := e.runtimeFor(th.ID)
			pause, err := rt.newWorkerCollectionPause(turn, reg.PendingAgents(), 3, 4)
			if err != nil {
				t.Fatal(err)
			}
			ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
			defer cancel()
			proceed, automatic, err := rt.waitForWorkerCollection(ctx, turn, reg, pause)
			if proceed || automatic || !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("nonhuman message renewed human collection budget: %v %v %v", proceed, automatic, err)
			}
			events, _ := e.store.ListTurnEvents(turn.ID)
			for _, event := range events {
				if event.Kind == KindMaxIterationsContinued || event.Kind == KindModelRetry {
					t.Fatalf("nonhuman message produced a human continuation receipt: %s", event.Kind)
				}
			}
		})
	}
}
