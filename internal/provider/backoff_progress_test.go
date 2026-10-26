package provider

import (
	"context"
	"testing"
	"testing/synctest"
	"time"

	"github.com/cloudwego/eino/schema"
)

// Decoded output must release waiting conversations without letting their
// results settle the outage before the original recovery probe finishes.
func TestBackoffProbeContentReleasesWaitersWithoutSettlingHealth(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		gate := newBackoffGate(5*time.Second, 30*time.Second)
		mustAcquireBackoff(t, gate, nil).finish(backoffFailure)
		probe := mustAcquireBackoff(t, gate, nil)
		before := backoffProgressStateOf(gate)
		arrived := make(chan *backoffTicket, 3)
		for range cap(arrived) {
			go func() { arrived <- mustAcquireBackoff(t, gate, nil) }()
		}
		synctest.Wait()
		assertNoBackoffTicket(t, arrived)
		probe.progress()
		synctest.Wait()
		if gate.failures != before.failures || gate.delay != before.delay || gate.next != before.next || gate.generation != before.generation || !gate.probe || !gate.probeReady {
			t.Fatalf("content changed outage health instead of only releasing concurrency: %+v", backoffProgressStateOf(gate))
		}
		select {
		case <-before.changed:
		default:
			t.Fatal("content did not wake the existing gate waiters")
		}
		released := backoffProgressStateOf(gate)
		probe.progress()
		if got := backoffProgressStateOf(gate); got != released {
			t.Fatalf("repeated content changed the gate again: got=%+v want=%+v", got, released)
		}
		for _, outcome := range []backoffOutcome{backoffSuccess, backoffFailure, backoffCanceled} {
			ticket := <-arrived
			if ticket.probe {
				t.Fatal("a released parallel request claimed ownership of the probe")
			}
			ticket.finish(outcome)
			if got := backoffProgressStateOf(gate); got != released {
				t.Fatalf("parallel outcome %d changed the active probe's outage: got=%+v want=%+v", outcome, got, released)
			}
		}
		probe.finish(backoffSuccess)
		if gate.failures != 0 || gate.probe || gate.probeReady {
			t.Fatal("the actual probe's success did not settle its outage")
		}
	})
}

func TestBackoffLateReleasedResultsCannotChangeTheNextGeneration(t *testing.T) {
	for _, tc := range []struct {
		name     string
		outcome  backoffOutcome
		failures int
		cooldown time.Duration
	}{
		{"success", backoffSuccess, 0, 0},
		{"failure", backoffFailure, 2, 10 * time.Second},
		{"cancellation", backoffCanceled, 1, 5 * time.Second},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				gate := newBackoffGate(5*time.Second, 30*time.Second)
				mustAcquireBackoff(t, gate, nil).finish(backoffFailure)
				probe := mustAcquireBackoff(t, gate, nil)
				probe.progress()
				var parallel []*backoffTicket
				for range 3 {
					parallel = append(parallel, mustAcquireBackoff(t, gate, nil))
				}
				probe.finish(tc.outcome)
				if gate.failures != tc.failures || gate.probe || gate.probeReady || (tc.cooldown == 0 && !gate.next.IsZero()) || (tc.cooldown > 0 && time.Until(gate.next) != tc.cooldown) {
					t.Fatalf("probe outcome %d did not establish the expected next generation: %+v", tc.outcome, backoffProgressStateOf(gate))
				}
				settled := backoffProgressStateOf(gate)
				for i, outcome := range []backoffOutcome{backoffSuccess, backoffFailure, backoffCanceled} {
					parallel[i].finish(outcome)
				}
				probe.progress()
				if got := backoffProgressStateOf(gate); got != settled {
					t.Fatalf("late content/results changed the settled outage: got=%+v want=%+v", got, settled)
				}
				if tc.outcome == backoffSuccess {
					mustAcquireBackoff(t, gate, nil).finish(backoffFailure)
				}
				next := mustAcquireBackoff(t, gate, nil)
				waiting := backoffProgressStateOf(gate)
				probe.progress()
				if got := backoffProgressStateOf(gate); got != waiting || gate.probeReady {
					t.Fatalf("old probe content opened a newer probe: got=%+v want=%+v", got, waiting)
				}
				next.finish(backoffSuccess)
			})
		})
	}
}

func TestBackoffCancelAfterContentRenewsTheExistingCooldown(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		gate := newBackoffGate(5*time.Second, 30*time.Second)
		mustAcquireBackoff(t, gate, nil).finish(backoffFailure)
		probe := mustAcquireBackoff(t, gate, nil)
		probe.progress()
		probe.finish(backoffCanceled)
		arrived := make(chan *backoffTicket, 1)
		go func() { arrived <- mustAcquireBackoff(t, gate, nil) }()
		synctest.Wait()
		assertNoBackoffTicket(t, arrived)
		cooling := backoffProgressStateOf(gate)
		if cooling.failures != 1 || cooling.probeReady || cooling.probe || time.Until(cooling.next) != 5*time.Second {
			t.Fatalf("cancellation after output erased or skipped the cooldown: %+v", cooling)
		}
		time.Sleep(5 * time.Second)
		synctest.Wait()
		next := <-arrived
		if !next.probe || gate.probeReady {
			t.Fatal("the replacement request did not start as an exclusive probe")
		}
		next.finish(backoffSuccess)
	})
}

func TestBackoffContentWithoutAnActiveProbeDoesNotOpenTheGate(t *testing.T) {
	var nilTicket *backoffTicket
	nilTicket.progress()
	new(backoffTicket).progress()
	synctest.Test(t, func(t *testing.T) {
		gate := newBackoffGate(5*time.Second, 30*time.Second)
		healthy := mustAcquireBackoff(t, gate, nil)
		before := backoffProgressStateOf(gate)
		healthy.progress()
		if got := backoffProgressStateOf(gate); got != before {
			t.Fatal("ordinary healthy output created a recovery state")
		}
		healthy.finish(backoffFailure)
		before = backoffProgressStateOf(gate)
		inactive := &backoffTicket{gate: gate, generation: gate.generation, probe: true}
		inactive.progress()
		if got := backoffProgressStateOf(gate); got != before {
			t.Fatal("content without an admitted active probe released the cooldown")
		}
	})
}

func TestBackoffAdmissionProgressUsesTheCurrentReconnectTicket(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		gate := newBackoffGate(5*time.Second, 30*time.Second)
		mustAcquireBackoff(t, gate, nil).finish(backoffFailure)
		ctx, finish, err := (&recordingModel{gate: gate}).admit(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		generation := gate.generation
		if err := ctx.Value(reconnectBackoffKey{}).(func() error)(); err != nil {
			t.Fatal(err)
		}
		if gate.generation == generation || gate.failures != 2 {
			t.Fatal("reconnect did not acquire a new recovery generation")
		}
		ctx.Value(backoffProgressKey{}).(func())()
		if !gate.probeReady || !gate.probe || gate.failures != 2 {
			t.Fatal("decoded reconnect output did not release the current probe")
		}
		finish(nil)
		if gate.failures != 0 || gate.probeReady {
			t.Fatal("reconnected probe could not settle its own generation")
		}
	})
}

func TestBackoffCanceledAdmissionCannotReleaseContent(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		gate := newBackoffGate(5*time.Second, 30*time.Second)
		mustAcquireBackoff(t, gate, nil).finish(backoffFailure)
		ctx, cancel := context.WithCancel(context.Background())
		ctx, finish, err := (&recordingModel{gate: gate}).admit(ctx)
		if err != nil {
			t.Fatal(err)
		}
		before := backoffProgressStateOf(gate)
		cancel()
		ctx.Value(backoffProgressKey{}).(func())()
		if got := backoffProgressStateOf(gate); got != before {
			t.Fatal("Stop allowed a buffered content chunk to release the gate")
		}
		finish(context.Canceled)
		if gate.failures != 1 || gate.probe || gate.probeReady {
			t.Fatal("Stop changed recovery health or retained the probe")
		}
	})
}

func TestBackoffRecoveryContentRequiresDecodedOutput(t *testing.T) {
	index := 0
	for _, tc := range []struct {
		name string
		msg  *schema.Message
		want bool
	}{
		{"nil", nil, false},
		{"empty", &schema.Message{}, false},
		{"role", &schema.Message{Role: schema.Assistant}, false},
		{"usage", &schema.Message{ResponseMeta: &schema.ResponseMeta{Usage: &schema.TokenUsage{TotalTokens: 7}}}, false},
		{"finish reason", &schema.Message{ResponseMeta: &schema.ResponseMeta{FinishReason: "stop"}}, false},
		{"empty tool", &schema.Message{ToolCalls: []schema.ToolCall{{}}}, false},
		{"tool index and type", &schema.Message{ToolCalls: []schema.ToolCall{{Index: &index, Type: "function"}}}, false},
		{"body", &schema.Message{Content: "decoded body"}, true},
		{"reasoning", &schema.Message{ReasoningContent: "decoded reasoning"}, true},
		{"tool identity", &schema.Message{ToolCalls: []schema.ToolCall{{ID: "call-id"}}}, true},
		{"tool name", &schema.Message{ToolCalls: []schema.ToolCall{{Function: schema.FunctionCall{Name: "tool"}}}}, true},
		{"tool arguments", &schema.Message{ToolCalls: []schema.ToolCall{{Function: schema.FunctionCall{Arguments: "{"}}}}, true},
		{"later tool increment", &schema.Message{ToolCalls: []schema.ToolCall{{Index: &index}, {Function: schema.FunctionCall{Arguments: "}"}}}}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := hasRecoveryContent(tc.msg); got != tc.want {
				t.Fatalf("hasRecoveryContent(%+v)=%v want %v", tc.msg, got, tc.want)
			}
		})
	}
}

type backoffProgressState struct {
	failures   int
	delay      time.Duration
	next       time.Time
	probe      bool
	probeReady bool
	generation uint64
	changed    chan struct{}
}

func backoffProgressStateOf(gate *backoffGate) backoffProgressState {
	gate.mu.Lock()
	defer gate.mu.Unlock()
	return backoffProgressState{gate.failures, gate.delay, gate.next, gate.probe, gate.probeReady, gate.generation, gate.changed}
}
