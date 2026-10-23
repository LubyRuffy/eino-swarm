package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/LubyRuffy/eino-swarm/internal/store"
)

const KindDesktopRecovery = "desktop_recovery"

var ErrInvalidDesktopRecovery = errors.New("engine: invalid desktop recovery attribution")

// DesktopRecoveryInput describes interface recovery, never task completion.
// It deliberately contains no draft text, attachment bytes, screenshot or token.
type DesktopRecoveryInput struct {
	SessionID  string    `json:"session_id"`
	Generation int       `json:"generation"`
	Attempt    int       `json:"attempt"`
	ShellPID   int       `json:"shell_pid"`
	EnginePID  int       `json:"engine_pid"`
	ThreadID   string    `json:"thread_id,omitempty"`
	TurnID     string    `json:"turn_id,omitempty"`
	Phase      string    `json:"phase"`
	Reason     string    `json:"reason,omitempty"`
	At         time.Time `json:"at"`
	DurationMS int64     `json:"duration_ms,omitempty"`
}

// RecordDesktopRecovery orders durable diagnostics with the same event lock as
// worker output. A failed write is returned and never advertised as a receipt.
func (e *Engine) RecordDesktopRecovery(ctx context.Context, in DesktopRecoveryInput) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if in.TurnID == "" {
		if in.ThreadID != "" {
			if _, err := e.store.GetThread(in.ThreadID); err != nil {
				return err
			}
		}
		e.log.Info("desktop recovery", "session_id", in.SessionID, "thread_id", in.ThreadID, "phase", in.Phase, "reason", in.Reason, "shell_pid", in.ShellPID, "engine_pid", in.EnginePID)
		return nil
	}
	e.recordMu.Lock()
	defer e.recordMu.Unlock()
	if _, dropped := e.droppedTurns[in.TurnID]; dropped {
		return store.ErrNotFound
	}
	turn, err := e.store.GetTurn(in.TurnID)
	if err != nil {
		return err
	}
	if turn.ThreadID != in.ThreadID {
		return ErrInvalidDesktopRecovery
	}
	body, err := json.Marshal(in)
	if err != nil {
		return fmt.Errorf("%w: invalid diagnostic time", ErrInvalidDesktopRecovery)
	}
	ev := store.Event{ThreadID: in.ThreadID, TurnID: in.TurnID, Kind: KindDesktopRecovery, Text: string(body)}
	if err := e.store.AppendEvent(&ev); err != nil {
		return fmt.Errorf("engine: persist desktop recovery: %w", err)
	}
	e.broadcast(ev)
	return nil
}
