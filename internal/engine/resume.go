package engine

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/LubyRuffy/eino-swarm/internal/tools"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

// resumeNotice is the timeline row. Short, task-agnostic, and stable: the
// front end matches on the kind, but the text is what a trace shows.
const resumeNotice = "the previous run was interrupted; continuing"

// resumeCue is appended for the model only. The workspace still has whatever
// the previous process wrote; this tells the manager to pick up from there
// rather than treat the original request as brand new.
const resumeCue = "The previous process stopped before this request finished. Continue from the current workspace and conversation. Do not redo work that is already complete."

// ResumeOrphanedTurns continues every turn left running by a crash or a
// process exit. A user Interrupt is recorded as cancelled and is not resumed.
func (e *Engine) ResumeOrphanedTurns() (int, error) {
	turns, err := e.store.ListRunningTurns()
	if err != nil {
		return 0, err
	}
	resume, drop := pickOrphanedTurns(turns)
	for i := range drop {
		t := drop[i]
		errText := "superseded by a later unfinished turn"
		_ = e.store.FinishTurn(t.ID, store.TurnCancelled, "", errText)
		e.record(store.Event{
			ThreadID: t.ThreadID, TurnID: t.ID,
			Kind: swarm.NotifyError.String(), AgentID: swarm.DefaultManagerID,
			Err: errText,
		})
	}
	n := 0
	for i := range resume {
		t := resume[i]
		if err := e.resumeTurn(&t); err != nil {
			e.log.Warn("could not resume a leftover turn", "turn", t.ID, "thread", t.ThreadID, "err", err)
			if errors.Is(err, ErrBusy) {
				continue
			}
			_ = e.store.FinishTurn(t.ID, store.TurnError, "", err.Error())
			e.record(store.Event{
				ThreadID: t.ThreadID, TurnID: t.ID,
				Kind: swarm.NotifyError.String(), AgentID: swarm.DefaultManagerID,
				Err: err.Error(),
			})
			continue
		}
		n++
	}
	return n, nil
}

// pickOrphanedTurns keeps the latest running turn per conversation. Two
// running rows on one thread cannot both be live, and resuming the older one
// would 409 the later one.
func pickOrphanedTurns(turns []store.Turn) (resume, drop []store.Turn) {
	latest := make(map[string]store.Turn, len(turns))
	for _, t := range turns {
		prev, ok := latest[t.ThreadID]
		if !ok {
			latest[t.ThreadID] = t
			continue
		}
		if t.Seq > prev.Seq {
			drop = append(drop, prev)
			latest[t.ThreadID] = t
			continue
		}
		drop = append(drop, t)
	}
	resume = make([]store.Turn, 0, len(latest))
	for _, t := range latest {
		resume = append(resume, t)
	}
	sort.Slice(resume, func(i, j int) bool {
		if resume[i].StartedAt.Equal(resume[j].StartedAt) {
			return resume[i].ID < resume[j].ID
		}
		return resume[i].StartedAt.Before(resume[j].StartedAt)
	})
	return resume, drop
}

func (e *Engine) resumeTurn(turn *store.Turn) error {
	th, err := e.store.GetThread(turn.ThreadID)
	if err != nil {
		return err
	}
	history, err := e.replayHistory(turn.ThreadID)
	if err != nil {
		return err
	}
	text := strings.TrimSpace(turn.UserText)
	if text == "" && !hasUserMessage(history) {
		return fmt.Errorf("engine: leftover turn has no request to continue")
	}
	messages := resumeMessages(history, text)

	pc, err := e.projectContextFor(th)
	if err != nil {
		return err
	}
	toolset, err := tools.Build(context.Background(), e.cfg, e.WorkspaceDir(turn.ThreadID))
	if err != nil {
		return err
	}

	providerID := strings.TrimSpace(turn.ProviderID)
	if providerID == "" {
		providerID = th.ProviderID
	}
	prov, err := e.pool.ResolveModel(providerID, turn.Model)
	if err != nil {
		return err
	}
	effort := turn.ReasoningEffort
	if effort == "" {
		effort = th.ReasoningEffort
	}
	builder, err := e.pool.ModelBuilder(context.Background(), prov.ID, prov.Model, effort, e.callRecorder(turn.ThreadID, turn.ID))
	if err != nil {
		return err
	}

	reg := e.newTurnRegistry(builder, toolset)

	rt := e.runtimeFor(turn.ThreadID)
	ctx, cancel := context.WithCancel(context.Background())
	idle := make(chan struct{})
	if !rt.occupy(reg, cancel, turn.ID, idle) {
		cancel()
		reg.Close()
		return ErrBusy
	}

	_ = e.store.TouchThread(turn.ThreadID)
	go rt.run(ctx, cancel, idle, turn, reg, toolset, pc, messages, len(messages), e.imagesForTurn(turn), true)
	return nil
}

func (e *Engine) imagesForTurn(turn *store.Turn) []store.ImageRef {
	rows, err := e.store.ListMessages(turn.ThreadID)
	if err != nil {
		return nil
	}
	for _, row := range rows {
		if row.TurnID == turn.ID && row.Role == string(schema.User) &&
			!strings.HasPrefix(strings.TrimSpace(row.Content), "[steer]") &&
			len(row.Images) > 0 {
			return row.Images
		}
	}
	return nil
}

func resumeMessages(history []adk.Message, userText string) []adk.Message {
	out := append([]adk.Message{}, history...)
	if userText != "" && !hasUserMessage(out) {
		out = append(out, schema.UserMessage(userText))
	}
	if !lastUserIs(out, resumeCue) {
		out = append(out, schema.UserMessage(resumeCue))
	}
	return out
}

func hasUserMessage(msgs []adk.Message) bool {
	for _, m := range msgs {
		if m != nil && m.Role == schema.User && (strings.TrimSpace(m.Content) != "" || len(m.UserInputMultiContent) > 0) {
			return true
		}
	}
	return false
}

func lastUserIs(msgs []adk.Message, text string) bool {
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i] == nil {
			continue
		}
		if msgs[i].Role == schema.User {
			return msgs[i].Content == text
		}
	}
	return false
}

func (e *Engine) turnHasKind(turnID, kind string) bool {
	events, err := e.store.ListTurnEvents(turnID)
	if err != nil {
		return false
	}
	for _, ev := range events {
		if ev.Kind == kind {
			return true
		}
	}
	return false
}
