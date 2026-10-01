import { agentRosterText } from "@/lib/agent-label"
import type { AgentState } from "@/lib/transcript"

/** What the Agents list can be ordered by. `updated` is the last moment
 *  this worker did something; a resume keeps the original `startedAt`. */
export type AgentSortKey = "created" | "updated" | "name"

export type AgentSortDir = "asc" | "desc"

export interface AgentRosterSort {
  key: AgentSortKey
  dir: AgentSortDir
}

/** Newest activity first. A long roster is read from the top, and the
 *  worker that just moved is the one you came to find. */
export const defaultAgentRosterSort: AgentRosterSort = {
  key: "updated",
  dir: "desc",
}

export function isAgentSortKey(value: string): value is AgentSortKey {
  return value === "created" || value === "updated" || value === "name"
}

/** Order ids inside one roster group. Equal keys keep the incoming order
 *  so a tie does not shuffle while events stream. A worker with no clock
 *  stays last in either direction: missing is not "oldest" or "newest". */
export function sortAgentIds(
  ids: readonly string[],
  agents: Record<string, AgentState>,
  sort: AgentRosterSort = defaultAgentRosterSort,
  locale = "en",
): string[] {
  const rows = ids.map((id, index) => ({ id, index, agent: agents[id] }))
  rows.sort((a, b) => {
    if (!a.agent && !b.agent) return a.index - b.index
    if (!a.agent) return 1
    if (!b.agent) return -1
    const cmp = compareAgents(a.agent, b.agent, sort, locale)
    if (cmp !== 0) return cmp
    return a.index - b.index
  })
  return rows.map((row) => row.id)
}

function compareAgents(
  a: AgentState,
  b: AgentState,
  sort: AgentRosterSort,
  locale: string,
): number {
  if (sort.key === "name") {
    const cmp = compareNames(a, b, locale)
    return sort.dir === "asc" ? cmp : -cmp
  }
  const left = sort.key === "created" ? createdMs(a) : updatedMs(a)
  const right = sort.key === "created" ? createdMs(b) : updatedMs(b)
  if (left === undefined && right === undefined) return 0
  if (left === undefined) return 1
  if (right === undefined) return -1
  const cmp = left - right
  return sort.dir === "asc" ? cmp : -cmp
}

function compareNames(a: AgentState, b: AgentState, locale: string): number {
  const opts: Intl.CollatorOptions = { numeric: true, sensitivity: "base" }
  const byLabel = agentRosterText(a.role, a.id).localeCompare(
    agentRosterText(b.role, b.id),
    locale,
    opts,
  )
  if (byLabel !== 0) return byLabel
  return a.id.localeCompare(b.id, locale, opts)
}

function createdMs(agent: AgentState): number | undefined {
  const started = ms(agent.startedAt)
  if (started !== undefined) return started
  const blocks = agent.blocks.map((block) => ms(block.at)).filter(isTime)
  const earliest = minOf(blocks)
  if (earliest !== undefined) return earliest
  return ms(agent.endedAt)
}

function updatedMs(agent: AgentState): number | undefined {
  return maxOf(
    [ms(agent.startedAt), ms(agent.endedAt), ...agent.blocks.map((block) => ms(block.at))].filter(
      isTime,
    ),
  )
}

function ms(iso?: string): number | undefined {
  if (!iso) return undefined
  const n = Date.parse(iso)
  return Number.isFinite(n) ? n : undefined
}

function isTime(n: number | undefined): n is number {
  return n !== undefined
}

function minOf(values: number[]): number | undefined {
  let best: number | undefined
  for (const n of values) if (best === undefined || n < best) best = n
  return best
}

function maxOf(values: number[]): number | undefined {
  let best: number | undefined
  for (const n of values) if (best === undefined || n > best) best = n
  return best
}
