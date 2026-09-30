package engine

import (
	"fmt"
	"strings"
	"time"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

// interruptedWorkersCueLead is the stable prefix of the model-only cue.
// The ids follow it. Replay and persist match the prefix: the sentence is
// not a human message, and a later dispatch must be allowed to replace it.
const interruptedWorkersCueLead = "These sub-agents are not running. The previous process stopped them before they finished. They are not writing."

// stoppedWorker is one sub-agent from the latest dispatch whose model
// stream died with the process. A clean finish, an end-of-turn cleanup,
// and an older dispatch are not this.
type stoppedWorker struct {
	ID   string
	Role string
	Err  string
}

// processStoppedWorkers is the latest spawn turn's sub-agents that finished
// because the process stopped. Older dispatches stay out: a long conversation
// has many finished workers, and only the last wave is what a parked wait
// can still be pretending is alive. A newer spawn or resume replaces that
// wave, which is what clears the block on the next wake.
func processStoppedWorkers(events []store.Event) []stoppedWorker {
	type slot struct {
		role, err, spawnTurn string
		spawned, running     bool
	}
	byID := map[string]*slot{}
	order := make([]string, 0)
	note := func(id string) *slot {
		s, ok := byID[id]
		if !ok {
			s = &slot{}
			byID[id] = s
			order = append(order, id)
		}
		return s
	}
	lastSpawnTurn := ""
	for _, ev := range events {
		if ev.Kind == KindCleanup {
			for _, id := range order {
				s := byID[id]
				if s == nil || !s.spawned || !s.running {
					continue
				}
				s.running = false
				s.err = cleanedUpWorkerErr
			}
			continue
		}
		if isManagerAgent(ev.AgentID) {
			continue
		}
		s := note(ev.AgentID)
		switch ev.Kind {
		case swarm.NotifySpawned.String():
			// resume_agent emits spawned again for the same id. That puts
			// this one back to work; it must not hide siblings from the
			// same dispatch that are still stopped.
			resumed := s.spawned && !s.running && processStopErr(s.err)
			s.spawned = true
			s.running = true
			s.err = ""
			if !resumed {
				s.spawnTurn = ev.TurnID
				lastSpawnTurn = ev.TurnID
			}
			if role := strings.TrimSpace(ev.Role); role != "" {
				s.role = role
			}
		case swarm.NotifyFinished.String():
			s.running = false
			s.err = ev.Err
			if s.role == "" {
				s.role = strings.TrimSpace(ev.Role)
			}
		}
	}
	if lastSpawnTurn == "" {
		return nil
	}
	out := make([]stoppedWorker, 0)
	for _, id := range order {
		s := byID[id]
		if s == nil || !s.spawned || s.running || s.spawnTurn != lastSpawnTurn {
			continue
		}
		if !processStopErr(s.err) {
			continue
		}
		out = append(out, stoppedWorker{ID: id, Role: s.role, Err: s.err})
	}
	return out
}

func processStopErr(errText string) bool {
	s := strings.ToLower(strings.TrimSpace(errText))
	if s == "" || s == strings.ToLower(cleanedUpWorkerErr) {
		return false
	}
	return strings.Contains(s, "context canceled") ||
		strings.Contains(s, "context deadline") ||
		strings.Contains(s, "the previous process stopped")
}

func formatInterruptedWorkersCue(dead []stoppedWorker) string {
	if len(dead) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString(interruptedWorkersCueLead)
	b.WriteString(" Use these exact ids with resume_agent, or spawn replacements. A shorter id is a different agent. wait_agents unknown is not evidence they are still working. Do not call schedule_wake, and do not pass next_in_s to report_schedule, until that recovery has started.\n")
	for _, w := range dead {
		fmt.Fprintf(&b, "- %s\n", w.ID)
	}
	return strings.TrimRight(b.String(), "\n")
}

func interruptedWakeBlock(dead []stoppedWorker) string {
	if len(dead) == 0 {
		return ""
	}
	ids := make([]string, 0, len(dead))
	for _, w := range dead {
		if id := strings.TrimSpace(w.ID); id != "" {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return ""
	}
	return "these sub-agents are not running (" + strings.Join(ids, ", ") + "). The previous process stopped them. resume_agent those exact ids or spawn replacements before arming a wait. A wake does not keep a stopped sub-agent working."
}

func (e *Engine) processStoppedOn(threadID string) ([]stoppedWorker, error) {
	if e == nil || e.store == nil || strings.TrimSpace(threadID) == "" {
		return nil, nil
	}
	events, err := e.store.ListEvents(threadID, 0, 0)
	if err != nil {
		return nil, err
	}
	return processStoppedWorkers(events), nil
}

func (e *Engine) interruptedWakeRefusal(threadID string) (string, error) {
	dead, err := e.processStoppedOn(threadID)
	if err != nil {
		return "", err
	}
	return interruptedWakeBlock(dead), nil
}

func (e *Engine) appendInterruptedWorkerCue(threadID string, msgs []adk.Message) []adk.Message {
	dead, err := e.processStoppedOn(threadID)
	if err != nil || len(dead) == 0 {
		return msgs
	}
	if hasUserPrefix(msgs, interruptedWorkersCueLead) {
		return msgs
	}
	cue := formatInterruptedWorkersCue(dead)
	if cue == "" {
		return msgs
	}
	return append(msgs, schema.UserMessage(cue))
}

func hasUserPrefix(msgs []adk.Message, prefix string) bool {
	if prefix == "" {
		return false
	}
	for _, m := range msgs {
		if m != nil && m.Role == schema.User && strings.HasPrefix(m.Content, prefix) {
			return true
		}
	}
	return false
}

func isEngineOnlyUser(content string) bool {
	switch content {
	case resumeCue, resumeWorkersCue, parkedWorkersCue:
		return true
	default:
		return strings.HasPrefix(content, interruptedWorkersCueLead)
	}
}

// PullInterruptedWorkerWakes makes a future thread wake due now when the
// latest dispatch was stopped by the previous process. The scheduler's
// first tick then starts the check. A conversation that is already running
// is left alone: resume owns that turn. An already-due wake is left on its
// own timestamp.
func (e *Engine) PullInterruptedWorkerWakes() (int, error) {
	if e == nil || e.store == nil {
		return 0, nil
	}
	rows, err := e.store.ListSchedules()
	if err != nil {
		return 0, err
	}
	now := e.clock()
	n := 0
	for _, row := range rows {
		if !shouldPullInterruptedWake(row, e.Status(row.ThreadID).Running, now) {
			continue
		}
		dead, err := e.processStoppedOn(row.ThreadID)
		if err != nil {
			return n, err
		}
		if len(dead) == 0 {
			continue
		}
		ok, err := e.store.UpdateActiveSchedule(row.ID, map[string]any{"next_run_at": now})
		if err != nil {
			return n, err
		}
		if ok {
			n++
		}
	}
	return n, nil
}

func shouldPullInterruptedWake(row store.Schedule, running bool, now time.Time) bool {
	if running || row.Kind != store.ScheduleThread || row.Status != store.ScheduleActive {
		return false
	}
	if strings.TrimSpace(row.ThreadID) == "" {
		return false
	}
	return row.NextRunAt.After(now)
}
