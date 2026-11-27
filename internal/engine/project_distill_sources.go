package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

var distillEventKinds = []string{
	KindUser, KindSteer, KindSteerRetracted, KindSteerRevised,
	swarm.NotifyAgentMessage.String(), swarm.NotifyToolCall.String(), swarm.NotifyToolResult.String(),
}

type distillSource struct {
	event    store.Event
	failed   bool
	offset   int
	steerSeq int64
}

type distillInput struct {
	transcript string
	sources    map[int64]distillSource
	memory     map[int64]store.Event
	omitted    bool
}

func selectDistillationSources(turns []store.DistillationTurn, omitted bool) distillInput {
	out := distillInput{
		sources: make(map[int64]distillSource), memory: make(map[int64]store.Event), omitted: omitted,
	}
	var items []reviewSourceItem
	var groups []reviewSourceGroup
	type origin struct {
		events  []store.Event
		handles map[int64]int64
		failed  bool
	}
	var origins []origin
	for i := len(turns) - 1; i >= 0; i-- {
		events := turns[i].Events
		failed := turns[i].Turn.Status == store.TurnError
		if failed {
			events = reviewHumanEvents(events)
		} else {
			events = distillOriginalEvents(events)
		}
		part, partGroups := collectReviewSources(events, "", "")
		handles := make(map[int64]int64, len(part))
		for j := range part {
			item := &part[j]
			handle := int64(len(items) + j + 1)
			ev := item.event
			handles[ev.Seq] = handle
			out.sources[handle] = distillSource{event: ev, failed: failed}
			item.event.Seq = handle
			status := "done"
			if failed {
				status = "failed"
			}
			item.reference = fmt.Sprintf(" thread=%q event_seq=%d turn_status=%s", ev.ThreadID, ev.Seq, status)
			item.group += len(groups)
		}
		for j := range partGroups {
			for k := range partGroups[j].items {
				partGroups[j].items[k] += len(items)
			}
		}
		items = append(items, part...)
		groups = append(groups, partGroups...)
		origins = append(origins, origin{events, handles, failed})
	}
	visible := selectReviewSourceItems(items, groups, "", omitted)
	for _, original := range origins {
		selected := make(map[int64]store.Event)
		for seq, handle := range original.handles {
			if ev, ok := visible.Events[handle]; ok {
				ev.Seq = seq
				selected[seq] = ev
			}
		}
		for seq, ev := range reviewEvidenceSources(original.events, selected) {
			handle := original.handles[seq]
			out.memory[handle] = ev
		}
	}
	for key := range out.sources {
		if _, mem := out.memory[key]; !mem {
			delete(out.sources, key)
		}
	}
	out.transcript = visible.Transcript
	out.omitted = omitted || strings.HasSuffix(visible.Transcript, reviewSourceOmitted)
	return out
}

func distillOriginalEvents(events []store.Event) []store.Event {
	// A previous extraction or an indexed summary cannot be its own evidence.
	derived := make(map[reviewToolKey]bool)
	var selected []store.Event
	for _, ev := range events {
		if ev.WorkRecordPayload {
			continue
		}
		key := reviewToolKey{ev.AgentID, ev.ToolCallID}
		if ev.Kind == swarm.NotifyToolCall.String() && ev.ToolCallID != "" {
			name := toolCallName(ev.Text)
			derived[key] = derivedDistillationTool(name)
			if derived[key] {
				continue
			}
		}
		if ev.Kind == swarm.NotifyToolResult.String() {
			var receipt map[string]any
			if derived[key] || (json.Unmarshal([]byte(ev.Text), &receipt) == nil && receipt["ok"] == false) {
				continue
			}
		}
		selected = append(selected, ev)
	}
	return selected
}

func derivedDistillationTool(name string) bool {
	return memoryWriteTool(name) || isWorkRecordTool(name) ||
		name == reviewBatchToolName || name == distillBatchName || name == ToolQueryHistory
}

func (e *Engine) verifyDistillationSources(ctx context.Context, work *projectDistillWork, handles []int64) error {
	refs := make([]store.HistoryReference, len(handles))
	for i, key := range handles {
		ev := work.sources.sources[key].event
		refs[i] = store.HistoryReference{Kind: "event", ThreadID: ev.ThreadID, Seq: ev.Seq}
	}
	rows, err := e.store.ReadProjectHistoryIdentities(ctx, work.projectID, refs)
	if err != nil {
		return err
	}
	for i, key := range handles {
		source := work.sources.sources[key]
		ev, row := source.event, rows[i]
		if row == nil || row.RowID != ev.ID || row.TurnID != ev.TurnID ||
			!row.CreatedAt.Valid || !row.CreatedAt.Time.Equal(ev.CreatedAt) {
			return fmt.Errorf("%w: source %d changed or left the project", store.ErrHistorySourceUnavailable, key)
		}
		if source.steerSeq != 0 {
			part, err := e.store.ReadDistillationText(ctx, work.result.ReviewID, ev.ID, source.offset, len(ev.Text))
			if err != nil {
				return err
			}
			if part.MessageSeq != source.steerSeq || part.Text != ev.Text {
				return fmt.Errorf("%w: steer source %d changed", store.ErrHistorySourceUnavailable, key)
			}
		} else if ev.Kind == KindSteer {
			msg, err := e.store.DistillationSteerMessage(ctx, ev)
			if err != nil {
				return err
			}
			if msg.Content != steerModelCaption(ev.Text) {
				return fmt.Errorf("%w: steer source %d changed", store.ErrHistorySourceUnavailable, key)
			}
		}
	}
	return nil
}
