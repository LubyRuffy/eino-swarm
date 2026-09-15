import type { ReviewOutcome, SwarmEvent } from "./types"

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
  | "spawn"
  | "notice"
  | "error"

export interface Block {
  id: string
  kind: BlockKind
  agentId: string
  role?: string
  text: string
  /** Still being streamed. Complete blocks stop showing a cursor and can be
   *  collapsed. */
  streaming?: boolean
  /** Tool blocks only. */
  tool?: {
    callId: string
    name: string
    args: string
    result?: string
    failed?: boolean
    /** A call with no result yet is still running. */
    pending: boolean
  }
  /** Spawn blocks only: which sub-agent was started. */
  spawn?: { agentId: string; role: string }
  turnId: string
  seq: number
  at: string
}

export type AgentStatus = "running" | "done" | "failed"

export interface AgentState {
  id: string
  role: string
  status: AgentStatus
  /** The last thing this agent did, for the one-line summary in the roster. */
  activity: string
  blocks: Block[]
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
}

export const MANAGER_ID = "manager"

export function emptyTranscript(): TranscriptState {
  return { agentOrder: [], agents: {}, turns: [], lastSeq: 0, running: false }
}

/** Fold one event into the transcript, returning a new state. Pure, so the
 *  store stays a thin wrapper and the interesting logic is testable without
 *  a browser. */
export function reduceEvent(
  state: TranscriptState,
  ev: SwarmEvent,
): TranscriptState {
  const next: TranscriptState = {
    agentOrder: state.agentOrder,
    agents: { ...state.agents },
    turns: state.turns,
    lastSeq: ev.seq > state.lastSeq ? ev.seq : state.lastSeq,
    running: state.running,
    pulse: state.pulse,
  }
  // The memory review runs after the turn, on its own. It is not a member of
  // the swarm, so it must not join the roster as a worker with nothing to
  // show; and when it kept nothing — the common case — it says nothing.
  if (ev.kind === "memory_review") {
    const notice = reviewNotice(parseReview(ev))
    if (notice) append(touchAgent(next, MANAGER_ID), block(ev, "notice", notice))
    return next
  }

  const agentId = ev.agent_id || MANAGER_ID
  const agent = touchAgent(next, agentId, ev.role)

  switch (ev.kind) {
    case "user_message":
      upsertTurn(next, ev.turn_id, { userText: ev.text ?? "", status: "running", startedAt: ev.created_at })
      next.running = true
      next.pulse = undefined
      resetManagerForNewTurn(next, ev.turn_id)
      append(agent, block(ev, "user", ev.text ?? ""))
      break

    case "steer":
      append(agent, block(ev, "steer", ev.text ?? ""))
      break

    case "reasoning_delta":
      // Deltas carry the whole accumulated string, so the open block is
      // replaced. Appending here is the classic bug that doubles every word.
      replaceStreaming(agent, ev, "reasoning")
      agent.activity = "thinking"
      break

    case "delta":
      // The model has started answering, so any thinking block is done.
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
      closeStreaming(agent, "reasoning")
      replaceComplete(agent, ev, "answer")
      agent.activity = summarise(ev.text ?? "")
      break

    case "tool_call": {
      closeStreaming(agent, "reasoning")
      closeStreaming(agent, "answer")
      const { name, args } = splitToolCall(ev.text ?? "")
      append(agent, {
        ...block(ev, "tool", name),
        tool: { callId: ev.tool_call_id ?? "", name, args, pending: true },
      })
      agent.activity = `${name}`
      break
    }

    case "tool_result": {
      // Pair by call id: a manager that fans out four calls at once gets four
      // results back in whatever order they finish.
      const target = findToolBlock(agent, ev.tool_call_id)
      if (target?.tool) {
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
      const child = touchAgent(next, ev.agent_id, ev.role ?? ev.text)
      const manager = touchAgent(next, MANAGER_ID)
      const seen = manager.blocks.some((b) => b.kind === "spawn" && b.spawn?.agentId === child.id)
      child.status = "running"
      child.endedAt = undefined
      child.error = undefined
      child.activity = seen ? "continuing" : "starting"
      if (!seen) child.startedAt = ev.created_at
      addAgentToTurn(next, ev.turn_id, child.id)
      // A resume re-emits spawned for the same id so the roster comes back
      // to life. A second "Started" row is the bug that looks like two
      // workers with the same name.
      if (!seen) {
        append(manager, {
          ...block(ev, "spawn", ev.role ?? ev.text ?? "sub-agent"),
          agentId: MANAGER_ID,
          spawn: { agentId: child.id, role: ev.role ?? ev.text ?? "sub-agent" },
        })
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
      break
    }

    case "cleanup":
      append(touchAgent(next, MANAGER_ID), block(ev, "notice", ev.text ?? ""))
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
      for (const id of next.agentOrder) {
        const a = next.agents[id]
        closeStreaming(a, "reasoning")
        closeStreaming(a, "answer")
        if (a.status === "running") {
          a.status = failed ? "failed" : "done"
          a.activity = failed ? "stopped" : "done"
        }
      }
      if (failed && ev.err) {
        append(touchAgent(next, MANAGER_ID), block(ev, "error", ev.err))
      }
      break
    }

    case "turn":
      // Marks a model round boundary. Useful to the engine, noise to a reader.
      break

    default:
      append(agent, block(ev, "notice", ev.text ?? ev.kind))
  }

  return next
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
 *  becoming fifty React renders. Non-delta events keep their place. */
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
    out.push(ev)
  }
  return out.reverse()
}

export function reduceEvents(
  state: TranscriptState,
  events: SwarmEvent[],
): TranscriptState {
  return events.reduce(reduceEvent, state)
}

/** The manager's transcript is per turn: the previous turn's blocks stay in
 *  the conversation list, but a new turn starts a fresh section. */
function resetManagerForNewTurn(state: TranscriptState, _turnId: string) {
  void _turnId
  for (const id of state.agentOrder) {
    const a = state.agents[id]
    if (a.status === "running" && id !== MANAGER_ID) {
      // A sub-agent still marked running when a new turn starts was killed by
      // the end-of-turn cleanup; showing it as running forever is a lie.
      state.agents[id] = { ...a, status: "done", activity: "done" }
    }
  }
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
    status: id === MANAGER_ID ? "running" : "running",
    activity: "",
    blocks: [],
  }
  state.agents[id] = created
  state.agentOrder =
    id === MANAGER_ID
      ? [id, ...state.agentOrder]
      : [...state.agentOrder, id]
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

function lastOpenIndex(blocks: Block[], kind: BlockKind): number {
  for (let i = blocks.length - 1; i >= 0; i--) {
    if (blocks[i].kind === kind && blocks[i].streaming) return i
    // A block of another kind closes the run: thinking then a tool call then
    // more thinking is two separate thinking blocks, not one.
    if (blocks[i].kind === "tool" || blocks[i].kind === "spawn") return -1
  }
  return -1
}

function openId(ev: SwarmEvent, kind: string): string {
  return `${ev.turn_id}:${ev.agent_id || MANAGER_ID}:${kind}:open:${ev.created_at}`
}

function findToolBlock(agent: AgentState, callId?: string): Block | undefined {
  const blocks = agent.blocks
  for (let i = blocks.length - 1; i >= 0; i--) {
    const b = blocks[i]
    if (b.kind !== "tool" || !b.tool) continue
    if (callId && b.tool.callId === callId) {
      const copy = { ...b, tool: { ...b.tool } }
      agent.blocks = blocks.map((x, j) => (j === i ? copy : x))
      return copy
    }
    if (!callId && b.tool.pending) {
      const copy = { ...b, tool: { ...b.tool } }
      agent.blocks = blocks.map((x, j) => (j === i ? copy : x))
      return copy
    }
  }
  return undefined
}

/** Read a pulse out of an event. A malformed one is dropped rather than
 *  rendered: the next pulse is seconds away, and half a snapshot would show a
 *  turn with no agents in it. */
export function parsePulse(ev: SwarmEvent): Pulse | undefined {
  let raw: unknown
  try {
    raw = JSON.parse(ev.text ?? "")
  } catch {
    return undefined
  }
  if (!raw || typeof raw !== "object") return undefined
  const body = raw as { elapsed_ms?: unknown; agents?: unknown }
  const agents = Array.isArray(body.agents) ? body.agents : []
  return {
    at: ev.created_at,
    elapsedMs: num(body.elapsed_ms),
    agents: agents.flatMap((a: unknown) => {
      const row = a as { agent_id?: unknown; role?: unknown; status?: unknown; elapsed_ms?: unknown }
      if (typeof row?.agent_id !== "string" || !row.agent_id) return []
      return [
        {
          agentId: row.agent_id,
          role: typeof row.role === "string" ? row.role : undefined,
          status: pulseStatus(row.status),
          elapsedMs: num(row.elapsed_ms),
        },
      ]
    }),
  }
}

/** Read a review's outcome out of an event. A malformed one is dropped: the
 *  review is an extra, and half a summary is worse than none. */
export function parseReview(ev: SwarmEvent): ReviewOutcome | undefined {
  let raw: unknown
  try {
    raw = JSON.parse(ev.text ?? "")
  } catch {
    return undefined
  }
  if (!raw || typeof raw !== "object") return undefined
  const body = raw as Partial<ReviewOutcome>
  return {
    changed: body.changed === true,
    notes: typeof body.notes === "object" && body.notes ? body.notes : undefined,
    skills: Array.isArray(body.skills) ? body.skills : undefined,
    note: typeof body.note === "string" ? body.note : undefined,
    err: typeof body.err === "string" && body.err ? body.err : ev.err || undefined,
  }
}

/** What the review's tool actions are called in the transcript. */
const NOTE_VERBS: Record<string, string> = {
  add: "stored",
  replace: "revised",
  remove: "removed",
}
const SKILL_VERBS: Record<string, string> = {
  create: "recorded",
  patch: "updated",
  delete: "removed",
}

/** One line for the transcript, or nothing.
 *
 *  Most turns teach the project nothing, and a row saying so after every
 *  answer would train the reader to stop looking at the ones that matter. */
export function reviewNotice(outcome?: ReviewOutcome): string | undefined {
  if (!outcome) return undefined
  if (outcome.err) return `Memory review failed: ${outcome.err}`
  if (!outcome.changed) return undefined
  const parts: string[] = []
  for (const action of Object.keys(NOTE_VERBS)) {
    const n = outcome.notes?.[action] ?? 0
    if (n > 0) parts.push(`${n} ${n === 1 ? "note" : "notes"} ${NOTE_VERBS[action]}`)
  }
  for (const s of outcome.skills ?? []) {
    if (!s?.name) continue
    parts.push(`skill "${s.name}" ${SKILL_VERBS[s.action] ?? s.action}`)
  }
  if (parts.length === 0) return undefined
  return `Memory updated: ${parts.join(", ")}.`
}

function pulseStatus(raw: unknown): AgentStatus {
  return raw === "running" || raw === "failed" ? raw : "done"
}

function num(raw: unknown): number {
  return typeof raw === "number" && Number.isFinite(raw) ? raw : 0
}

/** The server sends `name({json})`; the UI shows the verb and hides the
 *  arguments until asked. */
export function splitToolCall(raw: string): { name: string; args: string } {
  const open = raw.indexOf("(")
  if (open === -1) return { name: raw.trim(), args: "" }
  const name = raw.slice(0, open).trim()
  let args = raw.slice(open + 1)
  if (args.endsWith(")")) args = args.slice(0, -1)
  return { name: name || "tool", args }
}

export function summarise(text: string, max = 80): string {
  const line = text.trim().replace(/\s+/g, " ")
  return line.length > max ? `${line.slice(0, max - 1)}…` : line
}
