package engine

import (
	"errors"
	"strings"
	"time"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

// scheduleFiredNotice is the short timeline chip. The durable instruction
// lives on the stored user message for replay; rendering that wrapper as
// user_message would look like the human typed the protocol.
const scheduleFiredNotice = "Scheduled check."

// ScheduleContinueText is the user message the runtime injects when a wait
// fires. Generic on purpose: a sample CI/deploy task leaking in here would
// become the product's scheduled-check protocol.
func ScheduleContinueText(prompt string) string {
	return "This turn is a scheduled check. Do the check in the instruction below, then call report_schedule. Empty findings archive the run. Cancel the schedule when the wait is over.\n\n" + strings.TrimSpace(prompt)
}

func (e *Engine) clock() time.Time {
	fn := e.now
	if fn == nil {
		fn = time.Now
	}
	return fn().UTC()
}

// StartScheduler looks for due waits on ScheduleTick. Tests that need a
// deterministic clock install e.now first, then either call this or invoke
// fireDueSchedules directly. Engine.New does not start it.
func (e *Engine) StartScheduler() {
	e.schedMu.Lock()
	defer e.schedMu.Unlock()
	if e.schedStop != nil {
		return
	}
	stop := make(chan struct{})
	e.schedStop = stop
	e.schedWG.Add(1)
	tick := e.cfg.Swarm.ScheduleTick()
	go func() {
		defer e.schedWG.Done()
		ticker := time.NewTicker(tick)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				e.fireDueSchedules()
			}
		}
	}()
}

// StopScheduler ends the ticker loop and waits for the goroutine. Safe to
// call more than once; Shutdown always calls it so a test engine that never
// started one is still fine.
func (e *Engine) StopScheduler() {
	e.schedMu.Lock()
	stop := e.schedStop
	e.schedStop = nil
	e.schedMu.Unlock()
	if stop == nil {
		return
	}
	close(stop)
	e.schedWG.Wait()
}

func (e *Engine) fireDueSchedules() {
	e.fireMu.Lock()
	defer e.fireMu.Unlock()

	now := e.clock()
	due, err := e.store.ListDue(now)
	if err != nil {
		e.log.Warn("could not list due schedules", "err", err)
		return
	}
	max := e.cfg.Swarm.MaxActiveSchedules()
	for _, row := range due {
		running, err := e.store.HasRunningRun(row.ID)
		if err != nil {
			e.log.Warn("could not check an in-flight schedule run", "schedule", row.ID, "err", err)
			continue
		}
		if running {
			continue
		}
		n, err := e.store.CountRunningRuns()
		if err != nil {
			e.log.Warn("could not count in-flight schedule runs", "err", err)
			return
		}
		if n >= max {
			// Leave remaining due rows for the next tick. A cap defer is
			// not skipped_busy: the conversation may be idle.
			return
		}
		e.fireDueSchedule(row, now)
	}
}

func (e *Engine) fireDueSchedule(row store.Schedule, now time.Time) {
	spec, err := parseScheduleSpec(row.DelayS, row.EveryS, row.Cron, e.cfg.Swarm.ScheduleMinInterval())
	if err != nil {
		e.log.Warn("stored schedule has a broken cadence", "schedule", row.ID, "err", err)
		return
	}
	threadID := row.ThreadID
	if row.Kind == store.ScheduleStandalone || threadID == "" {
		e.fireStandalone(row, spec, now)
		return
	}
	th, err := e.store.GetThread(threadID)
	if err != nil {
		e.log.Warn("due wake has no conversation", "schedule", row.ID, "thread", threadID, "err", err)
		return
	}
	if th.PlanMode || e.Status(threadID).Running {
		e.skipBusy(row, spec, now, threadID)
		return
	}
	e.startScheduledTurn(row, spec, now, threadID)
}

func (e *Engine) fireStandalone(row store.Schedule, spec scheduleSpec, now time.Time) {
	th, err := e.CreateThread(row.Title, row.ProviderID, row.ProjectID)
	if err != nil {
		e.log.Warn("could not mint a conversation for a standalone schedule", "schedule", row.ID, "err", err)
		return
	}
	fields := map[string]any{}
	if strings.TrimSpace(row.Model) != "" {
		fields["model"] = row.Model
	}
	if strings.TrimSpace(row.ReasoningEffort) != "" {
		fields["reasoning_effort"] = row.ReasoningEffort
	}
	if len(fields) > 0 {
		if err := e.store.UpdateThread(th.ID, fields); err != nil {
			e.log.Warn("could not copy schedule model onto the minted conversation", "schedule", row.ID, "err", err)
		}
	}
	e.startScheduledTurn(row, spec, now, th.ID)
}

func (e *Engine) skipBusy(row store.Schedule, spec scheduleSpec, now time.Time, threadID string) {
	run := &store.ScheduleRun{
		ScheduleID: row.ID,
		ThreadID:   threadID,
		Status:     store.ScheduleRunSkippedBusy,
	}
	if err := e.store.CreateRun(run); err != nil {
		e.log.Warn("could not record a skipped schedule tick", "schedule", row.ID, "err", err)
		return
	}
	e.record(store.Event{
		ThreadID: threadID, TurnID: e.lastTurnID(threadID),
		Kind: KindScheduleSkipped, AgentID: swarm.DefaultManagerID,
		Text: row.ID,
	})
	e.advanceAfterSkip(row, spec, now)
}

func (e *Engine) advanceAfterSkip(row store.Schedule, spec scheduleSpec, now time.Time) {
	if spec.delay != 0 {
		// A one-shot that hits a busy tick stays due. Marking it done
		// would drop the check forever; bumping next_run_at by delay_s
		// again would make a 90s wait into 90s-plus-however-long-busy.
		// next_run_at=now retries on the next idle tick.
		if err := e.store.UpdateSchedule(row.ID, map[string]any{
			"status":      store.ScheduleActive,
			"next_run_at": now,
		}); err != nil {
			e.log.Warn("could not keep a skipped delay due", "schedule", row.ID, "err", err)
		}
		return
	}
	// Advance from now, not from the stale next_run_at. Missed interval
	// or cron beats are not queued as follow-ups and are not steered.
	next := spec.nextAfter(now)
	if next.IsZero() {
		return
	}
	if err := e.store.UpdateSchedule(row.ID, map[string]any{"next_run_at": next}); err != nil {
		e.log.Warn("could not advance a skipped schedule", "schedule", row.ID, "err", err)
	}
}

func (e *Engine) startScheduledTurn(row store.Schedule, spec scheduleSpec, now time.Time, threadID string) {
	run := &store.ScheduleRun{
		ScheduleID: row.ID,
		ThreadID:   threadID,
		Status:     store.ScheduleRunRunning,
	}
	if err := e.store.CreateRun(run); err != nil {
		e.log.Warn("could not claim a schedule fire", "schedule", row.ID, "err", err)
		return
	}
	turn, err := e.StartTurnInput(threadID, UserInput{
		Text:             ScheduleContinueText(row.Prompt),
		ContinueSchedule: true,
		ScheduleID:       row.ID,
		ScheduleRunID:    run.ID,
	})
	if err != nil {
		status := store.ScheduleRunError
		if errors.Is(err, ErrBusy) {
			status = store.ScheduleRunSkippedBusy
			e.record(store.Event{
				ThreadID: threadID, TurnID: e.lastTurnID(threadID),
				Kind: KindScheduleSkipped, AgentID: swarm.DefaultManagerID,
				Text: row.ID,
			})
			e.advanceAfterSkip(row, spec, now)
		}
		if finErr := e.store.FinishRun(run.ID, status, err.Error(), status == store.ScheduleRunError); finErr != nil {
			e.log.Warn("could not close a failed schedule claim", "schedule", row.ID, "err", finErr)
		}
		if !errors.Is(err, ErrBusy) {
			e.log.Warn("could not start a scheduled turn", "schedule", row.ID, "err", err)
		}
		return
	}
	if err := e.store.SetRunTurn(run.ID, turn.ID); err != nil {
		e.log.Warn("could not bind a schedule run to its turn", "schedule", row.ID, "turn", turn.ID, "err", err)
	}
	e.advanceAfterFire(row, spec, now)
}

func (e *Engine) advanceAfterFire(row store.Schedule, spec scheduleSpec, now time.Time) {
	fields := map[string]any{
		"last_run_at": now,
		"run_count":   row.RunCount + 1,
	}
	if spec.delay != 0 {
		fields["status"] = store.ScheduleDone
	} else {
		next := spec.nextAfter(now)
		if !next.IsZero() {
			fields["next_run_at"] = next
		}
	}
	if err := e.store.UpdateSchedule(row.ID, fields); err != nil {
		e.log.Warn("could not advance a fired schedule", "schedule", row.ID, "err", err)
	}
}
