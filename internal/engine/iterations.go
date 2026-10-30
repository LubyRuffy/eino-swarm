package engine

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"

	swarm "github.com/LubyRuffy/eino-swarm"
	"github.com/LubyRuffy/eino-swarm/internal/store"
	"github.com/LubyRuffy/eino-swarm/internal/tools"
	"github.com/LubyRuffy/eino-swarm/internal/workerstate"
	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

// limitStopError is the turn ending because the human declined to extend
// past the manager's tool-round cap. Distinct from eino's NodeRunError so
// the transcript can say what happened without looking like a crash.
type limitStopError struct {
	Used   int
	Reason string
}

func (e limitStopError) Error() string {
	if e.Reason == "workers_uncollected" {
		return "stopped before collecting worker results"
	}
	return fmt.Sprintf("stopped after %d tool rounds", e.Used)
}

func isLimitStop(err error) bool {
	var stop limitStopError
	return errors.As(err, &stop)
}

// isMaxIterations reports whether err is eino's ReAct cap, including the
// NodeRunError wrapper the graph puts around the preprocessor failure.
func isMaxIterations(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, adk.ErrExceedMaxIterations) {
		return true
	}
	return strings.Contains(err.Error(), adk.ErrExceedMaxIterations.Error())
}

func (rt *runtime) runManager(ctx context.Context, turn *store.Turn, reg *swarm.Registry,
	acc *accumulator, toolset *tools.Set, pc *projectContext, messages []adk.Message,
	snapshots ...*restoreSnapshot,
) (swarm.RunResult, error) {
	e := rt.engine
	// Bind the actual owner before restoring or spawning workers. A worker may
	// outlive this manager check and must keep this original turn identity.
	ctx = withHistoryReadOwner(ctx, turn.ThreadID, turn.ID)
	rt.mu.Lock()
	paused := rt.restartCap
	rt.restartCap = nil
	rt.mu.Unlock()
	proceed := true
	collectionPause, _, stateErr := e.workerCollectionWaitState(turn, snapshots...)
	if stateErr != nil {
		return swarm.RunResult{}, stateErr
	}
	// Durable tail decisions supersede an older handoff checkpoint.
	if paused != nil && paused.Pause == "worker_collection" && collectionPause == nil {
		paused = nil
	}
	if collectionPause != nil && collectionPause.Decision != nil {
		proceed = *collectionPause.Decision
	} else if collectionPause != nil && (paused == nil || paused.CapDecision == nil) {
		if err := rt.restorePendingScheduleWorkers(ctx, reg); err != nil {
			return swarm.RunResult{}, err
		}
		var err error
		proceed, _, err = rt.waitForWorkerCollection(ctx, turn, reg, collectionPause)
		if err != nil {
			return swarm.RunResult{}, err
		}
		snapshots = nil
	} else if paused != nil {
		if paused.CapDecision != nil {
			proceed = *paused.CapDecision
		} else {
			proceed = rt.waitToExtend(ctx, turn, paused.CapUsed, paused.CapExtend)
			// A new human continuation is later work, outside the startup
			// snapshot. Read its durable allowance receipt from the live log.
			snapshots = nil
		}
	}
	if !proceed {
		if ctx.Err() != nil {
			return swarm.RunResult{}, ctx.Err()
		}
		used := 0
		if paused != nil {
			used = paused.CapUsed
		} else if collectionPause != nil {
			used = collectionPause.Limit
		}
		reason := ""
		if collectionPause != nil {
			reason = "workers_uncollected"
		}
		return swarm.RunResult{}, limitStopError{Used: used, Reason: reason}
	}
	var th *store.Thread
	if got, err := e.store.GetThread(rt.threadID); err == nil {
		th = got
	}
	humanReply, err := e.pausedGoalAnswerSection(th, turn)
	if err != nil {
		return swarm.RunResult{}, err
	}
	audience := historyForManager
	if report, _, failure := e.historyAnswerMode(turn); failure != nil {
		return swarm.RunResult{}, failure
	} else if report {
		audience = historyForManagerReport
	}
	instruction := ManagerPrompt(toolset, JoinPromptSections(historyPrompt(toolset, audience), taskCoordinationPrompt,
		managerExtra(e.cfg, th, pc, e.scheduleLines(rt.threadID)), humanReply, rt.finalGoalWorkerSection(th)))
	// Fresh slice: pc.managerTools may return the workspace toolset's
	// backing array, and appending complete_goal / block_goal in place
	// would hand them to every sub-agent.
	managerTools := append([]tool.BaseTool(nil), pc.managerTools(toolset)...)
	if th != nil && th.PlanMode {
		managerTools = dropPlanMutatingManagerTools(managerTools)
		tid := rt.threadID
		managerTools = append(managerTools, ProposePlanTool(func(markdown string) (string, error) {
			return e.proposePlanJSON(tid, markdown)
		}))
	}
	if pursuingGoal(th) && (th == nil || !th.PlanMode) {
		tid := rt.threadID
		managerTools = append(managerTools,
			CompleteGoalTool(func(summary string) (string, error) {
				return e.completeGoalForTurnJSON(tid, turn.ID, summary)
			}),
			BlockGoalTool(func(reason string) (string, error) {
				return e.blockGoalJSON(tid, reason)
			}),
			ReopenGoalTool(func(reason string) (string, error) {
				return e.reopenGoalJSON(tid, reason)
			}),
		)
	}
	managerTools = append(managerTools, AskUserTool(rt.waitAsk))
	managerTools = append(managerTools, rt.managerScheduleTools(turn)...)
	managerTools = append(managerTools, rt.managerTaskTools(turn)...)
	budget := e.cfg.SwarmSnapshot().ManagerIterations()
	if pursuingGoal(th) && !pausedStandingGoal(th) {
		budget = e.cfg.SwarmSnapshot().GoalSessionIterations()
	}
	used := 0
	if collectionPause != nil {
		used = collectionPause.Limit
	}
	modelRetries := 0
	overflowRetries := 0
	forceCompact := false
	scheduleContinued := false
	answerContinued, answerStateErr := e.scheduleAnswerContinued(turn, snapshots...)
	if answerStateErr != nil {
		return swarm.RunResult{}, answerStateErr
	}
	collectionContinued, collectionStateErr := e.workerCollectionContinued(turn, snapshots...)
	if collectionStateErr != nil {
		return swarm.RunResult{}, collectionStateErr
	}
	var res swarm.RunResult
	var runErr error
	pending, restoredAnswer, restoreFailure := e.restorePendingScheduleAnswer(turn)
	if restoreFailure != nil {
		return res, restoreFailure
	}
	if pending != nil && pending.State == "deferred" {
		messages, budget, err = rt.continueAfterDeferredAnswer(ctx, turn, reg, swarm.RunResult{}, messages, used, budget, paused != nil && proceed)
		if err != nil {
			return res, err
		}
		pending = nil
	}
	for {
		var modelRounds atomic.Int32
		answer := newManagerAnswerBuffer(acc)
		answer.historyScope = historyScopeFromTools(toolset)
		var candidate *pendingScheduleAnswer
		var row *store.PendingScheduleAnswer
		if restoredAnswer != nil {
			row, candidate = pending, restoredAnswer
			pending, restoredAnswer = nil, nil
			answer.id = candidate.AnswerID
			res = swarm.RunResult{Final: candidate.Final, Transcript: candidate.Transcript}
			reg.SetHistory(res.Transcript)
			if err := rt.restorePendingScheduleWorkers(ctx, reg); err != nil {
				return answer.reject(res), err
			}
			runErr = nil
			if candidate.MaxIterations {
				runErr = adk.ErrExceedMaxIterations
			}
		} else {
			restore, planted := rt.takeWorkerRestore()
			middlewares := append(e.autoCompactHandlers(rt.threadID, turn.ID, th, forceCompact), answer)
			answerCtx := context.WithValue(ctx, scheduleAnswerBufferContextKey{}, answer)
			res, runErr = reg.RunWith(answerCtx, swarm.RunConfig{
				Instruction: instruction, Messages: messages, ManagerTools: managerTools, MaxIterations: budget,
				RestoreWorkers: restore, FinishedWorkers: planted, ManagerMiddlewares: middlewares,
			}, swarm.Callback(func(n swarm.Notification) {
				if n.Kind == swarm.NotifyGenerationStarted && isManagerAgent(n.AgentID) {
					modelRounds.Add(1)
				}
				answer.onNotify(n)
			}))
		}
		forceCompact = false
		used += int(modelRounds.Load())
		if ctx.Err() != nil {
			return answer.reject(res), ctx.Err()
		}
		var failure *scheduleAnswerError
		var evidence []store.Schedule
		admitted := row != nil && row.State == "accepted"
		if !admitted && (runErr == nil || isMaxIterations(runErr)) {
			failure = answer.reportError()
			if failure == nil {
				if row == nil {
					res, failure = answer.prepareHistoryAnswer(ctx, turn, toolset, res, runErr)
					if failure == nil {
						reg.SetHistory(res.Transcript)
						row, failure = answer.prepare(turn, res, runErr)
					}
					if failure == nil {
						candidate = &pendingScheduleAnswer{}
						if err := json.Unmarshal([]byte(row.Payload), candidate); err != nil {
							failure = &scheduleAnswerError{Code: "answer_state_invalid", Cause: err}
						}
					}
				}
				if failure == nil {
					policyCtx, endPolicy := rt.scheduleAnswerContext(ctx, reg)
					failure = e.verifyPendingHistoryAnswer(policyCtx, turn, candidate)
					if failure == nil {
						failure = rt.admitPendingScheduleReports(policyCtx, turn)
					}
					if failure == nil {
						evidence, failure = rt.verifyPendingScheduleAnswer(policyCtx, turn, row, candidate)
					}
					endPolicy()
				}
			}
		} else if !admitted {
			res = answer.reject(res)
			reg.SetHistory(res.Transcript)
		}
		// A human Interrupt Injection cancels the epoch, not this ctx. The
		// generate can die with context.Canceled while the turn is still live;
		// stitch unread steering and keep going. Stop still hits ctx above.
		preempted := reg.TakePreempt()
		if !admitted && (preempted || failure != nil && failure.Code == "answer_superseded") && (runErr == nil || isPreemptRunError(runErr)) {
			if runErr != nil || reg.HasPendingSteers() {
				if err := rt.rejectPendingScheduleAnswer(turn, row); err != nil {
					return answer.reject(res), err
				}
				res = answer.reject(res)
				reg.SetHistory(res.Transcript)
				messages = stitchManagerToolResults(
					nextManagerMessages(res, messages, reg.TakePendingSteerMessages()),
					rt.engine.managerToolResults(turn.ID),
				)
				messages = dropTrailingIncompleteToolCalls(messages)
				continue
			}
		}
		if !admitted && failure != nil && failure.Code == "classification_deferred" {
			res = answer.reject(res)
			reg.SetHistory(res.Transcript)
			// A report can yield before this answer's own verification. That
			// unexamined answer must not regain execution priority on restart.
			if row != nil && row.State == "pending" {
				if err := rt.rejectPendingScheduleAnswer(turn, row); err != nil {
					return res, err
				}
			}
			messages, budget, err = rt.continueAfterDeferredAnswer(ctx, turn, reg, res, messages, used, budget, false)
			if err != nil {
				return res, err
			}
			continue
		}
		// Existing completion eligibility precedes answer admission. A failed
		// scheduled check is not an ordinary successful final with worker debt.
		if !admitted && runErr == nil && turn.ScheduleContinue && failure == nil {
			events, err := e.store.ListTurnEvents(turn.ID)
			if err != nil {
				res = answer.reject(res)
				reg.SetHistory(res.Transcript)
				return res, fmt.Errorf("read scheduled check evidence: %w", err)
			}
			if !scheduledOutputRecorded(events, res.Final) {
				if err := rt.rejectPendingScheduleAnswer(turn, row); err != nil {
					return answer.reject(res), err
				}
				res = answer.reject(res)
				reg.SetHistory(res.Transcript)
				if scheduleContinued {
					return res, errors.New("scheduled check ended without returning text, executing a tool or reporting its result")
				}
				scheduleContinued = true
				e.record(store.Event{ThreadID: turn.ThreadID, TurnID: turn.ID,
					Kind: KindModelRetry, AgentID: swarm.DefaultManagerID,
					Text: `{"attempt":1,"cap":1,"reason":"scheduled_no_activity"}`})
				messages = append(nextManagerMessages(res, messages, reg.TakePendingSteerMessages()),
					runtimeUserMessage(scheduleCompletionCue))
				continue
			}
		}
		if runErr == nil || isMaxIterations(runErr) {
			if failure == nil && !admitted {
				if ctx.Err() != nil {
					return answer.reject(res), ctx.Err()
				}
				// The policy call can outlive removal of the last goal/wake.
				// Recheck actual ownership and collection after it, before any
				// candidate prose is visible as a successful completion.
				if got, err := e.store.GetThread(rt.threadID); err == nil {
					th = got
				}
				if runErr == nil {
					if workers := rt.uncollectedWorkers(reg); len(workers) > 0 {
						ids := make([]string, 0, len(workers))
						for _, worker := range workers {
							ids = append(ids, worker.AgentID)
						}
						if err := rt.rejectPendingScheduleAnswer(turn, row); err != nil {
							return answer.reject(res), err
						}
						res = answer.reject(res)
						reg.SetHistory(res.Transcript)
						if collectionContinued {
							extend := e.cfg.SwarmSnapshot().ManagerIterations()
							pause, err := rt.newWorkerCollectionPause(turn, workers, used, extend)
							if err != nil {
								return res, err
							}
							proceed, _, err := rt.waitForWorkerCollection(ctx, turn, reg, pause)
							if err != nil {
								return res, err
							}
							if !proceed {
								if ctx.Err() != nil {
									return res, ctx.Err()
								}
								return res, limitStopError{Used: used, Reason: "workers_uncollected"}
							}
							budget = extend
							// Both automatic and human decisions have already consumed
							// their durable allowance before returning from the wait.
							messages = append(nextManagerMessages(res, messages, reg.TakePendingSteerMessages()), runtimeUserMessage(workerCollectionStateCue(reg.PendingAgents())))
							continue
						}
						if err := rt.recordWorkerCollectionRetry(turn, ids); err != nil {
							return res, err
						}
						collectionContinued = true
						messages = append(nextManagerMessages(res, messages, reg.TakePendingSteerMessages()), runtimeUserMessage(workerCollectionStateCue(reg.PendingAgents())))
						continue
					}
				}
				var history *store.HistoryEvidence
				if candidate.HistoryReport != nil {
					history = &candidate.HistoryReport.Evidence
				}
				failure = e.admitScheduleAnswerWithHistory(turn.ThreadID, turn.ID, evidence, candidate.Events, history, row.ID)
			}
			if failure != nil {
				res = answer.reject(res)
				reg.SetHistory(res.Transcript)
				if ctx.Err() != nil {
					return res, ctx.Err()
				}
				if err := rt.rejectPendingScheduleAnswer(turn, row); err != nil {
					return res, err
				}
				// A steer can arrive after the earlier epoch check, immediately
				// before atomic admission. It supersedes this candidate without
				// spending the allowance for a false scheduling assertion.
				if failure.Code == "answer_superseded" {
					messages = stitchManagerToolResults(
						nextManagerMessages(res, messages, reg.TakePendingSteerMessages()),
						e.managerToolResults(turn.ID),
					)
					messages = dropTrailingIncompleteToolCalls(messages)
					continue
				}
				// Infrastructure/storage refusal is not a finding about the
				// manager's claim and cannot authorize repeating its tools.
				if failure.Code != "no_matching_schedule" && failure.Code != "schedule_state_changed" && !isHistoryAnswerCorrection(failure) {
					return res, failure
				}
				if answerContinued {
					return res, failure
				}
				answerContinued = true
				if err := rt.recordScheduleAnswerRetry(turn, failure); err != nil {
					return res, err
				}
				var cue string
				if isHistoryAnswerCorrection(failure) {
					cue = historyAnswerCorrectionCue
				} else {
					cue = failure.correctionCue()
				}
				messages = append(nextManagerMessages(res, messages, reg.TakePendingSteerMessages()), runtimeUserMessage(cue))
				continue
			}
			res = answer.accept(res)
			reg.SetHistory(res.Transcript)
		}
		if !isMaxIterations(runErr) {
			if runErr != nil && th != nil && isContextOverflow(runErr) && overflowRetries < 1 {
				// A ceiling that does not move is still a prompt that did not
				// fit: the request asked for max_tokens on top of the input.
				// Fold once and retry. Sending the same messages again is how
				// the 400 repeats on the next scheduled check.
				_ = e.learnContextCeiling(th.ProviderID, th.Model, runErr, promptTokenCount(messages))
				overflowRetries++
				forceCompact = true
				e.record(store.Event{
					ThreadID: turn.ThreadID, TurnID: turn.ID,
					Kind: KindModelRetry, AgentID: swarm.DefaultManagerID,
					Text: `{"attempt":1,"cap":1,"reason":"` + modelRetryReasonContextOverflow + `"}`,
				})
				continue
			}
			if runErr != nil && isRetryableModelError(runErr) && modelRetries < modelErrorRetries {
				modelRetries++
				rt.recordModelRetry(turn, modelRetries, modelFailureClass(runErr))
				messages = rt.retryManagerRun(turn, res, messages, reg)
				continue
			}
			return res, runErr
		}
		if got, err := e.store.GetThread(rt.threadID); err == nil {
			th = got
		}
		if pursuingGoal(th) && !pausedStandingGoal(th) {
			// eino needs a finite ReAct slice. That is not a /goal
			// session boundary: keep this turn, keep the registry, do
			// not spend goal_max_auto_turns.
			messages = stitchManagerToolResults(
				nextManagerMessages(res, messages, reg.TakePendingSteerMessages()),
				rt.engine.managerToolResults(turn.ID),
			)
			budget = e.cfg.SwarmSnapshot().GoalSessionIterations()
			continue
		}
		if closedStandingGoal(th) {
			// complete_goal / block_goal landed mid-slice. Asking to
			// extend would pop the non-goal confirm card on a turn
			// that already stopped pursuing.
			return res, nil
		}
		extend := e.cfg.SwarmSnapshot().ManagerIterations()
		if !rt.waitToExtend(ctx, turn, used, extend) {
			if ctx.Err() != nil {
				return res, runErr
			}
			return res, limitStopError{Used: used}
		}
		messages = nextManagerMessages(res, messages, reg.TakePendingSteerMessages())
		budget = extend
	}
}

// waitToExtend pauses the turn at the tool-round cap until the human
// extends it, declines, or interrupts. The channel is installed before the
// event is recorded so a client that reacts to the event cannot land on
// ContinueTurn before anyone is listening.
func (rt *runtime) waitToExtend(ctx context.Context, turn *store.Turn, used, extend int, steered ...*swarm.Registry) bool {
	ch := make(chan bool, 1)
	rt.mu.Lock()
	rt.continueCh = ch
	rt.acceptedContinue = nil
	// Steering holds the same runtime lock. Check after installing the
	// waiter so guidance queued as policy verification ends cannot be lost.
	if len(steered) > 0 && steered[0].HasPendingSteers() {
		rt.signalContinueLocked(true)
	}
	rt.mu.Unlock()
	defer func() {
		rt.mu.Lock()
		rt.continueCh = nil
		rt.mu.Unlock()
	}()

	// Two ints: fmt keeps this branch-free. encoding/json cannot fail here
	// and a failure would stall the turn with no event for the UI to answer.
	body := fmt.Sprintf(`{"limit":%d,"extend_by":%d,"reason":"iterations"}`, used, extend)
	rt.engine.record(store.Event{
		ThreadID: rt.threadID, TurnID: turn.ID,
		Kind: KindMaxIterations, AgentID: swarm.DefaultManagerID,
		Text: body,
	})

	select {
	case proceed := <-ch:
		if proceed {
			rt.engine.record(store.Event{
				ThreadID: rt.threadID, TurnID: turn.ID,
				Kind: KindMaxIterationsContinued, AgentID: swarm.DefaultManagerID,
				Text: fmt.Sprintf("continuing for another %d tool rounds", extend),
			})
		}
		return proceed
	case <-ctx.Done():
		return false
	}
}

// ContinueTurn answers a pause at the manager's tool-round cap. proceed
// extends the current turn by another configured slice; false ends it.
func (e *Engine) ContinueTurn(threadID string, proceed bool) error {
	done, err := e.beginAdmission()
	if err != nil {
		return err
	}
	defer done()
	if _, err := e.store.GetThread(threadID); err != nil {
		return err
	}
	e.mu.Lock()
	rt := e.runtimes[threadID]
	e.mu.Unlock()
	if rt == nil {
		return ErrIdle
	}
	if !rt.status().Running {
		return ErrIdle
	}
	rt.mu.Lock()
	if pause := rt.collectionPause; pause != nil {
		if rt.continueCh == nil || rt.acceptedContinue != nil || rt.userStopped || rt.abandoned {
			rt.mu.Unlock()
			return ErrIdle
		}
		turn := &store.Turn{ThreadID: rt.threadID, ID: rt.turnID}
		var decisionErr error
		if proceed {
			decisionErr = rt.saveWorkerCollectionContinue(turn, pause)
		} else {
			decisionErr = rt.saveWorkerPauseEvent(turn, KindWorkersCollectionDeclined, workerCollectionReceipt{pause.WaitID, pause.WorkerIDs, pause.Workers})
		}
		if decisionErr != nil {
			pause.failure, pause.AutomaticResume = decisionErr, false
			rt.signalContinueLocked(false)
			rt.mu.Unlock()
			return decisionErr
		}
		pause.Decision = &proceed
		accepted := rt.signalContinueLocked(proceed)
		rt.mu.Unlock()
		if !accepted {
			return ErrIdle
		}
		return nil
	}
	rt.mu.Unlock()
	if !rt.signalContinue(proceed) {
		return ErrIdle
	}
	return nil
}

func (rt *runtime) signalContinue(proceed bool) bool {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	return rt.signalContinueLocked(proceed)
}

func (rt *runtime) signalContinueLocked(proceed bool) bool {
	ch := rt.continueCh
	if ch == nil {
		return false
	}
	select {
	case ch <- proceed:
		rt.acceptedContinue = &proceed
		return true
	default:
		return false
	}
}

// nextManagerMessages feeds the previous transcript back into the next
// ReAct slice. Instruction regenerates the system prompt; unread steers
// land at the end so they are the last thing the manager reads.
func nextManagerMessages(res swarm.RunResult, fallback []adk.Message, extra []*schema.Message) []adk.Message {
	next := messagesWithoutSystem(res.Transcript)
	if len(next) == 0 {
		next = fallback
	}
	if len(extra) == 0 {
		return next
	}
	out := make([]adk.Message, len(next), len(next)+len(extra))
	copy(out, next)
	for _, m := range extra {
		out = append(out, m)
	}
	return out
}

// stitchManagerToolResults puts manager tool_result rows back next to the
// assistant call that produced them. eino's iteration cap is a ChatModel
// preprocessor failure: AfterModel can snapshot the call, then SetHistory
// on the capped next step wipes the wrap-appended results. Without this a
// /goal slice re-reads a stale wait_agents report forever.
func stitchManagerToolResults(msgs []adk.Message, results []store.Event) []adk.Message {
	if len(results) == 0 {
		return msgs
	}
	byID := make(map[string]store.Event, len(results))
	order := make([]string, 0, len(results))
	for _, ev := range results {
		id := strings.TrimSpace(ev.ToolCallID)
		if id == "" {
			continue
		}
		if _, ok := byID[id]; !ok {
			order = append(order, id)
		}
		byID[id] = ev
	}
	if len(byID) == 0 {
		return msgs
	}
	placed := make(map[string]bool, len(byID))
	out := make([]adk.Message, 0, len(msgs)+len(byID))
	for _, m := range msgs {
		if m != nil && m.Role == schema.Tool {
			id := strings.TrimSpace(m.ToolCallID)
			if ev, ok := byID[id]; ok {
				if placed[id] {
					continue
				}
				out = append(out, schema.ToolMessage(ev.Text, id))
				placed[id] = true
				continue
			}
		}
		out = append(out, m)
		if m == nil || m.Role != schema.Assistant || len(m.ToolCalls) == 0 {
			continue
		}
		for _, tc := range m.ToolCalls {
			id := strings.TrimSpace(tc.ID)
			if id == "" || placed[id] {
				continue
			}
			ev, ok := byID[id]
			if !ok {
				continue
			}
			out = append(out, schema.ToolMessage(ev.Text, id))
			placed[id] = true
		}
	}
	for _, id := range order {
		if placed[id] {
			continue
		}
		out = append(out, schema.ToolMessage(byID[id].Text, id))
		placed[id] = true
	}
	return out
}

func (e *Engine) managerToolResults(turnID string) []store.Event {
	if e == nil || turnID == "" {
		return nil
	}
	events, err := e.store.ListTurnEvents(turnID)
	if err != nil {
		return nil
	}
	var out []store.Event
	for _, ev := range events {
		if ev.Kind != swarm.NotifyToolResult.String() {
			continue
		}
		if ev.AgentID != "" && ev.AgentID != swarm.DefaultManagerID {
			continue
		}
		out = append(out, ev)
	}
	return out
}

// messagesWithoutSystem drops regenerated prompt instructions while retaining
// the latest runtime roster. Losing that system state after a ReAct slice
// discards the only remaining worker identities after compaction.
func messagesWithoutSystem(msgs []adk.Message) []adk.Message {
	out := make([]adk.Message, 0, len(msgs))
	var roster *schema.Message
	for _, m := range msgs {
		if _, ok := workerstate.Parse(m); ok {
			roster = m
			continue
		}
		if m == nil || m.Role == schema.System {
			continue
		}
		out = append(out, m)
	}
	if roster != nil {
		return append([]adk.Message{roster}, out...)
	}
	return out
}
