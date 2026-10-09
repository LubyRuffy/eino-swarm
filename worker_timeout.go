package swarm

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// MaxConsecutiveWorkerTimeouts bounds repeated lifetimes of the same stalled worker.
const MaxConsecutiveWorkerTimeouts = 3

// WorkerTimeoutError distinguishes the worker watchdog from a provider idle timeout.
type WorkerTimeoutError struct {
	AgentID     string
	Timeout     time.Duration
	Consecutive int
	Err         error
}

func (e *WorkerTimeoutError) Error() string {
	return fmt.Sprintf("swarm: agent %s exceeded timeout %v (%d consecutive watchdog failures): %v", e.AgentID, e.Timeout, e.Consecutive, e.Err)
}
func (e *WorkerTimeoutError) Unwrap() error { return e.Err }

// WorkerTimeoutLimitError stops the manager rather than inviting another resume loop.
type WorkerTimeoutLimitError struct {
	AgentID  string
	Failures int
}

func (e *WorkerTimeoutLimitError) Error() string {
	return fmt.Sprintf("swarm: worker %s stopped after %d consecutive watchdog timeouts; automatic recovery is exhausted. Inspect the worker trace, model and task before explicitly starting again", e.AgentID, e.Failures)
}
func (e *WorkerTimeoutLimitError) Unwrap() error { return context.DeadlineExceeded }

func timeoutRecoveryError(id string, failures int) error {
	if failures >= MaxConsecutiveWorkerTimeouts {
		return &WorkerTimeoutLimitError{AgentID: id, Failures: failures}
	}
	return nil
}

func (h *Handle) completeWorker(result string, err error, timeout time.Duration, watchErr, runErr error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.result, h.err = result, err
	if err != nil && errors.Is(watchErr, context.DeadlineExceeded) && runErr == nil {
		h.timeoutFailures++
		h.err = &WorkerTimeoutError{AgentID: h.ID, Timeout: timeout, Consecutive: h.timeoutFailures, Err: err}
	} else {
		h.timeoutFailures = 0
		if err != nil && runErr != nil {
			h.err = fmt.Errorf("swarm: agent %s cancelled: %w", h.ID, err)
		}
	}
	h.finished = time.Now()
}

// ResetWorkerTimeouts is for an explicit human retry, never a model recovery tool.
// Running workers keep their current deadline and identity.
func (r *Registry) ResetWorkerTimeouts() {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, h := range r.agents {
		h.mu.Lock()
		h.timeoutFailures = 0
		h.mu.Unlock()
	}
	for _, p := range r.past {
		p.TimeoutFailures = 0
	}
}
