package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

// persistSteer records the timeline row before the UI can retract it, tags
// the inbox message with that seq, and stores the model-visible copy.
func (e *Engine) persistSteer(threadID, turnID string, msg *schema.Message, text string, refs []store.ImageRef) error {
	ev := store.Event{
		ThreadID: threadID, TurnID: turnID,
		Kind: KindSteer, AgentID: swarm.DefaultManagerID, Text: text,
		Images: refs,
	}
	e.recordMu.Lock()
	defer e.recordMu.Unlock()
	if _, dropped := e.droppedTurns[turnID]; dropped {
		return nil
	}
	if err := e.store.AppendEvent(&ev); err != nil {
		return err
	}
	swarm.SetSteerSeq(msg, ev.Seq)
	if err := e.store.AppendMessages(threadID, turnID, []store.Message{
		{Role: string(schema.User), Content: "[steer] " + text, Images: refs, EventSeq: ev.Seq},
	}); err != nil {
		e.log.Warn("could not persist a steer message", "turn", turnID, "err", err)
	}
	e.reindex(threadID)
	e.broadcast(ev)
	return nil
}

// Preempt aborts the current manager tool or generate so unread steering
// lands on the next model call of this turn. Workers stay up. Stop still
// cancels the whole turn.
func (e *Engine) Preempt(threadID string) error {
	if _, err := e.store.GetThread(threadID); err != nil {
		return err
	}
	e.mu.Lock()
	rt := e.runtimes[threadID]
	e.mu.Unlock()
	if rt == nil {
		return ErrIdle
	}
	rt.mu.Lock()
	reg, running, turnID := rt.reg, rt.running, rt.turnID
	rt.mu.Unlock()
	if !running || reg == nil {
		return ErrIdle
	}
	if !reg.HasPendingSteers() {
		return ErrNoPendingSteer
	}
	if !reg.Preempt() {
		return ErrIdle
	}
	e.record(store.Event{
		ThreadID: threadID, TurnID: turnID,
		Kind: KindSteerPreempted, AgentID: swarm.DefaultManagerID,
	})
	return nil
}

// RetractSteer drops one unread steering bubble. The manager never sees it.
// A steer the model already consumed stays put.
func (e *Engine) RetractSteer(threadID string, seq int64) error {
	if seq <= 0 {
		return ErrNotFound
	}
	if _, err := e.store.GetThread(threadID); err != nil {
		return err
	}
	e.mu.Lock()
	rt := e.runtimes[threadID]
	e.mu.Unlock()
	if rt == nil {
		return ErrIdle
	}
	rt.mu.Lock()
	reg, running, turnID := rt.reg, rt.running, rt.turnID
	rt.mu.Unlock()
	if !running || reg == nil {
		return ErrIdle
	}
	ev, err := e.unreadSteerEvent(threadID, turnID, seq)
	if err != nil {
		return err
	}
	if !reg.RetractManagerSteer(seq) {
		return ErrNotFound
	}
	if err := e.store.DeleteSteerMessage(threadID, seq, ev.Text); err != nil {
		e.log.Warn("could not drop a retracted steer message", "thread", threadID, "seq", seq, "err", err)
	}
	e.reindex(threadID)
	body, _ := json.Marshal(steerRetractPayload{Seq: seq})
	e.record(store.Event{
		ThreadID: threadID, TurnID: turnID,
		Kind: KindSteerRetracted, AgentID: swarm.DefaultManagerID,
		Text: string(body),
	})
	return nil
}

// ReviseUnreadSteer replaces the caption of one steer the manager has not
// read yet. The inbox slot stays put, so a model boundary cannot drain the
// old words in the gap before the new ones arrive. A steer the model already
// consumed is not found. The original steer event stays on the log; a
// steer_revised row names the seq and the caption the bubble should show.
func (e *Engine) ReviseUnreadSteer(threadID string, seq int64, text string) error {
	text = strings.TrimSpace(text)
	if seq <= 0 {
		return store.ErrNotFound
	}
	if _, err := e.store.GetThread(threadID); err != nil {
		return err
	}
	e.mu.Lock()
	rt := e.runtimes[threadID]
	e.mu.Unlock()
	if rt == nil {
		return ErrIdle
	}
	rt.mu.Lock()
	reg, running, turnID := rt.reg, rt.running, rt.turnID
	rt.mu.Unlock()
	if !running || reg == nil {
		return ErrIdle
	}
	ev, err := e.unreadSteerEvent(threadID, turnID, seq)
	if err != nil {
		return err
	}
	if text == "" && len(ev.Images) == 0 {
		return fmt.Errorf("engine: an empty steer has nothing to inject")
	}
	if text == strings.TrimSpace(ev.Text) {
		return nil
	}
	caption := steerModelCaption(text)
	if !reg.RewriteManagerSteer(seq, caption) {
		return store.ErrNotFound
	}
	if err := e.store.ReviseSteerMessage(threadID, seq, caption); err != nil {
		if !reg.RewriteManagerSteer(seq, steerModelCaption(ev.Text)) {
			e.log.Warn("could not restore a steer caption after a failed revise",
				"thread", threadID, "seq", seq, "err", err)
		}
		return err
	}
	body, err := json.Marshal(steerRevisionPayload{Seq: seq, Text: text})
	if err != nil {
		return err
	}
	e.record(store.Event{
		ThreadID: threadID, TurnID: turnID,
		Kind: KindSteerRevised, AgentID: swarm.DefaultManagerID,
		Text: string(body),
	})
	return nil
}

func steerModelCaption(text string) string {
	return strings.TrimSpace("[steer] " + strings.TrimSpace(text))
}

type steerRetractPayload struct {
	Seq int64 `json:"seq"`
}

type steerRevisionPayload struct {
	Seq  int64  `json:"seq"`
	Text string `json:"text"`
}

func parseSteerRetractSeq(text string) int64 {
	var p steerRetractPayload
	if err := json.Unmarshal([]byte(strings.TrimSpace(text)), &p); err == nil && p.Seq > 0 {
		return p.Seq
	}
	n, err := strconv.ParseInt(strings.TrimSpace(text), 10, 64)
	if err != nil || n <= 0 {
		return 0
	}
	return n
}

func (e *Engine) unreadSteerEvent(threadID, turnID string, seq int64) (store.Event, error) {
	events, err := e.store.ListEvents(threadID, 0, 0)
	if err != nil {
		return store.Event{}, err
	}
	retracted := retractedSteerSeqs(events)
	if _, ok := retracted[seq]; ok {
		return store.Event{}, ErrNotFound
	}
	for _, ev := range events {
		if ev.Seq != seq || ev.Kind != KindSteer || ev.TurnID != turnID {
			continue
		}
		return ev, nil
	}
	return store.Event{}, ErrNotFound
}

func parseSteerRevision(text string) (int64, string, bool) {
	var p struct {
		Seq  int64   `json:"seq"`
		Text *string `json:"text"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(text)), &p); err != nil || p.Seq <= 0 || p.Text == nil {
		return 0, "", false
	}
	return p.Seq, *p.Text, true
}

func revisedSteerText(events []store.Event) map[int64]string {
	out := map[int64]string{}
	for _, ev := range events {
		if ev.Kind != KindSteerRevised {
			continue
		}
		seq, text, ok := parseSteerRevision(ev.Text)
		if !ok {
			continue
		}
		out[seq] = text
	}
	return out
}

func retractedSteerSeqs(events []store.Event) map[int64]struct{} {
	out := map[int64]struct{}{}
	for _, ev := range events {
		if ev.Kind != KindSteerRetracted {
			continue
		}
		if seq := parseSteerRetractSeq(ev.Text); seq > 0 {
			out[seq] = struct{}{}
		}
	}
	return out
}

func (e *Engine) retractedSteerSeqs(threadID string) map[int64]struct{} {
	events, err := e.store.ListEvents(threadID, 0, 0)
	if err != nil {
		return nil
	}
	return retractedSteerSeqs(events)
}

func isPreemptRunError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) {
		return true
	}
	return strings.Contains(err.Error(), "context canceled")
}

func skipRetractedSteer(row store.Message, retracted map[int64]struct{}, captions map[string]struct{}) bool {
	if schema.RoleType(row.Role) != schema.User {
		return false
	}
	if row.EventSeq > 0 {
		if retracted == nil {
			return false
		}
		_, ok := retracted[row.EventSeq]
		return ok
	}
	if captions == nil {
		return false
	}
	_, ok := captions[strings.TrimSpace(row.Content)]
	return ok
}

func retractedSteerCaptionsFrom(events []store.Event) map[string]struct{} {
	seqs := retractedSteerSeqs(events)
	if len(seqs) == 0 {
		return nil
	}
	out := map[string]struct{}{}
	for _, ev := range events {
		if ev.Kind != KindSteer {
			continue
		}
		if _, ok := seqs[ev.Seq]; !ok {
			continue
		}
		out["[steer] "+ev.Text] = struct{}{}
	}
	return out
}

func (e *Engine) unreadSteerMessages(turn *store.Turn) []*schema.Message {
	if e == nil || turn == nil {
		return nil
	}
	events, err := e.store.ListTurnEvents(turn.ID)
	if err != nil {
		return nil
	}
	rows, _ := e.store.ListMessages(turn.ThreadID)
	bySeq := map[int64]store.Message{}
	for _, r := range rows {
		if r.TurnID == turn.ID && r.EventSeq > 0 {
			bySeq[r.EventSeq] = r
		}
	}
	retracted := retractedSteerSeqs(events)
	var out []*schema.Message
	for _, ev := range events {
		if ev.Kind != KindSteer {
			continue
		}
		if _, ok := retracted[ev.Seq]; ok {
			continue
		}
		if steerConsumedAfter(events, ev.Seq) {
			continue
		}
		var msg *schema.Message
		if row, ok := bySeq[ev.Seq]; ok {
			msg = e.schemaUser(row)
		} else {
			msg = schema.UserMessage("[steer] " + ev.Text)
		}
		if msg == nil {
			continue
		}
		swarm.SetSteerSeq(msg, ev.Seq)
		out = append(out, msg)
	}
	return out
}

func steerConsumedAfter(events []store.Event, seq int64) bool {
	for _, ev := range events {
		if ev.Seq <= seq {
			continue
		}
		switch ev.Kind {
		case swarm.NotifyAgentMessage.String(), swarm.NotifyToolCall.String(), KindSteerPreempted:
			if isManagerAgent(ev.AgentID) {
				return true
			}
		}
	}
	return false
}

func dropSteerMessages(msgs []adk.Message, queued []*schema.Message) []adk.Message {
	if len(queued) == 0 {
		return msgs
	}
	dropSeq := map[int64]struct{}{}
	dropText := map[string]struct{}{}
	for _, m := range queued {
		if m == nil {
			continue
		}
		if seq := swarm.SteerSeq(m); seq > 0 {
			dropSeq[seq] = struct{}{}
		}
		if t := strings.TrimSpace(userMessageText(m)); t != "" {
			dropText[t] = struct{}{}
		}
	}
	out := make([]adk.Message, 0, len(msgs))
	for _, m := range msgs {
		if isSteerUser(m) {
			if _, ok := dropSeq[swarm.SteerSeq(m)]; ok {
				continue
			}
			if _, ok := dropText[strings.TrimSpace(userMessageText(m))]; ok {
				continue
			}
		}
		out = append(out, m)
	}
	return out
}
