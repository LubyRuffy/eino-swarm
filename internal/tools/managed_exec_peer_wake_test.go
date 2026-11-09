package tools

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

type cancelAfterExecObservationCheck struct {
	context.Context
	cancel context.CancelFunc
}

func (c *cancelAfterExecObservationCheck) Err() error {
	err := c.Context.Err()
	c.cancel()
	return err
}

// Arbitration keeps the target's evidence and never consumes another job.
func TestManagedExecPeerWakeKeepsRevisionAndTerminalPriority(t *testing.T) {
	for _, name := range []string{"peer-ready", "target-terminal", "peer-pending", "peer-collected", "owner-cancelled", "invocation-cancelled"} {
		t.Run(name, func(t *testing.T) {
			owner := NewManagedExecSessions(t.Context())
			defer owner.cancel()
			progress := &managedExecProgress{callID: "target-call", revision: 3, started: time.Now(), stdout: []byte("retained"), limit: 4096}
			owner.observations["target-session"] = progress
			done := make(chan struct{})
			if name != "peer-pending" {
				close(done)
			}
			owner.active["peer-call"] = &managedExecSession{callID: "peer-call", sessionID: "peer-session", yielded: true, done: done}
			if name == "peer-collected" {
				delete(owner.active, "peer-call")
			}
			if name == "target-terminal" {
				progress.finished = time.Now()
				progress.exitCode = 7
				progress.status = "exited"
			}
			if name == "owner-cancelled" {
				owner.cancel()
			}
			observer := managedExecObserversForTest(t, owner)["wait_exec"]
			ctx, cancel := context.WithTimeout(t.Context(), time.Second)
			defer cancel()
			if name == "invocation-cancelled" {
				ctx = &cancelAfterExecObservationCheck{Context: ctx, cancel: cancel}
			}
			out, err := observer.InvokableRun(ctx, `{"session_id":"target-session","idle_timeout_ms":20,"after_revision":3}`)
			if name == "owner-cancelled" || name == "invocation-cancelled" {
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("owner cancellation lost: %q, %v", out, err)
				}
				return
			}
			var view struct {
				managedExecView
			}
			if err != nil || json.Unmarshal([]byte(out), &view) != nil {
				t.Fatalf("observation failed: %q, %v", out, err)
			}
			if view.SessionID != "target-session" || view.OriginalCallID != "target-call" || view.Revision != 3 || !view.Unchanged || view.Stdout != nil || view.Stderr != nil {
				t.Fatalf("target identity or revision/output references changed: %s", out)
			}
			switch name {
			case "peer-ready":
				if view.Code != "peer_exec_settled" || view.Status != "running" || view.ObservationTimedOut || view.ExitCode != nil || len(owner.active) != 1 {
					t.Fatalf("peer wake consumed or replaced a result: %s", out)
				}
			case "target-terminal":
				if view.Code != "" || view.Status != "exited" || view.ExitCode == nil || *view.ExitCode != 7 || view.ObservationTimedOut {
					t.Fatalf("peer hid target terminal: %s", out)
				}
			default:
				if view.Code != "" || view.Status != "running" || !view.ObservationTimedOut || view.ExitCode != nil {
					t.Fatalf("nonready peer changed idle timeout: %s", out)
				}
			}
		})
	}
}
