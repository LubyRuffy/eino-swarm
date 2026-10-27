import type { SwarmEvent } from "./types"
import type { AgentStatus, Pulse } from "./transcript"
import type { AgentState, TranscriptState } from "./transcript"

export interface ManagerPause {
  limit: number
  extendBy: number
  reason?: "workers_uncollected"
  automaticResume?: boolean
  waitId?: string
  sourceSeq?: number
  /** The validated Stop decision, replayed from its actual stored receipt. */
  declinedSeq?: number
  pending: boolean
  continued?: boolean
}

export function updateWorkerCollectionPause(agent: AgentState, ev: SwarmEvent, pause: Omit<ManagerPause, "pending" | "continued"> | undefined): boolean {
  if (pause?.reason !== "workers_uncollected" || !pause.waitId) return false
  const index = agent.blocks.findIndex((block) => block.turnId === ev.turn_id && block.confirm?.waitId === pause.waitId)
  if (index < 0) return false
  if ((agent.blocks[index].confirm?.sourceSeq ?? agent.blocks[index].seq) >= ev.seq) return true
  agent.blocks = agent.blocks.map((block, i) => i === index
    ? { ...block, text: ev.text ?? "", confirm: { ...pause, pending: true } } : block)
  return true
}

function resumedWaitId(ev: SwarmEvent): string | undefined {
  try {
    const id: unknown = JSON.parse(ev.text ?? "")?.wait_id
    return typeof id === "string" && id ? id : undefined
  } catch { return undefined }
}

export function isCurrentWorkerCollectionResume(agent: AgentState | undefined, ev: SwarmEvent): boolean {
  const waitId = resumedWaitId(ev)
  if (!agent || !waitId) return false
  for (let i = agent.blocks.length - 1; i >= 0; i--) {
    const block = agent.blocks[i]
    if (block.confirm?.pending) return ev.seq > (block.confirm.sourceSeq ?? block.seq) && block.turnId === ev.turn_id &&
      block.confirm.reason === "workers_uncollected" && block.confirm.waitId === waitId
  }
  return false
}

export function isCurrentWorkerCollectionDecline(agent: AgentState | undefined, ev: SwarmEvent): boolean {
  return ev.kind === "workers_collection_declined" &&
    (!ev.agent_id || ev.agent_id === "manager") && (!ev.role || ev.role === "manager") &&
    isCurrentWorkerCollectionResume(agent, ev)
}

export function applyWorkerCollectionDecline(state: TranscriptState, ev: SwarmEvent): boolean {
  if (ev.kind !== "workers_collection_declined") return false
  // This internal decision settles its existing card without creating a chat
  // row or agent. Its pre-Stop snapshot cannot change worker lifecycle state.
  const manager = state.agents.manager
  if (manager && isCurrentWorkerCollectionDecline(manager, ev)) {
    const settled = { ...manager }
    settleWorkerCollectionPause(settled, ev, false)
    state.agents.manager = settled
  }
  return true
}

export function settleWorkerCollectionPause(agent: AgentState, ev: SwarmEvent, continued = true) {
  const waitId = resumedWaitId(ev)
  if (!waitId) return
  let index = agent.blocks.length - 1
  while (index >= 0 && !(agent.blocks[index].turnId === ev.turn_id &&
    agent.blocks[index].confirm?.waitId === waitId && agent.blocks[index].confirm?.reason === "workers_uncollected" &&
    agent.blocks[index].confirm?.pending && ev.seq > (agent.blocks[index].confirm?.sourceSeq ?? agent.blocks[index].seq))) index--
  if (index < 0) return
  agent.blocks = agent.blocks.map((block, i) => i === index
    ? { ...block, confirm: { ...block.confirm!, pending: false, continued, ...(!continued ? { declinedSeq: ev.seq } : {}) } }
    : block)
}

/** Read the cap payload out of a max_iterations event. A malformed one still
 *  renders as a card; the numbers just show as zero rather than crashing. */
export function parseIterationLimit(
  ev: SwarmEvent,
): Omit<ManagerPause, "pending" | "continued"> | undefined {
  let raw: unknown
  try {
    raw = JSON.parse(ev.text ?? "")
  } catch {
    return undefined
  }
  if (!raw || typeof raw !== "object") return undefined
  const body = raw as { limit?: unknown; extend_by?: unknown; reason?: unknown; automatic_resume?: unknown; wait_id?: unknown }
  return { limit: num(body.limit), extendBy: num(body.extend_by),
    ...(body.reason === "workers_uncollected" ? {
      reason: "workers_uncollected" as const, automaticResume: body.automatic_resume === true,
      sourceSeq: ev.seq,
      ...(typeof body.wait_id === "string" ? { waitId: body.wait_id } : {}),
    } : {}),
  }
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

function pulseStatus(raw: unknown): AgentStatus {
  return raw === "running" || raw === "failed" ? raw : "done"
}

function num(raw: unknown): number {
  return typeof raw === "number" && Number.isFinite(raw) ? raw : 0
}
