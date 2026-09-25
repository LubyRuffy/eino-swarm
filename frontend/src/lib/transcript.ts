import type { ImageRef, SwarmEvent } from "./types"
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
import { askCardFromEvent, parseAskResult, ASK_TOOL, type AskCard } from "./transcript-ask"
import { isPlanEvent, planNotice } from "./transcript-plan"
import { parseIterationLimit, parsePulse } from "./transcript-pulse"
import {
  dropRetractedSteer,
  isRetractedSteer,
  parseSteerRetractSeq,
} from "./transcript-steer"
import { applyScheduleEvent, sealQuietTurns } from "./transcript-schedule"
import { findToolBlock, splitToolCall, summarise } from "./transcript-tool-block"

export { splitToolCall, summarise }

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
  /** Still being streamed. Complete blocks stop showing a cursor and can be
   *  collapsed. */
  streaming?: boolean
  /** A review the user asked not to see in the transcript. It still exists
   *  so the Trace tab's Full log and resume point do not lose the event. */
  quiet?: boolean
  /** Extra body a notice can open. Compact keeps the briefing here so the
   *  chat row stays a one-liner instead of pasting the next prompt. */
  detail?: string
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

export type AgentStatus = "running" | "done" | "failed"

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
}

export interface TurnState {
  id: string
  userText: string
  status: "running" | "done" | "error" | "cancelled"
  startedAt?: string
  endedAt?: string
  final?: string
  error?: string
  /** Sub-agents spawned during this turn, in the order they appeared. */
  agentIds: string[]
  /** True when this turn is a /goal work session (auto-continue or a forced yield). */
  session?: boolean
  /** quiet: omitted scheduled check. scheduledFindings: keep the chip on empty done. */
  quiet?: boolean
  scheduledFindings?: boolean
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
  quietTurns?: string[]
  scheduledFiredTurns?: string[]
  /** Stored seqs in `[from, through]` belong to a resent message. Live
   *  deltas stay dropped until the replacement `user_message` arrives. */
  rewindCut?: { from: number; through: number; live: boolean }
}

export const MANAGER_ID = "manager"

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
    quietTurns: state.quietTurns?.slice(),
    scheduledFiredTurns: state.scheduledFiredTurns?.slice(),
    rewindCut: state.rewindCut,
  }
  if (applyScheduleEvent(next, ev)) return sealQuietTurns(next, ev)
  if (ev.kind === "steer_preempted") {
    return next
  }
  if (ev.kind === "steer_retracted") {
    dropRetractedSteer(next, parseSteerRetractSeq(ev.text))
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
      append(touchAgent(next, MANAGER_ID), row)
    }
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
  if (ev.kind === "resumed") {
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
      const agent = next.agents[id]
      if (agent) closePendingTools(agent, resumeToolStopped, true)
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
    append(manager, block(ev, "notice", ev.text?.trim() || "Continuing the standing objective."))
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
  if (ev.kind === "goal_edited") {
    append(touchAgent(next, MANAGER_ID), block(ev, "notice", "Standing objective updated."))
    return next
  }
  if (ev.kind === "goal_resumed") {
    next.running = true
    upsertTurn(next, ev.turn_id, { status: "running", startedAt: ev.created_at })
    const manager = touchAgent(next, MANAGER_ID)
    manager.status = "running"
    append(manager, block(ev, "notice", ev.text?.trim() || "Resuming the standing objective."))
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

  switch (ev.kind) {
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
        append(agent, block(ev, "steer", ev.text ?? ""))
      }
      break

    case "reasoning_delta":
      // Deltas carry the whole accumulated string, so the open block is
      // replaced. Appending here is the classic bug that doubles every word.
      keepWorkerLive(agent)
      replaceStreaming(agent, ev, "reasoning")
      agent.activity = "thinking"
      break

    case "delta":
      // The model has started answering, so any thinking block is done.
      keepWorkerLive(agent)
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
      keepWorkerLive(agent)
      closeStreaming(agent, "reasoning")
      replaceComplete(agent, ev, "answer")
      agent.activity = summarise(ev.text ?? "")
      break

    case "tool_call":
    case "tool_call_delta": {
      keepWorkerLive(agent)
      closeStreaming(agent, "reasoning")
      closeStreaming(agent, "answer")
      const { name, args } = splitToolCall(ev.text ?? "")
      const writing = ev.kind === "tool_call_delta" ? Number(args) : undefined
      const ask = ev.kind === "tool_call" ? askCardFromEvent(ev, name, args) : undefined
      const existing = ev.tool_call_id ? findToolBlock(agent, ev.tool_call_id) : undefined
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
        existing.question = ask
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
      const target = findToolBlock(agent, ev.tool_call_id)
      if (target?.question) {
        target.question = {
          ...target.question,
          pending: false,
          answers: parseAskResult(ev.text ?? ""),
        }
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
      const instruction = recordedInstruction(ev.role, ev.text)
      if (instruction) child.instruction = instruction
      const manager = touchAgent(next, MANAGER_ID)
      const painted = manager.blocks.some((b) => b.kind === "spawn" && b.spawn?.agentId === child.id)
      child.status = "running"
      child.endedAt = undefined
      child.error = undefined
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
      child.status = ev.err ? "failed" : "done"
      child.endedAt = ev.created_at
      child.result = ev.text
      child.error = ev.err
      child.activity = ev.err ? "failed" : "done"
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
        child.status = "done"
        child.activity = "done"
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
      upsertTurn(next, ev.turn_id, {
        status: failed ? "error" : "done",
        endedAt: ev.created_at,
        final: ev.text,
        error: ev.err,
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
      // still blocked on that same worker. An error/interrupt does kill them.
      if (failed) {
        for (const id of next.agentOrder) {
          if (id === MANAGER_ID) continue
          const child = next.agents[id]
          if (!child) continue
          closeStreaming(child, "reasoning")
          closeStreaming(child, "answer")
          closePendingTools(child, ev.err)
          if (child.status === "running") {
            child.status = "failed"
            child.activity = "stopped"
          }
        }
        if (ev.err) append(manager, block(ev, "error", ev.err))
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
      const key = `${ev.agent_id || MANAGER_ID}:${ev.kind}`
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
    turns: state.turns.filter((t) => keptTurns.has(t.id)),
    lastSeq: state.lastSeq,
    running: pending,
    pulse: undefined,
    retractedSteers: (state.retractedSteers ?? []).filter((s) => s < fromSeq),
    quietTurns: (state.quietTurns ?? []).filter((id) => keptTurns.has(id)),
    scheduledFiredTurns: (state.scheduledFiredTurns ?? []).filter((id) => keptTurns.has(id)),
    rewindCut: { from: fromSeq, through: state.lastSeq, live: true },
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
    quietTurns: state.quietTurns?.slice(),
    scheduledFiredTurns: state.scheduledFiredTurns?.slice(),
    rewindCut: state.rewindCut,
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

/** A parked worker that is still emitting is not done, even if an older
 *  reducer painted it that way. Live work wins over a stale roster tick. */
function keepWorkerLive(agent: AgentState) {
  if (agent.id === MANAGER_ID || agent.status === "running") return
  agent.status = "running"
  agent.endedAt = undefined
  agent.error = undefined
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

function block(ev: SwarmEvent, kind: BlockKind, text: string): Block {
  return {
    id: `${ev.turn_id}:${ev.agent_id || MANAGER_ID}:${kind}:${ev.seq || `d${ev.created_at}`}:${text.length}`,
    kind,
    agentId: ev.agent_id || MANAGER_ID,
    role: ev.role,
    text,
    turnId: ev.turn_id,
    seq: ev.seq,
    at: ev.created_at,
    images: ev.images,
  }
}

function append(agent: AgentState, b: Block) {
  agent.blocks = [...agent.blocks, b]
}

/** Replace the open streaming block of this kind, or open one. */
function replaceStreaming(
  agent: AgentState,
  ev: SwarmEvent,
  kind: "reasoning" | "answer",
) {
  const blocks = agent.blocks.slice()
  const i = lastOpenIndex(blocks, kind)
  if (i === -1) {
    blocks.push({ ...block(ev, kind, ev.text ?? ""), streaming: true, id: openId(ev, kind) })
  } else {
    blocks[i] = { ...blocks[i], text: ev.text ?? "", streaming: true }
  }
  agent.blocks = blocks
}

/** Replace the open streaming block with the server's complete text, or add
 *  it if nothing was streamed (a non-streaming model, or a replay). */
function replaceComplete(
  agent: AgentState,
  ev: SwarmEvent,
  kind: "reasoning" | "answer",
) {
  const blocks = agent.blocks.slice()
  const i = lastOpenIndex(blocks, kind)
  if (i === -1) {
    blocks.push({ ...block(ev, kind, ev.text ?? "") })
  } else {
    blocks[i] = { ...blocks[i], text: ev.text ?? "", streaming: false, seq: ev.seq }
  }
  agent.blocks = blocks
}

function closeStreaming(agent: AgentState, kind: "reasoning" | "answer") {
  const i = lastOpenIndex(agent.blocks, kind)
  if (i === -1) return
  const blocks = agent.blocks.slice()
  blocks[i] = { ...blocks[i], streaming: false }
  agent.blocks = blocks
}

/** Stable with engine.resumeToolStopped. The wire event is English; a
 *  crashed exec must not keep saying "running…". */
const resumeToolStopped = "the previous process stopped"

/** A call with no result when the turn (or the agent) ends was killed, not
 *  left running. The spinner is keyed off `pending`; leaving it true next to
 *  an "interrupted" banner is the lie the Stop button used to leave behind.
 *  `result` must be a string or the expanded body still says "running…". */
function closePendingTools(agent: AgentState, reason?: string, keepQuestions?: boolean) {
  let changed = false
  const blocks = agent.blocks.map((b) => {
    if (b.kind === "question" && b.question?.pending) {
      if (keepQuestions) return b
      changed = true
      return { ...b, question: { ...b.question, pending: false } }
    }
    if (b.kind !== "tool" || !b.tool?.pending) return b
    changed = true
    return {
      ...b,
      tool: {
        ...b.tool,
        pending: false,
        failed: true,
        result: b.tool.result ?? reason ?? "",
      },
    }
  })
  if (changed) agent.blocks = blocks
}

function lastOpenIndex(blocks: Block[], kind: BlockKind): number {
  for (let i = blocks.length - 1; i >= 0; i--) {
    if (blocks[i].kind === kind && blocks[i].streaming) return i
    // A block of another kind closes the run: thinking then a tool call then
    // more thinking is two separate thinking blocks, not one.
    if (blocks[i].kind === "tool" || blocks[i].kind === "spawn" || blocks[i].kind === "question") return -1
  }
  return -1
}

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

function openId(ev: SwarmEvent, kind: string): string {
  return `${ev.turn_id}:${ev.agent_id || MANAGER_ID}:${kind}:open:${ev.created_at}`
}

/** The tool a result belongs to, looked up by call id after the reducer has
 *  paired them. Used to refresh the Memory panel the moment a write lands,
 *  rather than waiting for the review that may never come (memory off). */
export function toolNameOf(state: TranscriptState, callId?: string): string | undefined {
  if (!callId) return undefined
  for (const id of state.agentOrder) {
    for (const b of state.agents[id]?.blocks ?? []) {
      if (b.kind === "question" && b.question?.callId === callId) return ASK_TOOL
      if (b.kind === "tool" && b.tool?.callId === callId) return b.tool.name
    }
  }
}
