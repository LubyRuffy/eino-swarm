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

// resumeToolStopped is the tool_result for an in-flight call the previous
// process never finished. Keep in sync with the front-end reducer: a killed
// exec must not keep spinning. Task-agnostic on purpose.
const resumeToolStopped = "the previous process stopped"

// resumeCue is appended for the model only. The workspace still has whatever
// the previous process wrote; this tells the manager to pick up from there
// rather than treat the original request as brand new.
const resumeCue = "The previous process stopped before this request finished. Continue from the current workspace and conversation. Do not redo work that is already complete."

// ResumeOrphanedTurns continues every turn left running by a crash or a
// process exit. A user Interrupt is recorded as cancelled and is not resumed.
// Independent conversations continue after a failure; the result counts accepted
// resumptions and reports every failed conversation through RestoreFailures.
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
		e.finishScheduledRun(&t, store.TurnCancelled, "", errText)
		e.record(store.Event{
			ThreadID: t.ThreadID, TurnID: t.ID,
			Kind: swarm.NotifyError.String(), AgentID: swarm.DefaultManagerID,
			Err: errText,
		})
	}
	n := 0
	var failures []error
	for i := range resume {
		e.restoreMu.Lock()
		snapshot, err := e.loadRestoreSnapshot(context.Background(), resume[i].ThreadID)
		count := 0
		if err == nil {
			count, err = e.resumeOrphanedTurn(context.Background(), resume[i], snapshot)
		}
		e.restoreMu.Unlock()
		n += count
		if err != nil {
			failures = append(failures, fmt.Errorf("restore conversation %s: %w", resume[i].ThreadID, err))
		}
	}
	if len(failures) > 0 {
		return n, &RestoreFailures{Failed: len(failures), Err: errors.Join(failures...)}
	}
	return n, nil
}

func (e *Engine) resumeOrphanedTurn(ctx context.Context, t store.Turn, snapshot *restoreSnapshot) (int, error) {
	if reconciled, err := e.convergeTerminalTurn(&t, snapshot); err != nil {
		return 0, err
	} else if reconciled {
		return 0, nil
	}
	checkpoint := e.checkpointForTurn(t.ID, snapshot)
	if e.turnHasKind(t.ID, KindUserStopRequested, snapshot) || checkpoint != nil && checkpoint.HumanStopped {
		e.stopOrphanedGenerations(t.ThreadID, true, snapshot)
		e.record(store.Event{ThreadID: t.ThreadID, TurnID: t.ID, Kind: swarm.NotifyError.String(), AgentID: swarm.DefaultManagerID, Err: "interrupted", SuppressCompletion: true})
		if err := e.store.FinishTurn(t.ID, store.TurnCancelled, "", "interrupted"); err != nil {
			return 0, err
		}
		e.finishScheduledRun(&t, store.TurnCancelled, "", "interrupted")
		e.runtimeFor(t.ThreadID).pauseOpenGoalOnInterrupt()
		return 0, nil
	}
	if err := e.resumeTurn(ctx, &t, snapshot); err != nil {
		if ctx.Err() != nil {
			return 0, ctx.Err()
		}
		e.log.Warn("could not resume a leftover turn", "turn", t.ID, "thread", t.ThreadID, "err", err)
		if errors.Is(err, ErrBusy) {
			return 0, nil
		}
		e.stopOrphanedGenerations(t.ThreadID, false, snapshot)
		_ = e.store.FinishTurn(t.ID, store.TurnError, "", err.Error())
		if !errors.Is(err, store.ErrScheduleRunConflict) {
			e.finishScheduledRun(&t, store.TurnError, "", err.Error())
		}
		e.record(store.Event{
			ThreadID: t.ThreadID, TurnID: t.ID,
			Kind: swarm.NotifyError.String(), AgentID: swarm.DefaultManagerID,
			Err: err.Error(),
		})
		e.blockOpenGoalOnTurnError(t.ThreadID)
		return 0, err
	}
	return 1, nil
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

func (e *Engine) resumeTurn(restoreCtx context.Context, turn *store.Turn, snapshot *restoreSnapshot) error {
	if err := restoreCtx.Err(); err != nil {
		return err
	}
	th, err := e.store.GetThread(turn.ThreadID)
	if err != nil {
		return err
	}
	if turn.ScheduleContinue && strings.TrimSpace(th.Goal) != "" {
		if err := e.bindScheduledTurn(turn); err != nil {
			return err
		}
	}
	messages, err := e.resumeConversation(turn, snapshot)
	if err != nil {
		return err
	}
	pc, err := e.projectContextFor(th)
	if err != nil {
		return err
	}
	toolset, err := tools.Build(restoreCtx, e.cfg, e.WorkspaceDir(turn.ThreadID))
	if err != nil {
		return err
	}
	if th.PlanMode {
		toolset = tools.ExploreOnly(toolset)
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
	builder, err := e.pool.ModelBuilder(restoreCtx, prov.ID, prov.Model, effort, e.callRecorder(turn.ThreadID, turn.ID))
	if err != nil {
		return err
	}

	if err := restoreCtx.Err(); err != nil {
		return err
	}
	reg := e.newTurnRegistry(builder, toolset, pc)
	unread := e.unreadSteerMessages(turn, snapshot)
	messages = dropSteerMessages(messages, unread)

	rt := e.runtimeFor(turn.ThreadID)
	ctx, cancel := context.WithCancel(context.Background())
	idle := make(chan struct{})
	if !rt.occupy(reg, cancel, turn.ID, idle) {
		cancel()
		reg.Close()
		return ErrBusy
	}
	for _, m := range unread {
		_ = reg.SteerManagerMessage(m)
	}
	rt.setWorkerRestore(e.workersFromThread(turn.ThreadID, snapshot))
	if checkpoint := e.checkpointForTurn(turn.ID, snapshot); checkpoint != nil {
		if checkpoint.Pause == "cap" {
			rt.mu.Lock()
			rt.restartCap = checkpoint
			rt.mu.Unlock()
		}
		for _, msg := range checkpoint.ManagerInbox {
			duplicate := false
			for _, delivered := range unread {
				if sameRestartMessage(msg, delivered) {
					duplicate = true
				}
			}
			if !duplicate && msg != nil {
				_ = reg.SteerManagerMessage(msg)
			}
		}
	}

	e.interruptOrphanedGenerations(turn.ThreadID, e.checkpointForTurn(turn.ID, snapshot) != nil, snapshot)
	_ = e.store.TouchThread(turn.ThreadID)
	go rt.run(ctx, cancel, idle, turn, reg, toolset, pc, messages, len(messages), e.imagesForTurn(turn), true, snapshot)
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

func (e *Engine) turnHasKind(turnID, kind string, snapshots ...*restoreSnapshot) bool {
	events, err := e.restoreTurnEvents(turnID, snapshots...)
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

type orphanedToolCall struct {
	AgentID    string
	Role       string
	ToolCallID string
	Text       string
}

func orphanedToolCalls(events []store.Event) []orphanedToolCall {
	events = restartAskEvents(events)
	type callKey struct{ agent, id string }
	open := make(map[callKey]orphanedToolCall)
	order := make([]callKey, 0)
	for _, ev := range events {
		id := strings.TrimSpace(ev.ToolCallID)
		if id == "" {
			continue
		}
		agent := ev.AgentID
		if isManagerAgent(agent) {
			agent = swarm.DefaultManagerID
		}
		key := callKey{agent: agent, id: id}
		switch ev.Kind {
		case swarm.NotifyToolCall.String():
			if _, ok := open[key]; !ok {
				order = append(order, key)
			}
			open[key] = orphanedToolCall{AgentID: ev.AgentID, Role: ev.Role, ToolCallID: id, Text: ev.Text}
		case swarm.NotifyToolResult.String():
			delete(open, key)
		}
	}
	out := make([]orphanedToolCall, 0, len(open))
	for _, id := range order {
		if c, ok := open[id]; ok {
			out = append(out, c)
		}
	}
	return out
}

func (e *Engine) closeOrphanedToolCalls(turn *store.Turn, snapshots ...*restoreSnapshot) {
	if e == nil || turn == nil {
		return
	}
	events, err := e.restoreTurnEvents(turn.ID, snapshots...)
	if err != nil {
		e.log.Warn("could not list in-flight tools for resume", "turn", turn.ID, "err", err)
		return
	}
	running, finished := e.workersFromThread(turn.ThreadID, snapshots...)
	for _, c := range orphanedToolCalls(events) {
		if isAskUserToolText(c.Text) {
			// Re-arm the wait in run(); closing it as stopped would swallow
			// the question the human never saw a chance to answer.
			continue
		}
		agent := c.AgentID
		if agent == "" {
			agent = swarm.DefaultManagerID
		}
		text, errText := resumeToolStopped, resumeToolStopped
		if toolCallName(c.Text) == waitAgentsToolName {
			_, args := splitToolCallText(c.Text)
			ids := waitAgentIDsFromArgs(args)
			if len(ids) == 0 {
				for _, w := range running {
					ids = append(ids, w.ID)
				}
			}
			text = resumeWaitSnapshotJSON(ids, running, finished)
			errText = ""
		}
		e.record(store.Event{
			ThreadID:   turn.ThreadID,
			TurnID:     turn.ID,
			Kind:       swarm.NotifyToolResult.String(),
			AgentID:    agent,
			Role:       c.Role,
			ToolCallID: c.ToolCallID,
			Text:       text,
			Err:        errText,
		})
	}
}
