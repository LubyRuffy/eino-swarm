package tools

import (
	"context"
	"time"
)

const managedExecAdaptiveWindow = 5 * time.Second

type managedExecStandaloneCall struct {
	id   string
	wait bool
}

// NoteStandaloneModelCall identifies the only tool call in a completed model
// response. A single tool invocation cannot observe the rest of its batch.
func (s *ManagedExecSessions) NoteStandaloneModelCall(callID string, wait bool) {
	s.mu.Lock()
	s.standalone = managedExecStandaloneCall{id: callID, wait: wait}
	s.mu.Unlock()
}

func (s *ManagedExecSessions) learnAdaptiveObservation(ctx context.Context, callID string, view managedExecView) {
	if callID == "" || view.Status != "exited" || view.ExitCode == nil ||
		view.elapsed <= managedExecObservationWindow || view.elapsed > managedExecAdaptiveWindow {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	original := s.calls[view.OriginalCallID]
	if ctx.Err() == nil && s.ctx.Err() == nil &&
		s.standalone == (managedExecStandaloneCall{id: callID, wait: true}) &&
		original.standaloneRequest != 0 && original.standaloneRequest+1 == s.request &&
		original.sessionID == view.SessionID {
		s.adaptiveObservation = true
	}
}

// AwaitAdaptiveObservation lets an already-yielded standalone command finish
// before another model request. Canceling this observation leaves the process
// with the turn owner. Terminal delivery remains the caller's responsibility.
func (s *ManagedExecSessions) AwaitAdaptiveObservation(ctx context.Context) error {
	s.mu.Lock()
	request := s.request
	job := s.active[s.standalone.id]
	eligible := s.adaptiveObservation && !s.standalone.wait && len(s.active) == 1 &&
		job != nil && job.yielded && s.calls[job.callID].standaloneRequest == request
	if eligible {
		s.standalone = managedExecStandaloneCall{}
	}
	s.mu.Unlock()
	if !eligible {
		return nil
	}
	timer := time.NewTimer(time.Until(job.command.started.Add(managedExecAdaptiveWindow)))
	defer timer.Stop()
	expired := false
	select {
	case <-job.done:
	case <-ctx.Done():
	case <-s.ctx.Done():
	case <-timer.C:
		expired = true
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.ctx.Err(); err != nil {
		return err
	}
	if !expired {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	select {
	case <-job.done:
	default:
		if s.request == request && s.active[job.callID] == job {
			s.adaptiveObservation = false
		}
	}
	return nil
}
