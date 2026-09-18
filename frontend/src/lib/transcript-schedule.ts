import type { AgentState, Block, BlockKind, TranscriptState } from "./transcript"
import type { SwarmEvent } from "./types"

const MANAGER_ID = "manager"

const SCHEDULE_KINDS = new Set([
  "schedule",
  "schedule_fired",
  "schedule_skipped",
  "schedule_report",
  "schedule_cancelled",
])

/** Chat rows a quiet scheduled check must not leave on screen. Trace still
 *  has the events; this list is the reducer's output, not the log. */
const QUIET_DROP = new Set<BlockKind>(["user", "answer", "tool", "reasoning", "notice"])

/** Fold one schedule kind. True means reduceEvent should stop: skipped is a
 *  handled no-op, not an unknown kind that becomes a JSON notice. */
export function applyScheduleEvent(state: TranscriptState, ev: SwarmEvent): boolean {
  if (!SCHEDULE_KINDS.has(ev.kind)) return false
  switch (ev.kind) {
    case "schedule": {
      const row = block(ev, "notice", "A wait is armed.")
      const id = scheduleIdFromArmed(ev.text)
      if (id) row.detail = id
      append(touchAgent(state, MANAGER_ID), row)
      return true
    }
    case "schedule_fired": {
      state.running = true
      stampFired(state, ev.turn_id)
      upsertTurn(state, ev.turn_id, { status: "running", startedAt: ev.created_at })
      const manager = touchAgent(state, MANAGER_ID)
      manager.status = "running"
      append(manager, block(ev, "notice", ev.text?.trim() || "Scheduled check."))
      return true
    }
    case "schedule_skipped":
      return true
    case "schedule_cancelled": {
      const row = block(ev, "notice", "A wait was cancelled.")
      const id = ev.text?.trim()
      if (id) row.detail = id
      append(touchAgent(state, MANAGER_ID), row)
      return true
    }
    case "schedule_report": {
      if (isQuietReport(ev.text)) {
        markQuiet(state, ev.turn_id)
        dropQuietChat(state, ev.turn_id)
      } else {
        upsertTurn(state, ev.turn_id, { scheduledFindings: true })
      }
      return true
    }
    default:
      return true
  }
}

/** Later deltas / answers for a quiet turn_id must not grow the bubbles back.
 *  Pass the current event: an empty `done` after `schedule_fired` with no
 *  findings report is the omitted-report quiet path. An armed wait, another
 *  notice, or a findings `schedule_report` plus empty `done` stays visible. */
export function sealQuietTurns(state: TranscriptState, ev?: SwarmEvent): TranscriptState {
  if (ev && isOmittedQuietDone(state, ev)) {
    markQuiet(state, ev.turn_id)
    dropQuietChat(state, ev.turn_id)
  }
  for (const id of state.quietTurns ?? []) dropQuietChat(state, id)
  hideScheduledCheckUser(state)
  return state
}

function isOmittedQuietDone(state: TranscriptState, ev: SwarmEvent): boolean {
  if (ev.kind !== "done" || (ev.text?.trim() ?? "") !== "") return false
  if (!ev.turn_id) return false
  if (!(state.scheduledFiredTurns ?? []).includes(ev.turn_id)) return false
  return state.turns.find((t) => t.id === ev.turn_id)?.scheduledFindings !== true
}

function isQuietReport(text?: string): boolean {
  const raw = text?.trim() ?? ""
  if (!raw) return true
  try {
    const body = JSON.parse(raw) as { findings?: unknown; quiet?: unknown }
    if (body.quiet === true) return true
    const findings = typeof body.findings === "string" ? body.findings.trim() : ""
    return findings === ""
  } catch {
    return true
  }
}

function scheduleIdFromArmed(text?: string): string {
  const raw = text?.trim() ?? ""
  if (!raw.startsWith("{")) return ""
  try {
    const body = JSON.parse(raw) as { id?: unknown }
    return typeof body.id === "string" ? body.id : ""
  } catch {
    return ""
  }
}

function stampFired(state: TranscriptState, turnId: string) {
  if (!turnId) return
  const ids = state.scheduledFiredTurns ?? []
  if (!ids.includes(turnId)) state.scheduledFiredTurns = [...ids, turnId]
}

function markQuiet(state: TranscriptState, turnId: string) {
  if (!turnId) return
  const ids = state.quietTurns ?? []
  if (!ids.includes(turnId)) state.quietTurns = [...ids, turnId]
  upsertTurn(state, turnId, { quiet: true })
}

function dropQuietChat(state: TranscriptState, turnId: string) {
  if (!turnId) return
  for (const id of Object.keys(state.agents)) {
    const agent = state.agents[id]
    const blocks = agent.blocks.filter(
      (b) => b.turnId !== turnId || !QUIET_DROP.has(b.kind),
    )
    if (blocks.length !== agent.blocks.length) {
      state.agents[id] = { ...agent, blocks }
    }
  }
}

/** The durable scheduled-check wrapper is protocol, not a human bubble. */
function hideScheduledCheckUser(state: TranscriptState) {
  for (const id of Object.keys(state.agents)) {
    const agent = state.agents[id]
    const blocks = agent.blocks.filter(
      (b) => !(b.kind === "user" && b.text.startsWith("This turn is a scheduled check.")),
    )
    if (blocks.length !== agent.blocks.length) {
      state.agents[id] = { ...agent, blocks }
    }
  }
}

function touchAgent(state: TranscriptState, id: string, role?: string): AgentState {
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
  patch: {
    status?: "running" | "done" | "error" | "cancelled"
    startedAt?: string
    quiet?: boolean
    scheduledFindings?: boolean
  },
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
