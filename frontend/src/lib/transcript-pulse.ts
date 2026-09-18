import type { SwarmEvent } from "./types"
import type { AgentStatus, Pulse } from "./transcript"

/** Read the cap payload out of a max_iterations event. A malformed one still
 *  renders as a card; the numbers just show as zero rather than crashing. */
export function parseIterationLimit(
  ev: SwarmEvent,
): { limit: number; extendBy: number } | undefined {
  let raw: unknown
  try {
    raw = JSON.parse(ev.text ?? "")
  } catch {
    return undefined
  }
  if (!raw || typeof raw !== "object") return undefined
  const body = raw as { limit?: unknown; extend_by?: unknown }
  return { limit: num(body.limit), extendBy: num(body.extend_by) }
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
