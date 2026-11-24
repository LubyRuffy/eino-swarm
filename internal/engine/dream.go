package engine

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/LubyRuffy/eino-swarm/internal/memory"
	"github.com/LubyRuffy/eino-swarm/internal/store"
)

const dreamCorrectionFence = "```memory-correction"

type dreamCorrection struct{ name, before, after string }

type dreamReceipt struct {
	Status    string `json:"status"`
	SourceSeq int64  `json:"source_seq,omitempty"`
	SkillName string `json:"skill_name,omitempty"`
}

// A correction is an explicit, entire human message. Similar prose, a fence
// inside a quotation, and tool output have no authority to modify memory.
func dreamFrame(text string) (string, bool) {
	frame, ok := strings.CutPrefix(strings.TrimSpace(text), dreamCorrectionFence+"\n")
	return frame, ok && strings.HasSuffix(frame, "\n```")
}

func parseDreamCorrection(frame string) (dreamCorrection, bool) {
	body, closed := strings.CutSuffix(frame, "\n```")
	if !closed || len(body) > 64*1024 {
		return dreamCorrection{}, false
	}
	d := json.NewDecoder(strings.NewReader(body))
	if token, err := d.Token(); err != nil || token != json.Delim('{') {
		return dreamCorrection{}, false
	}
	var c dreamCorrection
	fields := map[string]*string{"name": &c.name, "before": &c.before, "after": &c.after}
	for d.More() {
		key, err := d.Token()
		name, _ := key.(string)
		dst := fields[name]
		if err != nil || dst == nil || d.Decode(dst) != nil || strings.TrimSpace(*dst) == "" {
			return dreamCorrection{}, false
		}
		delete(fields, name)
	}
	if token, err := d.Token(); err != nil || token != json.Delim('}') || len(fields) != 0 || d.Decode(new(any)) != io.EOF {
		return dreamCorrection{}, false
	}
	return c, memory.ValidSkillName(c.name) == nil
}

// scheduleDream consumes explicit correction frames. Its operation replaces
// the ordinary extraction pass for this request and adds no model call.
func (e *Engine) scheduleDream(turn *store.Turn, status string, pc *projectContext) bool {
	frame, ok := dreamFrame(turn.UserText)
	if !ok {
		return false
	}
	if status != store.TurnDone || turn.GoalContinue || turn.ScheduleContinue || turn.NuwaReadOnly ||
		pc == nil || pc.project == nil || pc.project.ID != turn.ProjectID || pc.memory == nil || pc.planning {
		return true
	}
	gate := e.reviews.begin(pc.reviewKey())
	if gate == nil {
		return true
	}
	// Do not retain the runtime's mutable pointer in the background job.
	threadID, turnID, projectID := turn.ThreadID, turn.ID, turn.ProjectID
	go func() {
		defer e.reviews.done()
		gate <- struct{}{}
		defer func() { <-gate }()
		e.applyDream(threadID, turnID, projectID, frame, pc.memory)
	}()
	return true
}

func (e *Engine) applyDream(threadID, turnID, projectID, frame string, mem *memory.Store) {
	done, err := e.beginAdmission()
	if err != nil {
		return
	}
	defer done()

	// The durable owner and evidence must still exist when the file changes.
	// Deletion/move and rewind share these short locks, in this order.
	e.generations.Lock()
	defer e.generations.Unlock()
	e.recordMu.Lock()
	defer e.recordMu.Unlock()
	if _, dropped := e.droppedTurns[turnID]; dropped {
		return
	}
	th, err := e.store.GetThread(threadID)
	if err != nil || projectID == "" || th.ProjectID != projectID || th.PlanMode {
		return
	}
	turn, err := e.store.GetTurn(turnID)
	if err != nil || turn.ThreadID != threadID || turn.ProjectID != projectID || turn.Status != store.TurnDone {
		return
	}
	if current, ok := dreamFrame(turn.UserText); !ok || current != frame {
		return
	}
	project, err := e.store.GetProject(projectID)
	if err != nil || !project.MemoryEnabled {
		return
	}
	settings := e.cfg.Snapshot().Memory
	if !settings.Enabled || !settings.AutoReview || !settings.AutoDream {
		return
	}
	seq := e.dreamSource(turn)
	if seq == 0 {
		return
	}
	c, valid := parseDreamCorrection(frame)
	out := reviewOutcome{Notify: settings.NotifyLevel(), StopReason: "error",
		Dream: &dreamReceipt{Status: "refused", SourceSeq: seq}}
	if !valid {
		out.Err = "Provide one existing skill name and its complete before and after bodies."
	} else {
		out.Dream.SkillName = c.name
		change, changed, err := mem.CorrectSkill(c.name, c.before, c.after, fmt.Sprintf("%s:%s:%d", threadID, turnID, seq))
		switch {
		case err != nil:
			out.Err = "The exact recorded skill could not be corrected. Reopen it and check the current version."
		case changed:
			collectReviewChange(&out, change)
			out.Dream.Status, out.StopReason, out.Note = "applied", "completed", "Applied the explicit skill correction."
		default:
			out.Dream.Status, out.StopReason, out.Note = "unchanged", "completed", "The requested correction does not change the skill body."
		}
	}
	// Do not broadcast a successful correction receipt unless it persisted.
	ev := store.Event{ThreadID: threadID, TurnID: turnID, AgentID: ReviewAgentID, Kind: KindMemoryReview, Err: out.Err}
	raw, _ := json.Marshal(out)
	ev.Text = string(raw)
	if err := e.store.AppendEvent(&ev); err != nil {
		e.logPersistFailure("could not persist a memory correction receipt", err, "turn", turnID)
		return
	}
	e.broadcast(ev)
}

func (e *Engine) dreamSource(turn *store.Turn) int64 {
	// The indexed turn lookup disallows legacy rows with unknown provenance.
	var count int64
	if err := e.store.DB().Model(&store.Message{}).
		Where("thread_id = ? AND turn_id = ? AND role = ? AND source = ? AND event_seq = 0 AND content = ?",
			turn.ThreadID, turn.ID, "user", store.MessageSourceHuman, turn.UserText).Count(&count).Error; err != nil || count != 1 {
		return 0
	}
	events, err := e.store.ListTurnEvents(turn.ID)
	if err != nil {
		return 0
	}
	var seq int64
	for _, ev := range events {
		if ev.Kind == KindResumed || ev.Kind == KindRestartResumed {
			return 0
		}
		if ev.Kind == KindMemoryReview {
			var prior struct{ Dream *dreamReceipt }
			if json.Unmarshal([]byte(ev.Text), &prior) == nil && prior.Dream != nil {
				return 0
			}
		}
		if ev.ThreadID == turn.ThreadID && ev.Kind == KindUser && ev.Text == turn.UserText &&
			ev.SourceThreadID == "" && ev.SourceTurnID == "" && len(ev.Images) == 0 {
			if seq != 0 {
				return 0
			}
			seq = ev.Seq
		}
	}
	return seq
}
