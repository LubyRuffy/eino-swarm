package engine

import (
	"cmp"
	"context"
	"errors"
	"slices"
	"strings"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

func newDistillationBatch(e *Engine, job *projectDistillWork, cursor uint) *projectDistillWork {
	return &projectDistillWork{
		projectID: job.projectID, route: job.route,
		checkpoint: &distillCheckpoint{e: e, current: &job.result, projectID: job.projectID, from: cursor, to: cursor},
		result: DistillationResult{
			ReviewID: job.result.ReviewID, Scope: job.result.Scope, Uncertain: job.result.Uncertain,
			FullText:      job.result.FullText,
			MemoryEnabled: job.result.MemoryEnabled,
			ThreadID:      job.result.ThreadID, TurnID: job.result.TurnID,
		},
	}
}

func distillationCallIDs(rows []store.DistillationSourceRow) []uint {
	var ids []uint
	for _, row := range rows {
		if row.CallID != 0 {
			ids = append(ids, row.CallID)
		}
	}
	return ids
}

func (e *Engine) distillationSliceFromRows(ctx context.Context, job *projectDistillWork, cursor uint, rows []store.DistillationSourceRow) (*projectDistillWork, error) {
	calls, err := e.store.DistillationToolCalls(ctx, distillationCallIDs(rows))
	if err != nil {
		return nil, err
	}
	batch := newDistillationBatch(e, job, cursor)
	checkpoint := batch.checkpoint
	var turns []store.DistillationTurn
	turnIndex := make(map[string]int)
	included := make(map[uint]bool)
	budget := reviewMaxChars - len(reviewSourceHeader+reviewSourceOmitted)
	for i := range rows {
		row := &rows[i]
		events, err := e.distillationPrimaryEvents(ctx, row, calls)
		if err != nil {
			return nil, err
		}
		cost := 0
		for _, event := range events {
			if !included[event.ID] {
				cost += distillationSourceCost(event)
			}
		}
		if cost > budget {
			break
		}
		budget -= cost
		checkpoint.to = row.Position
		checkpoint.count++
		if row.Oversize {
			checkpoint.oversizedSkipped++
		}
		batch.result.HistoryOmitted = batch.result.HistoryOmitted || row.Oversize
		if len(events) == 0 {
			continue
		}
		index, ok := turnIndex[row.Turn.ID]
		if !ok {
			index = len(turns)
			turnIndex[row.Turn.ID] = index
			turns = append(turns, store.DistillationTurn{Turn: row.Turn})
		}
		for _, ev := range events {
			if !included[ev.ID] {
				turns[index].Events = append(turns[index].Events, ev)
				included[ev.ID] = true
			}
		}
		batch.anchor = row.Turn
	}
	for i := range turns {
		slices.SortFunc(turns[i].Events, func(a, b store.Event) int { return cmp.Compare(a.Seq, b.Seq) })
	}
	slices.Reverse(turns) // the renderer accepts most-recent turn first
	batch.sources = selectDistillationSources(turns, batch.result.HistoryOmitted)
	batch.result.HistoryOmitted = batch.sources.omitted
	visible := make(map[uint]bool, len(batch.sources.sources))
	for handle, source := range batch.sources.sources {
		ev := batch.sources.memory[handle]
		visible[source.event.ID] = len(ev.Text) < len(strings.TrimSpace(source.event.Text))
	}
	for _, row := range rows[:checkpoint.count] {
		clipped, ok := visible[row.Position]
		if !ok {
			checkpoint.skipped++
		} else if clipped {
			checkpoint.excerpted++
		}
	}
	if batch.anchor.ID != "" {
		batch.result.ThreadID, batch.result.TurnID = batch.anchor.ThreadID, batch.anchor.ID
	}
	return batch, nil
}

func (e *Engine) distillationPrimaryEvents(ctx context.Context, row *store.DistillationSourceRow, calls map[uint]store.Event) ([]store.Event, error) {
	if row.ID == 0 || row.Oversize {
		return nil, nil
	}
	ev := row.Event
	switch ev.Kind {
	case KindSteer:
		message, err := e.store.DistillationSteerMessage(ctx, ev)
		if errors.Is(err, store.ErrHistorySourceUnavailable) {
			return nil, nil
		}
		if err != nil {
			return nil, err
		}
		text, ok := strings.CutPrefix(message.Content, "[steer] ")
		if !ok {
			return nil, nil
		}
		if len(text) > 64<<10 {
			row.Oversize = true
			return nil, nil
		}
		ev.Text = text
	case swarm.NotifyToolResult.String():
		call, ok := calls[row.CallID]
		if !ok || ev.Err != "" {
			return nil, nil
		}
		return []store.Event{call, ev}, nil
	}
	return []store.Event{ev}, nil
}

func distillationSourceCost(ev store.Event) int {
	// Quoted identities can expand when escaped. Estimate conservatively so the
	// renderer only clips a single source, never excludes a later queue entry.
	framing := 300 + 4*(len(ev.ThreadID)+len(ev.AgentID)*2+len(ev.Role)+len(ev.ToolCallID)+len(ev.Kind))
	return min(reviewMaxCharsPerMessage, framing+len(ev.Text)+len(ev.Err))
}
