package engine

import (
	"context"
	"errors"
	"fmt"
	"time"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

// RestoreFailures reports conversations whose recovery was attempted but could
// not be resumed. Other conversations and future scheduled checks remain usable.
type RestoreFailures struct {
	Failed int
	Err    error
}

func (e *RestoreFailures) Error() string {
	return fmt.Sprintf("could not restore %d conversation(s): %v", e.Failed, e.Err)
}
func (e *RestoreFailures) Unwrap() error { return e.Err }

// Restore owns startup recovery before scheduling. Each conversation gets one
// immutable event/checkpoint snapshot, also passed to the resumed bootstrap.
func (e *Engine) Restore(ctx context.Context, progress func(completed, total int)) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	threads, err := e.store.ListThreads(true, "")
	if err != nil {
		return err
	}
	schedules, err := e.store.ListSchedules()
	if err != nil {
		return err
	}
	byThread := make(map[string][]store.Schedule)
	for _, row := range schedules {
		byThread[row.ThreadID] = append(byThread[row.ThreadID], row)
	}
	if progress != nil {
		progress(0, len(threads))
	}
	var failures []error
	for i := range threads {
		if err := ctx.Err(); err != nil {
			return err
		}
		e.restoreMu.Lock()
		err := e.restoreThread(ctx, &threads[i], byThread[threads[i].ID])
		e.restoreMu.Unlock()
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			e.log.Warn("could not resume parked workers from a previous run", "thread", threads[i].ID, "err", err)
			failures = append(failures, fmt.Errorf("restore conversation %s: %w", threads[i].ID, err))
		}
		if progress != nil {
			progress(i+1, len(threads))
		}
	}
	if len(failures) > 0 {
		return &RestoreFailures{Failed: len(failures), Err: errors.Join(failures...)}
	}
	return nil
}

func (e *Engine) restoreThread(ctx context.Context, th *store.Thread, schedules []store.Schedule) (err error) {
	started := time.Now()
	turns, err := e.store.ListTurns(th.ID)
	if err != nil || len(turns) == 0 {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	snapshot, err := e.loadRestoreSnapshot(ctx, th.ID)
	if err != nil {
		return err
	}
	latest := turns[len(turns)-1]
	defer func() { e.recordRestoreSnapshot(&latest, snapshot, started, err, ctx.Err()) }()
	running := make([]store.Turn, 0)
	for _, turn := range turns {
		if turn.Status == store.TurnRunning {
			running = append(running, turn)
		}
	}
	resume, drop := pickOrphanedTurns(running)
	for _, turn := range drop {
		reason := "superseded by a later unfinished turn"
		if err := e.store.FinishTurn(turn.ID, store.TurnCancelled, "", reason); err != nil {
			return err
		}
		e.finishScheduledRun(&turn, store.TurnCancelled, "", reason)
		e.record(store.Event{ThreadID: turn.ThreadID, TurnID: turn.ID, Kind: swarm.NotifyError.String(), AgentID: swarm.DefaultManagerID, Err: reason})
	}
	if len(resume) > 0 {
		_, err = e.resumeOrphanedTurn(ctx, resume[0], snapshot)
		return err
	}
	count, err := e.resumeParkedThreadContext(ctx, th, snapshot)
	if err != nil {
		return err
	}
	if count == 0 {
		if st := e.Status(th.ID); !st.Running && st.Workers == 0 {
			e.stopOrphanedGenerations(th.ID, e.turnHasKind(latest.ID, KindUserStopRequested, snapshot), snapshot)
		}
	}
	return e.pullInterruptedThreadWake(th.ID, snapshot, schedules)
}

func (e *Engine) pullInterruptedThreadWake(threadID string, snapshot *restoreSnapshot, rows []store.Schedule) error {
	if len(processStoppedWorkers(snapshot.events)) == 0 {
		return nil
	}
	now := e.clock()
	for _, row := range rows {
		if row.ThreadID != threadID || !shouldPullInterruptedWake(row, e.Status(threadID).Running, now) {
			continue
		}
		if _, err := e.store.UpdateActiveSchedule(row.ID, map[string]any{"next_run_at": now}); err != nil {
			return err
		}
	}
	return nil
}
