package tools

import (
	"cmp"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"sync"
	"time"
)

const (
	managedExecObservationWindow = time.Second
	maxManagedExecSessions       = 16
)

// ManagedExecReceipt is the terminal result of an exec that yielded its call.
type ManagedExecReceipt struct {
	CallID    string
	SessionID string
	Arguments string
	Result    string
}

// ManagedExecSessions owns the processes of one manager turn.
type ManagedExecSessions struct {
	ctx    context.Context
	cancel context.CancelFunc

	mu      sync.Mutex
	next    uint64
	request uint64
	active  map[string]*managedExecSession
	changed chan struct{}
	retry   managedExecRetryGuard

	// Foreground results remain on their original ADK tool path.
	foregroundSettled bool

	standalone          managedExecStandaloneCall
	adaptiveObservation bool

	// Keep call identities after collection to reject replay and recognize a
	// delayed notification for the original running observation.
	calls map[string]managedExecCall

	observations map[string]*managedExecProgress
}

type managedExecCall struct {
	sessionID         string
	refused           bool
	standaloneRequest uint64
}

type managedExecSession struct {
	seq       uint64
	callID    string
	sessionID string
	arguments string
	identity  managedExecIdentity
	limit     int
	command   *managedExecCommand
	progress  *managedExecProgress
	cancel    context.CancelFunc
	done      chan struct{}
	yielded   bool // guarded by the session owner's mutex

	exitCode int // ready when done closes
	runErr   error
}

// NewManagedExecSessions keeps yielded commands owned by the parent turn.
func NewManagedExecSessions(owner context.Context) *ManagedExecSessions {
	ctx, cancel := context.WithCancel(owner)
	return &ManagedExecSessions{
		ctx: ctx, cancel: cancel,
		active: make(map[string]*managedExecSession), changed: make(chan struct{}),
		calls: make(map[string]managedExecCall), observations: make(map[string]*managedExecProgress),
	}
}

// AdvanceModelRequest starts an engine manager model request. The preceding
// ADK tool batch is joined, so its ordinary foreground results are now visible.
// It also separates retry responses and clears only the transient batch wake.
func (s *ManagedExecSessions) AdvanceModelRequest() {
	s.mu.Lock()
	s.request++
	s.standalone = managedExecStandaloneCall{}
	s.foregroundSettled = false
	s.mu.Unlock()
}

func (s *ManagedExecSessions) run(ctx context.Context, callID, args string, command *managedExecCommand, identity managedExecIdentity, limit int, yieldAfter time.Duration) (string, error) {
	defaultYield := yieldAfter == 0
	if defaultYield {
		yieldAfter = managedExecObservationWindow
	}
	job, err := s.start(ctx, callID, args, command, identity, limit, defaultYield)
	if err != nil {
		if repeat, ok := err.(*managedExecRepeatRefusal); ok {
			if repeat.terminal {
				return "", repeat
			}
			return repeat.result, nil
		}
		return refusedExecResult(err), nil
	}
	timer := time.NewTimer(yieldAfter)
	defer timer.Stop()
	select {
	case <-job.done:
	case <-ctx.Done():
		job.cancel()
		<-job.done
	case <-timer.C:
		// Cancellation that has already reached the foreground invocation
		// still owns cleanup. Once it yields, the turn owns cancellation.
		if ctx.Err() != nil || s.ctx.Err() != nil {
			job.cancel()
			<-job.done
		} else {
			s.mu.Lock()
			select {
			case <-job.done:
				s.mu.Unlock()
			default:
				job.yielded = true
				call := s.calls[callID]
				call.sessionID = job.sessionID
				s.calls[callID] = call
				s.observations[job.sessionID] = job.progress
				s.mu.Unlock()
				out, err := json.Marshal(map[string]any{
					"session_id": job.sessionID, "status": "running",
					"timeout_ms": command.timeoutMS, "elapsed_ms": time.Since(command.started).Milliseconds(),
				})
				return string(out), err
			}
		}
	}
	s.mu.Lock()
	delete(s.active, callID)
	s.mu.Unlock()
	return command.result("", job.exitCode, job.runErr)
}

func (s *ManagedExecSessions) start(ctx context.Context, callID, args string, command *managedExecCommand, identity managedExecIdentity, limit int, defaultYield bool) (*managedExecSession, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := s.ctx.Err(); err != nil {
		return nil, err
	}
	prior, used := s.calls[callID]
	if (!used || prior.refused) && s.retry.seq == s.next && s.retry.failure.identity == identity && s.retry.result != "" {
		terminal := !used && s.retry.refused && s.retry.refusedAt != s.request
		if !s.retry.refused {
			s.retry.refused, s.retry.refusedAt = true, s.request
		}
		s.calls[callID] = managedExecCall{refused: !terminal}
		return nil, &managedExecRepeatRefusal{failure: s.retry.failure, result: s.retry.result, terminal: terminal}
	}
	if used {
		return nil, errors.New("this tool call already owns a command")
	}
	if len(s.active) >= maxManagedExecSessions {
		return nil, fmt.Errorf("this turn already owns %d pending commands", maxManagedExecSessions)
	}
	progress := &managedExecProgress{callID: callID, revision: 1, timeoutMS: command.timeoutMS, limit: limit}
	command.stdout.progress, command.stderr.progress = progress, progress
	wait, cancel, err := command.start(s.ctx)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to start command: %w", err)
	}
	s.next++
	progress.started = command.started
	job := &managedExecSession{
		seq: s.next, callID: callID, sessionID: "exec-" + rand.Text(),
		arguments: args, identity: identity, limit: limit, command: command, progress: progress, cancel: cancel, done: make(chan struct{}),
	}
	s.active[callID] = job
	call := managedExecCall{}
	if defaultYield && s.standalone == (managedExecStandaloneCall{id: callID}) {
		call.standaloneRequest = s.request
	}
	s.calls[callID] = call
	go func() {
		job.exitCode, job.runErr = wait()
		command.finished = time.Now()
		cancel()
		s.mu.Lock()
		// An earlier concurrent exit cannot become the latest execution,
		// or join a retry streak after another command has started.
		if job.seq == s.next {
			s.retry.record(job)
		}
		progress.finish(command.finished, job.exitCode, job.runErr)
		close(job.done)
		if job.yielded || s.request > 0 {
			if !job.yielded {
				s.foregroundSettled = true
			}
			close(s.changed)
			s.changed = make(chan struct{})
		}
		s.mu.Unlock()
	}()
	return job, nil
}

// IsProvisional reports whether text is the running observation of this call.
func (s *ManagedExecSessions) IsProvisional(callID, text string) bool {
	s.mu.Lock()
	sessionID := s.calls[callID].sessionID
	s.mu.Unlock()
	if sessionID == "" {
		return false
	}
	var result struct {
		SessionID string `json:"session_id"`
		Status    string `json:"status"`
		ExitCode  *int   `json:"exit_code"`
	}
	return json.Unmarshal([]byte(text), &result) == nil && result.Status == "running" &&
		result.ExitCode == nil && result.SessionID == sessionID
}

// Pending reports whether a yielded call still needs its terminal receipt.
func (s *ManagedExecSessions) Pending() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, job := range s.active {
		if job.yielded {
			return true
		}
	}
	return false
}

// TakeReady claims only the already-completed yielded commands, in launch order.
func (s *ManagedExecSessions) TakeReady() ([]ManagedExecReceipt, error) {
	jobs, _, _ := s.readyJobs()
	return s.collect(context.Background(), jobs)
}

// Wait returns once at least one yielded command completes. Other running
// commands stay owned and can be collected by a later observation.
func (s *ManagedExecSessions) Wait(ctx context.Context) ([]ManagedExecReceipt, error) {
	for {
		jobs, pending, changed := s.readyJobs()
		if len(jobs) != 0 || !pending {
			receipts, err := s.collect(ctx, jobs)
			if err != nil || len(receipts) != 0 || !pending {
				return receipts, err
			}
			continue // A concurrent observer claimed the same receipt.
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-changed:
		}
	}
}

func (s *ManagedExecSessions) readyJobs() (jobs []*managedExecSession, pending bool, changed <-chan struct{}) {
	jobs, pending, _, changed = s.observationState()
	return
}

// observationState captures readiness and its next broadcast under one lock.
// Foreground results stay on their ordinary ADK tool path, never in TakeReady.
func (s *ManagedExecSessions) observationState() (jobs []*managedExecSession, pending bool, foreground bool, changed <-chan struct{}) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, job := range s.active {
		if job.yielded {
			pending = true
			select {
			case <-job.done:
				jobs = append(jobs, job)
			default:
			}
		}
	}
	return jobs, pending, s.foregroundSettled, s.changed
}

func (s *ManagedExecSessions) collect(ctx context.Context, jobs []*managedExecSession) ([]ManagedExecReceipt, error) {
	slices.SortFunc(jobs, func(a, b *managedExecSession) int { return cmp.Compare(a.seq, b.seq) })
	receipts := make([]ManagedExecReceipt, 0, len(jobs))
	for _, job := range jobs {
		result, err := job.command.result(job.sessionID, job.exitCode, job.runErr)
		if err != nil {
			return nil, err
		}
		receipts = append(receipts, ManagedExecReceipt{
			CallID: job.callID, SessionID: job.sessionID, Arguments: job.arguments,
			Result: capExecResult(result, min(job.limit, liveExecNotifyLimit)),
		})
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	collected := receipts[:0]
	for i, job := range jobs {
		if s.active[job.callID] == job {
			delete(s.active, job.callID)
			collected = append(collected, receipts[i])
		}
	}
	return collected, nil
}

// Close cancels the owner's processes and waits for their cleanup.
func (s *ManagedExecSessions) Close() error {
	s.mu.Lock()
	s.cancel()
	jobs := make([]*managedExecSession, 0, len(s.active))
	for _, job := range s.active {
		jobs = append(jobs, job)
	}
	s.mu.Unlock()
	var result error
	for _, job := range jobs {
		<-job.done
		if errors.Is(job.runErr, errManagedExecCleanup) {
			result = errors.Join(result, job.runErr)
		}
	}
	return result
}
