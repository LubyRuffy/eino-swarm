package engine

import (
	"errors"
	"fmt"
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

// StartScheduler looks for due waits on ScheduleTick. It fires once
// immediately so a restart overdue row is not delayed a full tick. Tests
// that wait on a turn use fireDueSchedules only, or set a huge tick.
func (e *Engine) StartScheduler() {
	e.schedMu.Lock()
	defer e.schedMu.Unlock()
	if e.schedStop != nil {
		return
	}
	e.snapshotScheduleCaps()
	e.schedCapsFrozen.Store(true)
	stop := make(chan struct{})
	e.schedStop = stop
	e.schedWG.Add(1)
	tick := e.cfg.Swarm.ScheduleTick()
	go func() {
		defer e.schedWG.Done()
		e.fireDueSchedules()
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
	e.schedCapsFrozen.Store(false)
}

type claimedFire struct {
	row        store.Schedule
	spec       scheduleSpec
	run        *store.ScheduleRun
	threadID   string
	now        time.Time
	standalone bool
}

func (e *Engine) fireDueSchedules() {
	claimed := e.claimDueSchedules()
	for _, c := range claimed {
		_, _ = e.startClaimedFire(c)
	}
}

func (e *Engine) claimDueSchedules() []claimedFire {
	e.fireMu.Lock()
	defer e.fireMu.Unlock()

	now := e.clock()
	due, err := e.store.ListDue(now)
	if err != nil {
		e.log.Warn("could not list due schedules", "err", err)
		return nil
	}
	max := e.maxActiveSchedules()
	var out []claimedFire
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
			return out
		}
		if n >= max {
			// Leave remaining due rows for the next tick. A cap defer is
			// not skipped_busy: the conversation may be idle.
			return out
		}
		if c := e.claimDueSchedule(row, now); c != nil {
			out = append(out, *c)
		}
	}
	return out
}

func (e *Engine) claimDueSchedule(row store.Schedule, now time.Time) *claimedFire {
	spec, err := parseScheduleSpec(row.DelayS, row.EveryS, row.Cron, e.scheduleMinInterval())
	if err != nil {
		e.log.Warn("stored schedule has a broken cadence", "schedule", row.ID, "err", err)
		return nil
	}
	if row.Kind == store.ScheduleStandalone || row.ThreadID == "" {
		return e.claimStandalone(row, spec, now)
	}
	th, err := e.store.GetThread(row.ThreadID)
	if err != nil {
		e.log.Warn("due wake has no conversation", "schedule", row.ID, "thread", row.ThreadID, "err", err)
		return nil
	}
	if th.PlanMode || e.Status(row.ThreadID).Running {
		e.skipBusy(row, spec, now, row.ThreadID)
		return nil
	}
	return e.claimScheduledFire(row, spec, now, row.ThreadID, false)
}

func (e *Engine) claimStandalone(row store.Schedule, spec scheduleSpec, now time.Time) *claimedFire {
	providerID := strings.TrimSpace(row.ProviderID)
	if providerID == "" {
		providerID = e.scheduleDefaultProvider()
	}
	if _, err := e.pool.ResolveModel(providerID, row.Model); err != nil {
		e.log.Warn("standalone schedule has no usable model", "schedule", row.ID, "err", err)
		return nil
	}
	return e.claimScheduledFire(row, spec, now, "", true)
}

func (e *Engine) claimScheduledFire(row store.Schedule, spec scheduleSpec, now time.Time, threadID string, standalone bool) *claimedFire {
	run := &store.ScheduleRun{
		ScheduleID: row.ID,
		ThreadID:   threadID,
		Status:     store.ScheduleRunRunning,
	}
	if err := e.store.CreateRun(run); err != nil {
		e.log.Warn("could not claim a schedule fire", "schedule", row.ID, "err", err)
		return nil
	}
	if !e.advanceAfterFire(row, spec, now) {
		if err := e.store.FinishRun(run.ID, store.ScheduleRunError, "schedule is no longer active", false); err != nil {
			e.log.Warn("could not drop a cancelled schedule claim", "schedule", row.ID, "err", err)
		}
		return nil
	}
	return &claimedFire{row: row, spec: spec, run: run, threadID: threadID, now: now, standalone: standalone}
}

func (e *Engine) fireStandalone(row store.Schedule, spec scheduleSpec, now time.Time) {
	if c := e.claimStandalone(row, spec, now); c != nil {
		_, _ = e.startClaimedFire(*c)
	}
}

func (e *Engine) skipBusy(row store.Schedule, spec scheduleSpec, now time.Time, threadID string) {
	if e.latestRunIsSkipped(row.ID) {
		e.advanceAfterSkip(row, spec, now)
		return
	}
	if !e.advanceAfterSkip(row, spec, now) {
		return
	}
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
}

func (e *Engine) latestRunIsSkipped(scheduleID string) bool {
	runs, err := e.store.ListRuns(scheduleID)
	if err != nil || len(runs) == 0 {
		return false
	}
	return runs[len(runs)-1].Status == store.ScheduleRunSkippedBusy
}

func (e *Engine) advanceAfterSkip(row store.Schedule, spec scheduleSpec, now time.Time) bool {
	fields := map[string]any{}
	if spec.delay != 0 {
		// A one-shot that hits a busy tick stays due. Do not write
		// status=active: that would resurrect a cancelled wait. Only
		// bump next_run_at=now so the next idle tick retries.
		fields["next_run_at"] = now
	} else {
		// Advance from now, not from the stale next_run_at. Missed
		// interval or cron beats are not queued as follow-ups.
		next := spec.nextAfter(now)
		if next.IsZero() {
			return true
		}
		fields["next_run_at"] = next
	}
	ok, err := e.store.UpdateActiveSchedule(row.ID, fields)
	if err != nil {
		e.log.Warn("could not advance a skipped schedule", "schedule", row.ID, "err", err)
		return false
	}
	return ok
}

func (e *Engine) startScheduledTurn(row store.Schedule, spec scheduleSpec, now time.Time, threadID string) {
	c := e.claimScheduledFire(row, spec, now, threadID, false)
	if c == nil {
		return
	}
	_, _ = e.startClaimedFire(*c)
}

func (e *Engine) startClaimedFire(c claimedFire) (*store.Turn, error) {
	threadID := c.threadID
	if c.standalone {
		th, err := e.mintStandaloneThread(c.row)
		if err != nil {
			if finErr := e.store.FinishRun(c.run.ID, store.ScheduleRunError, err.Error(), true); finErr != nil {
				e.log.Warn("could not close a mint failure", "schedule", c.row.ID, "err", finErr)
			}
			return nil, err
		}
		threadID = th.ID
	}
	turn, err := e.StartTurnInput(threadID, UserInput{
		Text:             ScheduleContinueText(c.row.Prompt),
		ContinueSchedule: true,
		ScheduleID:       c.row.ID,
		ScheduleRunID:    c.run.ID,
	})
	if err != nil {
		status := store.ScheduleRunError
		out := err
		if errors.Is(err, ErrBusy) {
			status = store.ScheduleRunSkippedBusy
			out = ErrSkippedBusy
			e.record(store.Event{
				ThreadID: threadID, TurnID: e.lastTurnID(threadID),
				Kind: KindScheduleSkipped, AgentID: swarm.DefaultManagerID,
				Text: c.row.ID,
			})
		}
		if finErr := e.store.FinishRun(c.run.ID, status, err.Error(), status == store.ScheduleRunError); finErr != nil {
			e.log.Warn("could not close a failed schedule claim", "schedule", c.row.ID, "err", finErr)
		}
		if !errors.Is(err, ErrBusy) {
			e.log.Warn("could not start a scheduled turn", "schedule", c.row.ID, "err", err)
		}
		return nil, out
	}
	if err := e.store.BindRun(c.run.ID, threadID, turn.ID); err != nil {
		e.log.Warn("could not bind a schedule run to its turn", "schedule", c.row.ID, "turn", turn.ID, "err", err)
	}
	return turn, nil
}

func (e *Engine) mintStandaloneThread(row store.Schedule) (*store.Thread, error) {
	th, err := e.CreateThread(row.Title, row.ProviderID, row.ProjectID)
	if err != nil {
		return nil, err
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
	return th, nil
}

func (e *Engine) advanceAfterFire(row store.Schedule, spec scheduleSpec, now time.Time) bool {
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
	ok, err := e.store.UpdateActiveSchedule(row.ID, fields)
	if err != nil {
		e.log.Warn("could not advance a fired schedule", "schedule", row.ID, "err", err)
		return false
	}
	return ok
}

// RunScheduleNow fires one wait immediately, even when next_run_at is
// still in the future. The ticker path is ListDue; this is the inbox
// button. fireMu is only held while claiming — StartTurn is outside it.
func (e *Engine) RunScheduleNow(id string) (*store.Turn, error) {
	c, err := e.claimRunNow(id)
	if err != nil {
		return nil, err
	}
	return e.startClaimedFire(*c)
}

func (e *Engine) claimRunNow(id string) (*claimedFire, error) {
	e.fireMu.Lock()
	defer e.fireMu.Unlock()

	row, err := e.store.GetSchedule(id)
	if err != nil {
		return nil, err
	}
	if row.Status != store.ScheduleActive {
		return nil, fmt.Errorf("engine: schedule is not active")
	}
	running, err := e.store.HasRunningRun(row.ID)
	if err != nil {
		return nil, err
	}
	if running {
		return nil, ErrSkippedBusy
	}
	spec, err := parseScheduleSpec(row.DelayS, row.EveryS, row.Cron, e.scheduleMinInterval())
	if err != nil {
		return nil, err
	}
	now := e.clock()
	if row.Kind == store.ScheduleStandalone || row.ThreadID == "" {
		c := e.claimStandalone(*row, spec, now)
		if c == nil {
			return nil, fmt.Errorf("engine: could not claim the schedule")
		}
		return c, nil
	}
	th, err := e.store.GetThread(row.ThreadID)
	if err != nil {
		return nil, err
	}
	if th.PlanMode || e.Status(row.ThreadID).Running {
		e.skipBusy(*row, spec, now, row.ThreadID)
		return nil, ErrSkippedBusy
	}
	c := e.claimScheduledFire(*row, spec, now, row.ThreadID, false)
	if c == nil {
		return nil, fmt.Errorf("engine: could not claim the schedule")
	}
	return c, nil
}
