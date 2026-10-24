package engine

import (
	"context"
	"encoding/json"
	"sort"
	"time"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

// restoreSnapshot is immutable startup input. It is passed to the resumed
// turn's bootstrap, never reused as a cache for later user work.
type restoreSnapshot struct {
	threadID        string
	events          []store.Event
	checkpoint      *restartCheckpoint
	checkpointEvent *store.Event
	running         []swarm.RestoredWorker
	finished        []swarm.FinishedWorker
}

func firstRestore(snapshots []*restoreSnapshot) *restoreSnapshot {
	if len(snapshots) > 0 {
		return snapshots[0]
	}
	return nil
}

func (e *Engine) loadRestoreSnapshot(ctx context.Context, threadID string) (*restoreSnapshot, error) {
	var accepted *restartCheckpoint
	cp, err := e.store.LatestEventOfKind(ctx, threadID, KindRestartCheckpoint, func(event *store.Event) bool {
		var checkpoint restartCheckpoint
		if json.Unmarshal([]byte(event.Text), &checkpoint) != nil || checkpoint.Version != 1 {
			return false
		}
		accepted = &checkpoint
		return true
	})
	if err != nil {
		return nil, err
	}
	s := &restoreSnapshot{threadID: threadID, checkpointEvent: cp, checkpoint: accepted}
	cutoff := int64(0)
	if accepted != nil && len(accepted.ManagerHistory) > 0 {
		cutoff = cp.Seq
	}
	var pendingIDs []string
	if s.checkpoint != nil {
		open := map[string]bool{}
		for _, msg := range s.checkpoint.ManagerHistory {
			if msg == nil {
				continue
			}
			for _, call := range msg.ToolCalls {
				open[call.ID] = true
			}
			if msg.Role == "tool" && msg.Content != swarm.HandoffUnknownToolResult {
				delete(open, msg.ToolCallID)
			}
		}
		for id := range open {
			pendingIDs = append(pendingIDs, id)
		}
	}
	s.events, err = e.store.ListRecoveryEvents(ctx, threadID, cutoff, pendingIDs...)
	if err != nil {
		return nil, err
	}
	index := -1
	if s.checkpoint != nil {
		s.events = append(s.events, *cp)
		sort.Slice(s.events, func(i, j int) bool { return s.events[i].Seq < s.events[j].Seq })
		for i := range s.events {
			if s.events[i].Seq == cp.Seq {
				index = i
				break
			}
		}
	}
	s.running, s.finished = orphanedWorkersFromCheckpoint(s.events, s.checkpoint, index)
	e.generations.Lock()
	if e.generations.threads == nil {
		e.generations.threads = map[string]*generationLog{}
	}
	if e.generations.threads[threadID] == nil {
		log := &generationLog{open: map[string]store.Event{}, pending: map[string]store.Event{}}
		for _, ev := range s.events {
			log.apply(ev)
		}
		e.generations.threads[threadID] = log
	}
	e.generations.Unlock()
	return s, nil
}

func (e *Engine) restoreTurnEvents(turnID string, snapshots ...*restoreSnapshot) ([]store.Event, error) {
	if s := firstRestore(snapshots); s != nil {
		events := make([]store.Event, 0)
		for _, ev := range s.events {
			if ev.TurnID == turnID {
				events = append(events, ev)
			}
		}
		return events, nil
	}
	return e.store.ListTurnEvents(turnID)
}

func (e *Engine) recordRestoreSnapshot(turn *store.Turn, s *restoreSnapshot, started time.Time, restoreErr, contextErr error) {
	phase, category := "restored", ""
	if restoreErr != nil {
		phase, category = "failed", "restore_error"
		if contextErr != nil {
			phase, category = "cancelled", "context_cancelled"
		}
	}
	body, _ := json.Marshal(struct {
		Phase           string `json:"phase"`
		ErrorCategory   string `json:"error_category,omitempty"`
		DurationMS      int64  `json:"duration_ms"`
		ScannedEvents   int    `json:"scanned_events"`
		CheckpointReads int    `json:"checkpoint_reads"`
	}{phase, category, time.Since(started).Milliseconds(), len(s.events), 1})
	e.record(store.Event{ThreadID: turn.ThreadID, TurnID: turn.ID, Kind: "engine_restore", AgentID: swarm.DefaultManagerID, Text: string(body)})
	e.log.Info("engine conversation restoration", "phase", phase, "error_category", category, "thread", turn.ThreadID, "turn", turn.ID, "duration_ms", time.Since(started).Milliseconds(), "scanned_events", len(s.events), "checkpoint_reads", 1)
}

func checkpointFromRestoreEvents(events []store.Event, snapshots []*restoreSnapshot) (*restartCheckpoint, int) {
	if s := firstRestore(snapshots); s != nil {
		if s.checkpointEvent != nil {
			for i, ev := range events {
				if ev.Seq == s.checkpointEvent.Seq {
					return s.checkpoint, i
				}
			}
		}
		return nil, -1
	}
	return latestRestartCheckpoint(events)
}
