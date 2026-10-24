package engine

import "errors"

// ErrRestoring means the engine is serving history while restoring its owners.
var ErrRestoring = errors.New("engine: restoring unfinished tasks")

// RestoreProgress counts conversations whose durable startup state was examined.
type RestoreProgress struct {
	Completed int `json:"completed"`
	Total     int `json:"total"`
	Failed    int `json:"failed,omitempty"`
}

// BeginRestore closes admission before the listener is published.
func (e *Engine) BeginRestore() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.startupRestoring = true
	e.restoreProgress = RestoreProgress{}
}

// SetRestoreProgress publishes a consistent snapshot to both client transports.
func (e *Engine) SetRestoreProgress(completed, total int) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.restoreProgress.Completed, e.restoreProgress.Total = completed, total
}

// SetRestoreFailures exposes failed conversations without claiming they resumed.
func (e *Engine) SetRestoreFailures(failed int) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.restoreProgress.Failed = failed
}

// FinishRestore admits work only after recovery and scheduler setup have ended.
func (e *Engine) FinishRestore() {
	e.mu.Lock()
	defer e.mu.Unlock()
	// The first immediate scheduler pass takes this same lock for admission.
	// It must not observe restoring after startup has enabled the ticker.
	e.StartScheduler()
	e.startupRestoring = false
}

// StartupStatus distinguishes a reachable engine from a recovered one.
func (e *Engine) StartupStatus() (string, RestoreProgress) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.startupRestoring {
		return "restoring", e.restoreProgress
	}
	return "ready", e.restoreProgress
}

// RequireReady guards transport operations that mutate recovery inputs.
func (e *Engine) RequireReady() error {
	state, _ := e.StartupStatus()
	if state == "restoring" {
		return ErrRestoring
	}
	return nil
}

// Stop can precede this conversation's recovery. Persist its identity now so
// the next recovery cannot bring those checkpointed workers back to life.
func (e *Engine) stopBeforeRestore(threadID string) error {
	if err := e.RequireReady(); !errors.Is(err, ErrRestoring) {
		return ErrIdle
	}
	turns, err := e.store.ListTurns(threadID)
	if err != nil {
		return err
	}
	if len(turns) == 0 {
		return ErrIdle
	}
	turn := turns[len(turns)-1]
	rt := e.runtimeFor(threadID)
	if err := e.writeRestartEvent(&restartTarget{rt: rt, turnID: turn.ID}, KindUserStopRequested, "Stop requested by the user."); err != nil {
		return err
	}
	rt.mu.Lock()
	rt.userStopped = true
	rt.mu.Unlock()
	return nil
}
