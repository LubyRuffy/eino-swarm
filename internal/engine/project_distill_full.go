package engine

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/skills"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

func (e *Engine) distillationFullSlice(ctx context.Context, job *projectDistillWork, cursor uint, rows []store.DistillationSourceRow, maxBytes int) (*projectDistillWork, error) {
	calls, err := e.store.DistillationCallHeads(ctx, distillationCallIDs(rows))
	if err != nil {
		return nil, err
	}
	batch := newDistillationBatch(e, job, cursor)
	batch.sources = distillInput{sources: make(map[int64]distillSource), memory: make(map[int64]store.Event), wiki: make(map[int64]store.Event)}
	checkpoint := batch.checkpoint
	offset := job.result.Progress.SegmentBytes
	var transcript strings.Builder
	transcript.WriteString(reviewSourceHeader)
	for _, row := range rows {
		if row.WikiReviewed && offset == 0 {
			checkpoint.to, checkpoint.count = row.Position, checkpoint.count+1
			continue
		}
		handle := int64(len(batch.sources.sources) + 1)
		header := distillationFullHeader(row, handle, offset, calls[row.CallID])
		room := maxBytes - transcript.Len() - len(header) - len("\n\n")
		if header != "" && room < utf8.UTFMax {
			if len(batch.sources.sources) == 0 {
				return nil, errors.New("distillation source identity exceeds the review budget")
			}
			break
		}
		part, err := e.distillationFullText(ctx, job, row.Position, header, offset, room)
		if err != nil {
			return nil, err
		}
		if part.Text == "" {
			checkpoint.skipped++
		} else {
			transcript.WriteString(distillationFullHeader(row, handle, part.Offset, calls[row.CallID]))
			transcript.WriteString(part.Text)
			transcript.WriteString("\n\n")
			batch.addDistillationPart(handle, row, part)
			next := part.Offset + len(part.Text)
			if next < part.TotalBytes {
				// Keep the queue cursor at the preceding completed record.
				// The byte offset and output receipts commit together.
				checkpoint.segmentBytes = next
				checkpoint.segmentTotalBytes = part.TotalBytes
				break
			}
			if job.result.WikiGraph {
				checkpoint.wikiReviewed = append(checkpoint.wikiReviewed, row.Position)
			}
		}
		checkpoint.to, checkpoint.count = row.Position, checkpoint.count+1
		offset = 0
	}
	batch.sources.transcript = transcript.String()
	return batch, nil
}

func distillationFullHeader(row store.DistillationSourceRow, handle int64, offset int, call store.Event) string {
	if row.ID == 0 {
		return ""
	}
	ev := row.Event
	role, toolContext := "human", ""
	switch ev.Kind {
	case KindUser, KindSteer:
	case swarm.NotifyAgentMessage.String():
		role = "assistant"
	case swarm.NotifyToolResult.String():
		name := toolCallName(call.Text)
		if name == "" || name == skills.ToolLoad || derivedDistillationTool(name) {
			return ""
		}
		role, toolContext = "tool", fmt.Sprintf(" paired tool=%q", name)
	default:
		return ""
	}
	status := "done"
	if row.Turn.Status == store.TurnError {
		status = "failed"
	}
	return fmt.Sprintf("[seq=%d kind=%s thread=%q event_seq=%d turn_status=%s byte_offset=%d%s] %s: ",
		handle, ev.Kind, ev.ThreadID, ev.Seq, status, offset, toolContext, role)
}

func (e *Engine) distillationFullText(ctx context.Context, job *projectDistillWork, position uint, header string, offset, room int) (store.DistillationText, error) {
	if header == "" {
		return store.DistillationText{}, nil
	}
	// One extra byte reveals whether the end of the window falls inside a
	// rune, without fetching the rest of the record into the Go process.
	read := e.store.ReadDistillationText
	start := offset
	if job.result.WikiGraph {
		read = e.store.ReadWikiSourceText
		// Preserve context for names, relationships and events straddling a
		// segment boundary. The checkpoint only advances by newly read bytes.
		start = max(0, offset-384)
	}
	part, err := read(ctx, job.result.ReviewID, position, start, room+1)
	if errors.Is(err, store.ErrHistorySourceUnavailable) {
		return store.DistillationText{}, nil
	}
	if err != nil {
		return part, err
	}
	if offset > 0 && part.TotalBytes != job.result.Progress.SegmentTotalBytes {
		return store.DistillationText{}, nil
	}
	end := min(room, len(part.Text))
	for end > 0 && end < len(part.Text) && !utf8.RuneStart(part.Text[end]) {
		end--
	}
	for part.Offset < offset && part.Offset-start < end && !utf8.RuneStart(part.Text[part.Offset-start]) {
		part.Offset++
	}
	part.Text = part.Text[part.Offset-start : end]
	if !utf8.ValidString(part.Text) || (offset == 0 && end == part.TotalBytes && strings.TrimSpace(part.Text) == "") {
		return store.DistillationText{}, nil
	}
	return part, nil
}

func (batch *projectDistillWork) addDistillationPart(handle int64, row store.DistillationSourceRow, part store.DistillationText) {
	ev := row.Event
	ev.Text = part.Text
	failed := row.Turn.Status == store.TurnError
	batch.sources.sources[handle] = distillSource{event: ev, failed: failed, offset: part.Offset, steerSeq: part.MessageSeq}
	if ev.Kind != swarm.NotifyAgentMessage.String() {
		batch.sources.memory[handle] = ev
	}
	if !failed {
		batch.sources.wiki[handle] = ev
	}
	batch.anchor = row.Turn
	batch.result.ThreadID, batch.result.TurnID = row.Turn.ThreadID, row.Turn.ID
}
