package engine

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/store"
)

// EnqueueFollowup records a message to run after the current turn finishes.
// It is not steering: the manager will not see this text until a later turn.
// Nothing running is idle, not a silent queue that never flushes.
func (e *Engine) EnqueueFollowup(threadID, text string) (*store.Followup, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, fmt.Errorf("engine: an empty follow-up has nothing to wait for")
	}
	if _, err := e.store.GetThread(threadID); err != nil {
		return nil, err
	}
	if !e.Status(threadID).Running {
		return nil, ErrIdle
	}
	return e.store.EnqueueFollowup(threadID, text)
}

// ListFollowups returns waiting messages in the order they were typed.
func (e *Engine) ListFollowups(threadID string) ([]store.Followup, error) {
	if _, err := e.store.GetThread(threadID); err != nil {
		return nil, err
	}
	return e.store.ListFollowups(threadID)
}

// DeleteFollowup drops one waiting message. Missing is not found.
func (e *Engine) DeleteFollowup(threadID, id string) error {
	if _, err := e.store.GetThread(threadID); err != nil {
		return err
	}
	return e.store.DeleteFollowup(threadID, id)
}

// RequeueFollowup saves an edited waiting message at the back of the FIFO.
// The queue can outlive the turn that created it (Stop leaves rows), so this
// does not require a running turn.
func (e *Engine) RequeueFollowup(threadID, id, text string) (*store.Followup, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, fmt.Errorf("engine: an empty follow-up has nothing to wait for")
	}
	if _, err := e.store.GetThread(threadID); err != nil {
		return nil, err
	}
	return e.store.RequeueFollowup(threadID, id, text)
}

// SteerFollowup pulls a waiting message into the running turn at the next
// model boundary. It does not cancel an in-flight tool. If the turn has
// already ended, the row is put back so a refresh still shows it.
func (e *Engine) SteerFollowup(threadID, id string) error {
	f, err := e.store.GetFollowup(threadID, id)
	if err != nil {
		return err
	}
	if err := e.store.DeleteFollowup(threadID, id); err != nil {
		return err
	}
	if err := e.Steer(threadID, f.Text); err != nil {
		if uerr := e.store.UnshiftFollowup(f); uerr != nil {
			e.log.Warn("could not restore a follow-up after a failed steer",
				"thread", threadID, "followup", id, "err", uerr)
		}
		return err
	}
	return nil
}

// flushFollowup starts the oldest waiting message as the next turn. Only a
// clean finish does this: Stop and errors leave the queue where it is.
// A late steer that already claimed the next turn gets ErrBusy; the row
// goes back to the front so it runs after that one.
func (rt *runtime) flushFollowup(status string) bool {
	if status != store.TurnDone {
		return false
	}
	item, err := rt.engine.store.PopFollowup(rt.threadID)
	if err != nil {
		rt.engine.log.Warn("could not pop a follow-up", "thread", rt.threadID, "err", err)
		return false
	}
	if item == nil {
		return false
	}
	var startErr error
	for attempt := 0; attempt < 8; attempt++ {
		_, startErr = rt.engine.StartTurn(rt.threadID, item.Text)
		if startErr == nil {
			return true
		}
		if errors.Is(startErr, ErrBusy) {
			break
		}
		if !strings.Contains(startErr.Error(), "database is locked") &&
			!strings.Contains(startErr.Error(), "SQLITE_BUSY") {
			break
		}
		time.Sleep(time.Duration(20*(attempt+1)) * time.Millisecond)
	}
	if uerr := rt.engine.store.UnshiftFollowup(item); uerr != nil {
		rt.engine.log.Warn("could not put a follow-up back after it failed to start",
			"thread", rt.threadID, "followup", item.ID, "err", uerr)
	}
	if !errors.Is(startErr, ErrBusy) {
		rt.engine.log.Warn("could not start the next follow-up",
			"thread", rt.threadID, "err", startErr)
	}
	return false
}
