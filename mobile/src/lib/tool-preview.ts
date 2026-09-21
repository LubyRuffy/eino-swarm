/** One-line tool preview. Packed JSON is not a summary — pull a human
 *  field instead of dumping the envelope the way a raw notice would. */

export type RosterCounts = {
  done: number
  failed: number
  running: number
  undelivered: number
}

const PREFERRED = [
  "findings",
  "command",
  "query",
  "url",
  "file_path",
  "path",
  "pattern",
  "text",
]

const SKIP = new Set([
  "id",
  "ok",
  "quiet",
  "keep",
  "kind",
  "thread_id",
  "origin_thread",
  "origin_thread_id",
  "next_in_s",
  "delay_s",
  "every_s",
  "cron",
  "status",
  "created_at",
  "prompt",
])

export function jsonPreview(raw?: string): string {
  const obj = parseObject(raw)
  if (!obj) return ""
  for (const key of PREFERRED) {
    const v = asText(obj[key])
    if (v) return flatten(v)
  }
  for (const [k, v] of Object.entries(obj)) {
    if (SKIP.has(k)) continue
    const t = asText(v)
    if (t) return flatten(t)
  }
  return ""
}

export function looksPacked(s: string): boolean {
  const t = s.trim()
  return t.startsWith("{") || t.startsWith("[")
}

/** A pulse / wait_agents report is a roster, not a findings string. */
export function rosterCounts(raw?: string): RosterCounts | null {
  const agents = parseAgents(raw)
  if (!agents.length) return null
  const counts: RosterCounts = { done: 0, failed: 0, running: 0, undelivered: 0 }
  for (const row of agents) {
    if (row.status === "failed") counts.failed++
    else if (row.status === "running") counts.running++
    else if (row.status === "undelivered") counts.undelivered++
    else counts.done++
  }
  return counts
}

/** Compact expand body: role + status, never agent_id / elapsed_ms. */
export function rosterLines(raw?: string, limit = 8): string[] {
  const agents = parseAgents(raw)
  if (!agents.length) return []
  const rows = agents.map((row) => {
    const role = row.role.trim() || "agent"
    return `${role} ${row.status}`
  })
  if (rows.length <= limit) return rows
  return [...rows.slice(0, limit), "…"]
}

/** Drop a trailing wait-agents roster so markdown does not paint it.
 *  Arbitrary JSON in a user bubble stays; only a roster envelope is quiet. */
export function dropPackedJson(s: string): string {
  const t = s.trim()
  if (!t) return ""
  if (parseJSON(t) !== undefined) return rosterCounts(t) ? "" : t
  const idx = trailingPacked(t)
  if (idx <= 0) return t
  const tail = t.slice(idx).trim()
  if (!rosterCounts(tail) && !t.slice(0, idx).includes("\n")) {
    return t
  }
  return t.slice(0, idx).trim()
}

function trailingPacked(s: string): number {
  for (let i = 0; i < s.length; i++) {
    const c = s[i]
    if (c !== "{" && c !== "[") continue
    if (parseJSON(s.slice(i)) !== undefined) return i
  }
  return -1
}

export function flatten(s: string): string {
  return s.replace(/\s+/g, " ").trim()
}

function parseObject(raw?: string): Record<string, unknown> | null {
  const t = raw?.trim() ?? ""
  if (!t.startsWith("{")) return null
  try {
    const v = JSON.parse(t) as unknown
    if (!v || typeof v !== "object" || Array.isArray(v)) return null
    return v as Record<string, unknown>
  } catch {
    return null
  }
}

function asText(v: unknown): string {
  return typeof v === "string" ? v.trim() : ""
}

function parseJSON(raw: string): unknown {
  try {
    return JSON.parse(raw)
  } catch {
    return undefined
  }
}

function parseAgents(raw?: string): { role: string; status: string }[] {
  const value = parseJSON(raw?.trim() ?? "")
  const list = agentList(value)
  if (!list) return []
  const out: { role: string; status: string }[] = []
  for (const item of list) {
    if (!item || typeof item !== "object" || Array.isArray(item)) continue
    const row = item as { role?: unknown; status?: unknown; agent_id?: unknown }
    const status = typeof row.status === "string" ? row.status.trim() : ""
    if (!status) continue
    const role = typeof row.role === "string" ? row.role : ""
    out.push({ role, status })
  }
  return out
}

function agentList(value: unknown): unknown[] | null {
  if (Array.isArray(value)) return value
  if (!value || typeof value !== "object") return null
  const agents = (value as { agents?: unknown }).agents
  return Array.isArray(agents) ? agents : null
}
