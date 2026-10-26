import type { ImageRef, ReviewOutcome, SwarmEvent } from "./types"
import { applyGenerationEvent, applyInterruptedOutput, recoverLegacyGeneration, type GenerationInterruption } from "./transcript-generation"
import { cloudToolSummary, parseCloudTool } from "./cloud-tool"
import {
  compactBriefing,
  compactNotice,
  goalCappedNotice,
  goalIdleNotice,
  goalNotice,
  goalSessionNotice,
  isGoalSessionWrapSteer,
  modelRetryNotice,
} from "./transcript-notices"
import { parseReview, reviewNotice } from "./transcript-review"
import { modelBackoffNotice } from "./transcript-backoff"
import { recordRecovery, type RecoveryEvent } from "./transcript-recovery"
import { askCardFromEvent, askRecoveryCard, applyAskRecovery, settleAskCard, type AskCard } from "./transcript-ask"
import { isPlanEvent, planNotice } from "./transcript-plan"
import { parseIterationLimit, parsePulse } from "./transcript-pulse"
import {
  dropRetractedSteer,
  isRetractedSteer,
  parseSteerRetractSeq,
  parseSteerRevision,
  rememberSteerRevision,
  revisedSteerText,
  steerRevisionsBefore,
} from "./transcript-steer"
import { applyScheduleEvent, sealQuietTurns } from "./transcript-schedule"
import { findToolBlock, splitToolCall, summarise } from "./transcript-tool-block"
import { closePendingTools, parseWorkerRetention, settleFailedWorkers, type WorkerRetention } from "./transcript-worker-ownership"
import {
  MANAGER_ID,
  append,
  block,
  closeStreaming,
  replaceComplete,
  replaceStreaming,
} from "./transcript-blocks"

export { MANAGER_ID }

export { splitToolCall, summarise }
export { toolNameOf } from "./transcript-tool-block"

export { parseIterationLimit, parsePulse } from "./transcript-pulse"
export { splitQueuedSteers } from "./transcript-steer"

export {
  compactBriefing,
  compactNotice,
  goalCappedNotice,
  goalIdleNotice,
  goalNotice,
  goalSessionNotice,
  modelRetryNotice,
}
export { parseReview, reviewNotice, reviewPanelHint } from "./transcript-review"

/** A transcript is a list of blocks per agent. The event stream is flat and
 *  interleaved across agents, so the reducer's whole job is to fold it into
 *  something renderable, with streamed text replacing rather than appending —
 *  the server sends the accumulated string, not the increment. */

export type BlockKind =
  | "user"
  | "steer"
  | "reasoning"
  | "answer"
  | "tool"
  | "question"
  | "spawn"
  | "notice"
  | "confirm"
  | "error"
  | "title"
  | "session_memory"

export interface Block {
  id: string
  kind: BlockKind
  agentId: string
  role?: string
  text: string
  /** Source goal event for chat projection; the original receipt stays in Trace. */
  goalNoticeKind?: "goal_resumed" | "goal_continued"
  /** Shared service cooldown identity, independent of the waiting role/turn. */
  cooldownId?: string
  /** One model output can span several manager turns and durable checkpoints. */
  messageId?: string
  /** Still being streamed. Complete blocks stop showing a cursor and can be
   *  collapsed. */
  streaming?: boolean
  /** A review the user asked not to see in the transcript. It still exists
   *  so the Trace tab's Full log and resume point do not lose the event. */
  quiet?: boolean
  /** Extra body a notice can open. Compact keeps the briefing here so the
   *  chat row stays a one-liner instead of pasting the next prompt. */
  detail?: string
  /** Structured reviewer diagnostics, formatted when the UI language changes. */
  review?: ReviewOutcome
  /** Provider-executed tool metadata; never a local function call. */
  cloudTool?: Record<string, unknown>
  /** One interrupted output, independent of the worker lifecycle. */
  generation?: GenerationInterruption
  /** Tool blocks only. */
  tool?: {
    callId: string
    name: string
    args: string
    result?: string
    failed?: boolean
    /** A call with no result yet is still running. */
    pending: boolean
    /** Argument runes written so far. Set while the model is still
     *  composing the call, before it is issued. */
    writing?: number
  }
  /** ask_user cards. */
  question?: AskCard
  askRecovery?: SwarmEvent
  /** Spawn blocks only: which sub-agent was started. */
  spawn?: { agentId: string; role: string }
  /** Confirm blocks only: the manager hit its tool-round cap. */
  confirm?: {
    limit: number
    extendBy: number
    pending: boolean
    continued?: boolean
  }
  turnId: string
  seq: number
  at: string
  /** Pasted vision inputs on user / steer blocks. */
  images?: ImageRef[]
}

export type AgentStatus = "running" | "done" | "failed" | "cancelled"

export interface AgentState {
  id: string
  role: string
  status: AgentStatus
  /** The last thing this agent did, for the one-line summary in the roster. */
  activity: string
  blocks: Block[]
  /** The system prompt this worker was started with. Absent on older events
   *  that only stored the role name. */
  instruction?: string
  startedAt?: string
  endedAt?: string
  result?: string
  error?: string
  /** A parent turn can fail while this worker is still running. Preserve
   *  that cause without presenting it as the worker's own failure. */
  errorSource?: "agent" | "turn"
}

export interface TurnState {
  id: string
  userText: string
  status: "running" | "done" | "error" | "cancelled"
  startedAt?: string
  endedAt?: string
  final?: string
  error?: string
  /** A primary terminal event loaded into the main transcript, not a roster cursor. */
  completedSeq?: number
  /** Sub-agents spawned during this turn, in the order they appeared. */
  agentIds: string[]
  /** True when this turn is a /goal work session (auto-continue or a forced yield). */
  session?: boolean
  /** quiet: omitted scheduled check. scheduledFindings: keep the chip on empty done. */
  quiet?: boolean
  scheduledFindings?: boolean
  /** Durable proof that named workers survived this manager check's failure. */
  workerRetention?: WorkerRetention
}

/** The public error of the newest failed turn, if any. The banner used to
 *  store only "the last turn failed"; older conversations still do. */
export function lastFailedTurnError(turns: TurnState[]): string {
  for (let i = turns.length - 1; i >= 0; i--) {
    const err = turns[i]?.error?.trim()
    if (turns[i]?.status === "error" && err) return err
  }
  return ""
}

/** One sub-agent as the server last saw it. Ages are measured on the server so
 *  a client's clock, or a tab the browser had throttled, cannot invent them.
 *  There is no activity text: what an agent is doing is already on screen from
 *  its own events, and a silence is missing proof rather than words. */
export interface PulseAgent {
  agentId: string
  role?: string
  status: AgentStatus
  elapsedMs: number
}

/** The latest progress pulse. While every agent sits inside a long tool call
 *  this is the only thing that moves, and its absence is how a genuinely stuck
 *  run tells itself apart from a busy one.
 *
 *  It says how long, not how many: a count taken a pulse ago would contradict
 *  the agent rows on screen, which are updated by events as they happen. */
export interface Pulse {
  at: string
  elapsedMs: number
  agents: PulseAgent[]
}

export interface TranscriptState {
  /** Manager first, then sub-agents in spawn order. */
  agentOrder: string[]
  agents: Record<string, AgentState>
  turns: TurnState[]
  /** Highest stored sequence number seen, which is where a reconnect resumes
   *  from. Deltas carry 0 and never move it. */
  lastSeq: number
  running: boolean
  pulse?: Pulse
  /** Retracted steer seqs, quiet scheduled turns, and fired scheduled turns.
   *  Arrays are cloned on each reduce so later events cannot mutate history. */
  retractedSteers?: number[]
  /** Latest caption for an unread steer, keyed by that steer's event seq. */
  steerRevisions?: Record<number, string>
  quietTurns?: string[]
  scheduledFiredTurns?: string[]
  /** Stored seqs in `[from, through]` belong to a resent message. Live
   *  deltas stay dropped until the replacement `user_message` arrives. */
  rewindCut?: { from: number; through: number; live: boolean }
  recoveryEvents?: RecoveryEvent[]
}

export function emptyTranscript(): TranscriptState {
  return { agentOrder: [], agents: {}, turns: [], lastSeq: 0, running: false }
}

/** Fold one event. `roster` still creates workers and applies finished/cleanup
 *  but does not insert "Started" / cleanup rows into the manager transcript. */
export type ReduceMode = "full" | "roster"

export function reduceEvent(
  state: TranscriptState,
  ev: SwarmEvent,
  mode: ReduceMode = "full",
): TranscriptState {
  // Array.reduce would pass the index as the third argument; only explicit "roster" skips chrome.
  const rosterOnly = mode === "roster"
  if (ev.kind === "rewound") {
    const from = Number.parseInt(String(ev.text ?? ""), 10)
    if (!Number.isFinite(from) || from <= 0) return state
    return rewindTranscript(state, from)
  }
  if (droppedByRewind(state, ev)) return state
  const next: TranscriptState = {
    agentOrder: state.agentOrder,
    agents: { ...state.agents },
    turns: state.turns,
    lastSeq: ev.seq > state.lastSeq ? ev.seq : state.lastSeq,
    running: state.running,
    pulse: state.pulse,
    retractedSteers: state.retractedSteers,
    steerRevisions: state.steerRevisions,
    quietTurns: state.quietTurns?.slice(),
    scheduledFiredTurns: state.scheduledFiredTurns?.slice(),
    rewindCut: state.rewindCut,
    recoveryEvents: state.recoveryEvents,
  }
  if (recordRecovery(next, ev)) return next
  if (applyScheduleEvent(next, ev)) return sealQuietTurns(next, ev)
  if (ev.kind === "workers_retained") {
    const retained = parseWorkerRetention(ev)
    const turn = next.turns.find((item) => item.id === ev.turn_id)
    if (retained && (!turn || turn.status === "running")) upsertTurn(next, ev.turn_id, { workerRetention: retained })
    return next
  }
  if (ev.kind === "generation_started" || ev.kind === "generation_completed") return next
  // Recovery state is trace evidence, not a chat message or a new agent.
  if (ev.kind === "engine_restart_checkpoint") return next
  if (ev.kind === "ask_recovered") {
    const card = askRecoveryCard(ev)
    if (card) applyAskRecovery(touchAgent(next, MANAGER_ID), ev, card)
    return next
  }
  if (ev.kind === "restart_preparing" || ev.kind === "restart_ready" || ev.kind === "user_stop_requested") {
    if (ev.text) append(touchAgent(next, MANAGER_ID), block(ev, "notice", ev.text))
    return next
  }
  if (ev.kind === "steer_preempted") {
    return next
  }
  if (ev.kind === "steer_retracted") {
    dropRetractedSteer(next, parseSteerRetractSeq(ev.text))
    return next
  }
  if (ev.kind === "steer_revised") {
    const rev = parseSteerRevision(ev.text)
    if (rev) rememberSteerRevision(next, rev.seq, rev.text)
    return next
  }
  // The memory review runs after the turn, on its own. It is not a member of
  // the swarm, so it must not join the roster as a worker with nothing to
  // show; and when it kept nothing — the common case — it says nothing.
  if (ev.kind === "memory_review") {
    const outcome = parseReview(ev)
    const notice = reviewNotice(outcome)
    if (notice) {
      const row = block(ev, "notice", notice)
      row.quiet = outcome?.notify === "off"
      row.review = outcome
      append(touchAgent(next, MANAGER_ID), row)
    }
    return next
  }
  if (ev.kind === "builtin_tool") {
    const metadata = parseCloudTool(ev.text)
    const row = block(ev, "notice", cloudToolSummary(metadata))
    row.cloudTool = metadata
    row.detail = ev.text
    append(touchAgent(next, MANAGER_ID), row)
    return sealQuietTurns(next, ev)
  }
  if (ev.kind === "task_message") {
    const manager = touchAgent(next, MANAGER_ID)
    const notice = block(ev, "notice", `Peer task ${ev.source_thread_id ?? ""}: ${ev.text ?? ""}`)
    if (!manager.blocks.some((b) => b.id === notice.id)) append(manager, notice)
    return next
  }
  // A generated title is metadata for the sidebar, not a chat row. It still
  // lives on the manager as a quiet block so the Trace tab's Full log can show it
  // without inventing a "title-namer" worker in the roster.
  if (ev.kind === "title") {
    const row = block(ev, "title", ev.text || ev.err || "")
    row.quiet = true
    append(touchAgent(next, MANAGER_ID), row)
    return next
  }
  // Rolling session briefing: compact and the reviewer read it. The chat
  // does not — inventing a "session-memory" worker would be the same bug
  // as a title-namer row.
  if (ev.kind === "session_memory") {
    const row = block(ev, "session_memory", ev.text || ev.err || "")
    row.quiet = true
    append(touchAgent(next, MANAGER_ID), row)
    return next
  }
  if (ev.kind === "resumed" || ev.kind === "restart_resumed") {
    next.running = true
    upsertTurn(next, ev.turn_id, { status: "running" })
    const manager = touchAgent(next, MANAGER_ID)
    if (ev.text) append(manager, block(ev, "notice", ev.text))
    // Crash/quit is not "decline the cap". The turn is running again, so a
    // leftover confirm would look like it is still waiting for a click.
    settlePendingConfirm(manager, true)
    // The process that issued these calls is gone. Leaving them pending
    // keeps the spinner next to a row that is no longer doing anything.
    // Keep leftover workers running: they are restarted under the same id.
    for (const id of next.agentOrder) {
      const agent = touchAgent(next, id)
      recoverLegacyGeneration(agent, ev)
      closePendingTools(agent, resumeToolStopped, true)
    }
    return next
  }
  // /goal and /compact are conversation metadata. They must not mint a
  // compact-summarizer (or similar) worker in the roster.
  if (ev.kind === "goal") {
    append(touchAgent(next, MANAGER_ID), block(ev, "notice", goalNotice(ev.text)))
    return next
  }
  if (ev.kind === "goal_complete") {
    append(touchAgent(next, MANAGER_ID), block(ev, "notice", "Standing objective completed."))
    return next
  }
  if (ev.kind === "goal_continued") {
    next.running = true
    upsertTurn(next, ev.turn_id, { status: "running", startedAt: ev.created_at, session: true })
    // Parked workers keep their ids and their in-flight tools. Treating this
    // like a human turn used to paint them done, so wait_agents sat next to a
    // "Done" roster until the next session timed out.
    const manager = touchAgent(next, MANAGER_ID)
    manager.status = "running"
    append(manager, { ...block(ev, "notice", ev.text?.trim() || "Continuing the standing objective."), goalNoticeKind: ev.kind })
    return next
  }
  if (ev.kind === "goal_capped") {
    append(touchAgent(next, MANAGER_ID), block(ev, "notice", goalCappedNotice(ev.text)))
    return next
  }
  if (ev.kind === "goal_idle") {
    append(touchAgent(next, MANAGER_ID), block(ev, "notice", goalIdleNotice(ev.text)))
    return next
  }
  if (ev.kind === "goal_blocked") {
    append(touchAgent(next, MANAGER_ID), block(ev, "notice", "Standing objective blocked: progress needs you or an external change."))
    return next
  }
  if (ev.kind === "model_retry") {
    append(touchAgent(next, MANAGER_ID), block(ev, "notice", modelRetryNotice(ev.text)))
    return next
  }
  if (ev.kind === "model_backoff") {
    const notice = modelBackoffNotice(ev.text)
    const row = notice && { ...block(ev, "notice", notice.text), cooldownId: notice.cooldownId }
    if (row && !next.agents[MANAGER_ID]?.blocks.some((old) => old.id === row.id || row.cooldownId && old.cooldownId === row.cooldownId)) {
      append(touchAgent(next, MANAGER_ID), row)
    }
    return sealQuietTurns(next, ev)
  }
  if (ev.kind === "goal_edited") {
    append(touchAgent(next, MANAGER_ID), block(ev, "notice", "Standing objective updated."))
    return next
  }
  if (ev.kind === "goal_resumed") {
    next.running = true
    upsertTurn(next, ev.turn_id, { status: "running", startedAt: ev.created_at })
    const manager = touchAgent(next, MANAGER_ID)
    manager.status = "running"
    append(manager, { ...block(ev, "notice", ev.text?.trim() || "Resuming the standing objective."), goalNoticeKind: ev.kind })
    return next
  }
  if (ev.kind === "goal_session") {
    upsertTurn(next, ev.turn_id, { session: true })
    append(touchAgent(next, MANAGER_ID), block(ev, "notice", goalSessionNotice(ev.text)))
    return next
  }
  if (ev.kind === "compacted") {
    const row = block(ev, "notice", compactNotice(ev))
    const briefing = compactBriefing(ev)
    if (briefing) row.detail = briefing
    append(touchAgent(next, MANAGER_ID), row)
    return next
  }
  if (isPlanEvent(ev.kind)) {
    const text = planNotice(ev.kind, ev.text) ?? ""
    if (ev.kind === "plan_implemented") {
      next.running = true
      upsertTurn(next, ev.turn_id, { status: "running", startedAt: ev.created_at })
      const manager = touchAgent(next, MANAGER_ID)
      manager.status = "running"
      append(manager, block(ev, "notice", text))
      return next
    }
    append(touchAgent(next, MANAGER_ID), block(ev, "notice", text))
    return next
  }

  const agentId = ev.agent_id || MANAGER_ID
  const agent = touchAgent(next, agentId, ev.role)
  if (applyInterruptedOutput(agent, ev)) return next

  switch (ev.kind) {
    case "generation_interrupted":
    case "generation_recovered":
      applyGenerationEvent(agent, ev)
      break

    case "user_message":
      if (next.rewindCut?.live && ev.seq > next.rewindCut.through) {
        next.rewindCut = { ...next.rewindCut, live: false }
      }
      upsertTurn(next, ev.turn_id, { userText: ev.text ?? "", status: "running", startedAt: ev.created_at })
      next.running = true
      next.pulse = undefined
      agent.blocks = agent.blocks.filter((b) => b.id !== PENDING_EDIT_ID)
      append(agent, block(ev, "user", ev.text ?? ""))
      break

    case "steer":
      if (!isGoalSessionWrapSteer(ev.text) && !isRetractedSteer(next, ev.seq)) {
        append(agent, block(ev, "steer", revisedSteerText(next, ev.seq, ev.text ?? "")))
      }
      break

    case "reasoning_delta":
      // Deltas carry the whole accumulated string, so the open block is
      // replaced. Appending here is the classic bug that doubles every word.
      if (!keepWorkerLive(agent)) break
      replaceStreaming(agent, ev, "reasoning")
      agent.activity = "thinking"
      break

    case "delta":
      // The model has started answering, so any thinking block is done.
      if (!keepWorkerLive(agent)) break
      closeStreaming(agent, "reasoning")
      replaceStreaming(agent, ev, "answer")
      agent.activity = "writing"
      break

    case "reasoning":
      // The complete text replaces whatever was streamed. Closing the block
      // first would hide it from the lookup and leave the thought on screen
      // twice.
      replaceComplete(agent, ev, "reasoning")
      break

    case "agent_message":
      if (!keepWorkerLive(agent)) break
      closeStreaming(agent, "reasoning")
      replaceComplete(agent, ev, "answer")
      agent.activity = summarise(ev.text ?? "")
      break

    case "tool_call":
    case "tool_call_delta": {
      if (!keepWorkerLive(agent)) break
      closeStreaming(agent, "reasoning")
      // tool_call_delta arrives while the commentary is still streaming.
      // Closing the answer here seals that paragraph, and the agent_message
      // flushed when the call is issued can no longer find it, so the same
      // text is painted again under the tools.
      if (ev.kind !== "tool_call_delta") closeStreaming(agent, "answer")
      const { name, args } = splitToolCall(ev.text ?? "")
      const writing = ev.kind === "tool_call_delta" ? Number(args) : undefined
      const ask = ev.kind === "tool_call" ? askCardFromEvent(ev, name, args) : undefined
      const existing = ev.tool_call_id ? findToolBlock(agent, ev.tool_call_id, ask ? ev.turn_id : undefined) : undefined
      if (existing?.tool && !ask) {
        existing.tool = {
          ...existing.tool,
          name: name || existing.tool.name,
          args: ev.kind === "tool_call" ? args : existing.tool.args,
          pending: true,
          writing: Number.isFinite(writing) ? writing : undefined,
        }
        existing.text = name || existing.text
        agent.activity = name || agent.activity
        break
      }
      if (existing && ask) {
        existing.kind = "question"
        existing.tool = undefined
        existing.question ??= ask
        existing.text = name
        break
      }
      if (ask) {
        append(agent, { ...block(ev, "question", name), question: ask })
      } else if (name) {
        append(agent, {
          ...block(ev, "tool", name),
          tool: {
            callId: ev.tool_call_id ?? "",
            name,
            args: ev.kind === "tool_call" ? args : "",
            pending: true,
            writing: Number.isFinite(writing) ? writing : undefined,
          },
        })
      }
      if (name) agent.activity = name
      break
    }

    case "tool_delta": {
      // Live tool output is the accumulated snapshot, same as a text delta.
      // The call is still running: a later tool_result is what closes it.
      const target = findToolBlock(agent, ev.tool_call_id)
      if (target?.tool) {
        target.tool = {
          ...target.tool,
          result: ev.text ?? "",
        }
      }
      break
    }

    case "tool_result": {
      const target = findToolBlock(agent, ev.tool_call_id, ev.turn_id)
      if (target?.question) {
        target.question = settleAskCard(target.question, ev)
      } else if (target?.tool) {
        target.tool = {
          ...target.tool,
          result: ev.text ?? ev.err ?? "",
          failed: Boolean(ev.err),
          pending: false,
        }
      } else {
        append(agent, {
          ...block(ev, "tool", "result"),
          tool: {
            callId: ev.tool_call_id ?? "",
            name: "result",
            args: "",
            result: ev.text ?? "",
            pending: false,
          },
        })
      }
      break
    }

    case "spawned": {
      const existed = Boolean(next.agents[ev.agent_id])
      const child = touchAgent(next, ev.agent_id, ev.role ?? ev.text)
      if (existed) recoverLegacyGeneration(child, ev)
      const instruction = recordedInstruction(ev.role, ev.text)
      if (instruction) child.instruction = instruction
      const manager = touchAgent(next, MANAGER_ID)
      const painted = manager.blocks.some((b) => b.kind === "spawn" && b.spawn?.agentId === child.id)
      child.status = "running"
      child.endedAt = undefined
      child.error = undefined
      child.errorSource = undefined
      child.activity = existed || painted ? "continuing" : "starting"
      if (!existed) child.startedAt = ev.created_at
      if (!rosterOnly) {
        addAgentToTurn(next, ev.turn_id, child.id)
        // A resume re-emits spawned for the same id so the roster comes back
        // to life. A second "Started" row is the bug that looks like two
        // workers with the same name.
        if (!painted) {
          append(manager, {
            ...block(ev, "spawn", instruction ?? ev.role ?? ev.text ?? "sub-agent"),
            agentId: MANAGER_ID,
            spawn: { agentId: child.id, role: ev.role ?? ev.text ?? "sub-agent" },
          })
        }
      }
      break
    }

    case "finished": {
      const child = touchAgent(next, ev.agent_id, ev.role)
      // Stop wins over buffered receipts until an explicit spawn resumes it.
      if (child.status === "cancelled") break
      // Match the current worker's engine envelope, not provider prose.
      const cancelledPrefix = `swarm: agent ${ev.agent_id} cancelled: `
      const cancelled = Boolean(ev.err?.startsWith(cancelledPrefix) && ev.err.slice(cancelledPrefix.length).trim())
      child.status = cancelled ? "cancelled" : ev.err ? "failed" : "done"
      child.endedAt = ev.created_at
      child.result = ev.text
      child.error = cancelled ? undefined : ev.err
      child.errorSource = child.error ? "agent" : undefined
      child.activity = cancelled ? "stopped" : ev.err ? "failed" : "done"
      closeStreaming(child, "reasoning")
      closeStreaming(child, "answer")
      closePendingTools(child, ev.err)
      break
    }

    case "cleanup":
      if (!rosterOnly) {
        append(touchAgent(next, MANAGER_ID), block(ev, "notice", ev.text ?? ""))
      }
      // Cleanup is the kill signal. Manager `done` is not: a /goal session
      // parks workers and they keep running under the same ids.
      for (const id of next.agentOrder) {
        if (id === MANAGER_ID) continue
        const child = next.agents[id]
        if (!child || child.status !== "running") continue
        closeStreaming(child, "reasoning")
        closeStreaming(child, "answer")
        closePendingTools(child, ev.text)
        child.status = "cancelled"
        child.activity = "stopped"
        child.endedAt = ev.created_at
      }
      break

    case "max_iterations": {
      const payload = parseIterationLimit(ev)
      append(touchAgent(next, MANAGER_ID), {
        ...block(ev, "confirm", ev.text ?? ""),
        confirm: {
          limit: payload?.limit ?? 0,
          extendBy: payload?.extendBy ?? 0,
          pending: true,
        },
      })
      break
    }

    case "max_iterations_continued":
      settlePendingConfirm(touchAgent(next, MANAGER_ID), true, ev.text)
      break

    case "progress": {
      // A pulse is a snapshot, not a fact about the timeline: it carries no
      // sequence number and never becomes a block. It exists so a turn whose
      // agents are all deep inside a slow tool call still shows movement.
      // Statuses stay event-driven: a pulse built a moment before `finished`
      // can arrive after it, and reviving a finished agent is a worse lie than
      // an age that is a few seconds stale.
      const pulse = parsePulse(ev)
      if (pulse) next.pulse = pulse
      break
    }

    case "usage":
      // Token counts live on the composer, not in the transcript. A notice
      // per model call would bury the answer under billing.
      break

    case "done":
    case "error": {
      const failed = ev.kind === "error"
      const cancelled = failed && ev.err === "interrupted"
      upsertTurn(next, ev.turn_id, {
        status: cancelled ? "cancelled" : failed ? "error" : "done",
        endedAt: ev.created_at,
        final: ev.text,
        error: ev.err,
        ...(!rosterOnly && (!ev.agent_id || ev.agent_id === MANAGER_ID) && (!ev.role || ev.role === MANAGER_ID)
          ? { completedSeq: ev.seq } : {}),
      })
      next.running = false
      next.pulse = undefined
      const manager = touchAgent(next, MANAGER_ID)
      settlePendingConfirm(manager, false)
      closeStreaming(manager, "reasoning")
      closeStreaming(manager, "answer")
      closePendingTools(manager, ev.err)
      // A clean `done` parks leftover workers for the next /goal session.
      // Closing their exec here made Agents say Done while wait_agents was
      // still blocked on that same worker. A failed check may also retain
      // explicitly named workers; Stop and cleanup remain authoritative.
      if (failed) {
        settleFailedWorkers(next, ev)
        if (ev.err && !rosterOnly) append(manager, block(ev, cancelled ? "notice" : "error", ev.err))
      }
      break
    }

    case "turn":
      // Marks a model round boundary. Useful to the engine, noise to a reader.
      break

    default:
      append(agent, block(ev, "notice", ev.text ?? ev.kind))
  }

  return sealQuietTurns(next, ev)
}

/** Sub-agents still working, as the event stream last left them. The heartbeat
 *  counts from here rather than from the pulse: a count that is a pulse old
 *  would claim work is still running next to a row that says it finished. */
export function liveWorkers(state: TranscriptState): number {
  return state.agentOrder.filter(
    (id) => id !== MANAGER_ID && state.agents[id].status === "running",
  ).length
}

/** Collapse a burst of streamed events down to the latest snapshot per
 *  agent and kind. Deltas carry the accumulated string, so keeping only the
 *  last one of a burst is lossless and is what stops a 50-token burst from
 *  becoming fifty React renders. Tool deltas key on the call id so two
 *  parallel execs do not overwrite each other. Non-delta events keep their place. */
export function collapseLiveEvents(events: SwarmEvent[]): SwarmEvent[] {
  const seen = new Set<string>()
  const out: SwarmEvent[] = []
  for (let i = events.length - 1; i >= 0; i--) {
    const ev = events[i]
    if (ev.kind === "delta" || ev.kind === "reasoning_delta") {
      const key = `${ev.agent_id || MANAGER_ID}:${ev.kind}:${ev.message_id || ""}`
      if (seen.has(key)) continue
      seen.add(key)
    }
    if (ev.kind === "tool_delta" || ev.kind === "tool_call_delta") {
      const key = `${ev.agent_id || MANAGER_ID}:${ev.kind}:${ev.tool_call_id || ""}`
      if (seen.has(key)) continue
      seen.add(key)
    }
    if (ev.kind === "usage") {
      if (seen.has("usage")) continue
      seen.add("usage")
    }
    out.push(ev)
  }
  return out.reverse()
}

export function reduceEvents(
  state: TranscriptState,
  events: SwarmEvent[],
): TranscriptState {
  return events.reduce((s, ev) => reduceEvent(s, ev), state)
}

/** Seq stays 0 so a later rewind still treats this row as not-yet-stored. */
export const PENDING_EDIT_ID = "pending-edit"

/** Drop the named user message and everything after it. lastSeq stays so a
 *  reconnect does not replay the cut as if it were new. */
export function rewindTranscript(
  state: TranscriptState,
  fromSeq: number,
): TranscriptState {
  if (!(fromSeq > 0)) return state
  const keep = (b: Block) =>
    (b.seq > 0 && b.seq < fromSeq) || b.id === PENDING_EDIT_ID
  const agents: Record<string, AgentState> = {}
  const agentOrder: string[] = []
  for (const id of state.agentOrder) {
    const agent = state.agents[id]
    if (!agent) continue
    const blocks = agent.blocks.filter(keep)
    if (id !== MANAGER_ID && blocks.length === 0) continue
    agents[id] = {
      ...agent,
      blocks,
      status: "done",
      activity: "",
    }
    agentOrder.push(id)
  }
  if (!agents[MANAGER_ID]) {
    agents[MANAGER_ID] = {
      id: MANAGER_ID,
      role: MANAGER_ID,
      status: "done",
      activity: "",
      blocks: [],
    }
    agentOrder.unshift(MANAGER_ID)
  }
  const keptTurns = new Set<string>()
  let pending = false
  for (const agent of Object.values(agents)) {
    for (const b of agent.blocks) {
      keptTurns.add(b.turnId)
      if (b.id === PENDING_EDIT_ID) pending = true
    }
  }
  if (pending && agents[MANAGER_ID]) {
    agents[MANAGER_ID] = { ...agents[MANAGER_ID], status: "running" }
  }
  return {
    agentOrder,
    agents,
    turns: state.turns.filter((t) => keptTurns.has(t.id)).map((t) => ({
      ...t,
      completedSeq: t.completedSeq && t.completedSeq >= fromSeq ? undefined : t.completedSeq,
      workerRetention: t.workerRetention && t.workerRetention.seq >= fromSeq ? undefined : t.workerRetention,
    })),
    lastSeq: state.lastSeq,
    running: pending,
    pulse: undefined,
    retractedSteers: (state.retractedSteers ?? []).filter((s) => s < fromSeq),
    steerRevisions: steerRevisionsBefore(state.steerRevisions, fromSeq),
    quietTurns: (state.quietTurns ?? []).filter((id) => keptTurns.has(id)),
    scheduledFiredTurns: (state.scheduledFiredTurns ?? []).filter((id) => keptTurns.has(id)),
    rewindCut: { from: fromSeq, through: state.lastSeq, live: true },
    recoveryEvents: state.recoveryEvents?.filter((event) => event.seq > 0 && event.seq < fromSeq),
  }
}

function droppedByRewind(state: TranscriptState, ev: SwarmEvent): boolean {
  const cut = state.rewindCut
  if (!cut || ev.kind === "rewound") return false
  if (ev.seq > 0 && ev.seq >= cut.from && ev.seq <= cut.through) return true
  return cut.live && !(ev.seq > 0)
}

/** Put the edited text back at the cut so Send looks like Codex: this
 *  bubble stays, everything below is gone. The real user_message replaces
 *  this row when it lands. */
export function placePendingEdit(
  state: TranscriptState,
  text: string,
  images?: ImageRef[],
): TranscriptState {
  const next: TranscriptState = {
    agentOrder: state.agentOrder.includes(MANAGER_ID)
      ? state.agentOrder
      : [MANAGER_ID, ...state.agentOrder],
    agents: { ...state.agents },
    turns: state.turns,
    lastSeq: state.lastSeq,
    running: true,
    pulse: undefined,
    retractedSteers: state.retractedSteers,
    steerRevisions: state.steerRevisions,
    quietTurns: state.quietTurns?.slice(),
    scheduledFiredTurns: state.scheduledFiredTurns?.slice(),
    rewindCut: state.rewindCut,
    recoveryEvents: state.recoveryEvents,
  }
  const agent = touchAgent(next, MANAGER_ID)
  agent.blocks = agent.blocks.filter((b) => b.id !== PENDING_EDIT_ID)
  agent.status = "running"
  append(agent, {
    id: PENDING_EDIT_ID,
    kind: "user",
    agentId: MANAGER_ID,
    text,
    images,
    turnId: PENDING_EDIT_ID,
    seq: 0,
    at: "",
  })
  return next
}

/** Live output can correct a stale parked roster, but cannot undo Stop.
 *  Cancelled workers need an explicit spawned event before output resumes. */
function keepWorkerLive(agent: AgentState): boolean {
  if (agent.status === "cancelled") return false
  if (agent.id === MANAGER_ID || agent.status === "running") return true
  agent.status = "running"
  agent.endedAt = undefined
  agent.error = undefined
  agent.errorSource = undefined
  return true
}

/** Older spawned events stored the role in `text`. The instruction only
 *  exists when `role` is set and `text` is something else — the system prompt
 *  that worker actually received. */
function recordedInstruction(role?: string, text?: string): string | undefined {
  const prompt = (text ?? "").trim()
  const name = (role ?? "").trim()
  if (!prompt || !name || prompt === name) return undefined
  return prompt
}

function touchAgent(
  state: TranscriptState,
  id: string,
  role?: string,
): AgentState {
  const existing = state.agents[id]
  if (existing) {
    const updated = { ...existing, blocks: existing.blocks.slice() }
    if (role && !existing.role) updated.role = role
    state.agents[id] = updated
    return updated
  }
  const created: AgentState = {
    id,
    role: role || (id === MANAGER_ID ? "manager" : id),
    status: "running",
    activity: "",
    blocks: [],
  }
  state.agents[id] = created
  state.agentOrder =
    id === MANAGER_ID ? [id, ...state.agentOrder] : [...state.agentOrder, id]
  return created
}

function upsertTurn(
  state: TranscriptState,
  turnId: string,
  patch: Partial<TurnState>,
) {
  const i = state.turns.findIndex((t) => t.id === turnId)
  if (i === -1) {
    state.turns = [
      ...state.turns,
      { id: turnId, userText: "", status: "running", agentIds: [], ...patch },
    ]
    return
  }
  const turns = state.turns.slice()
  turns[i] = { ...turns[i], ...patch }
  state.turns = turns
}

function addAgentToTurn(state: TranscriptState, turnId: string, agentId: string) {
  const i = state.turns.findIndex((t) => t.id === turnId)
  if (i === -1) {
    state.turns = [
      ...state.turns,
      { id: turnId, userText: "", status: "running", agentIds: [agentId] },
    ]
    return
  }
  if (state.turns[i].agentIds.includes(agentId)) return
  const turns = state.turns.slice()
  turns[i] = { ...turns[i], agentIds: [...turns[i].agentIds, agentId] }
  state.turns = turns
}

/** Stable with engine.resumeToolStopped. The wire event is English; a
 *  crashed exec must not keep saying "running…". */
const resumeToolStopped = "the previous process stopped"

function settlePendingConfirm(agent: AgentState, continued: boolean, text?: string) {
  const blocks = agent.blocks.slice()
  for (let i = blocks.length - 1; i >= 0; i--) {
    if (blocks[i].kind === "confirm" && blocks[i].confirm?.pending) {
      blocks[i] = {
        ...blocks[i],
        text: text ?? blocks[i].text,
        confirm: { ...blocks[i].confirm!, pending: false, continued },
      }
      break
    }
  }
  agent.blocks = blocks
}
