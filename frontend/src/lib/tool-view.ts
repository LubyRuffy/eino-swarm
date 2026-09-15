/** How a built-in tool call should read on one line, and how its payload
 *  should look once expanded. Field names come from the tool schemas — not
 *  from anyone's example query. */

const PRIMARY: Record<string, string[]> = {
  exec: ["command"],
  web_search: ["query"],
  web_fetch: ["url"],
  read: ["file_path"],
  write: ["file_path"],
  edit: ["file_path"],
  ls: ["path"],
  tree: ["path"],
  glob: ["pattern", "path"],
  grep: ["pattern", "path"],
  python_runner: ["code"],
  screenshot: ["path"],
  spawn_agent: ["role"],
  send_message: ["text"],
  resume_agent: ["agent_id"],
  wait_agents: [],
  close_agent: ["agent_id"],
  memory: ["action", "content", "old_text"],
  skill_view: ["name"],
  skill_manage: ["action", "name"],
}

export type SearchHit = { title: string; url: string; snippet: string }

export type ToolView = {
  summary: string
  failed: boolean
  error?: string
  body: string
  hits?: SearchHit[]
}

export function summariseToolCall(name: string, args: string): string {
  const obj = parseObject(args)
  if (name === "memory" || name === "skill_manage" || name === "skill_view") {
    return summariseMemoryCall(name, obj, args)
  }
  if (!obj) return flatten(args)
  const keys = PRIMARY[name]
  if (keys) {
    const parts = keys.map((k) => asText(obj[k])).filter(Boolean)
    if (parts.length) return parts.join(" · ")
    return ""
  }
  for (const k of ["command", "query", "url", "file_path", "path", "pattern", "code", "text"]) {
    const v = asText(obj[k])
    if (v) return v
  }
  for (const v of Object.values(obj)) {
    const t = asText(v)
    if (t) return t
  }
  return flatten(args)
}

export function viewTool(name: string, args: string, result?: string, flagged?: boolean): ToolView {
  const summary = summariseToolCall(name, args)
  if (result === undefined) {
    return { summary, failed: Boolean(flagged), body: "" }
  }
  const prefix = errorPrefix(result)
  if (name === "exec" || name === "python_runner") {
    const run = parseRunResult(result)
    if (run) {
      const failed = Boolean(flagged || run.failed || (run.exitCode !== 0 && run.exitCode !== undefined))
      const body = [run.stdout, run.stderr].filter((s) => s.trim()).join("\n")
      const error = run.error || (failed ? `exit ${run.exitCode ?? "?"}` : undefined)
      return { summary, failed, error, body }
    }
  }
  if (name === "web_search") {
    const hits = parseSearchHits(result)
    if (hits) {
      return { summary, failed: Boolean(flagged || prefix), error: prefix, body: "", hits }
    }
  }
  const failed = Boolean(flagged || prefix)
  if (name === "memory" || name === "skill_manage" || name === "skill_view") {
    const mem = parseMemoryResult(result)
    if (mem) {
      return {
        summary,
        failed: Boolean(flagged || !mem.success),
        error: mem.error || prefix,
        body: mem.body,
      }
    }
  }
  return { summary, failed, error: prefix, body: result }
}

function summariseMemoryCall(
  name: string,
  obj: Record<string, unknown> | undefined,
  raw: string,
): string {
  if (!obj) return flatten(raw)
  if (name === "skill_view") return asText(obj.name)
  const action = asText(obj.action)
  if (name === "skill_manage") {
    return [action, asText(obj.name)].filter(Boolean).join(" · ")
  }
  const preview = asText(obj.content) || asText(obj.old_text)
  return [action, preview].filter(Boolean).join(" · ")
}

function parseMemoryResult(raw: string): { success: boolean; error?: string; body: string } | undefined {
  const obj = parseObject(raw)
  if (!obj || typeof obj.success !== "boolean") return undefined
  if (obj.success === false) {
    return { success: false, error: asText(obj.error) || "refused", body: memoryFailureBody(obj) }
  }
  const parts: string[] = []
  const usage = asText(obj.usage)
  if (usage) parts.push(usage)
  if (Array.isArray(obj.entries)) {
    for (const e of obj.entries) {
      const t = asText(e)
      if (t) parts.push(t)
    }
  }
  const name = asText(obj.name)
  if (name) parts.push(name)
  const body = asText(obj.body)
  if (body) parts.push(body)
  return { success: true, body: parts.join("\n") }
}

function memoryFailureBody(obj: Record<string, unknown>): string {
  const entries = Array.isArray(obj.current_entries)
    ? obj.current_entries.map(asText).filter(Boolean)
    : []
  return entries.length ? entries.join("\n") : asText(obj.error)
}

function parseRunResult(raw: string): {
  stdout: string
  stderr: string
  failed: boolean
  exitCode?: number
  error?: string
} | undefined {
  const obj = parseObject(raw)
  if (!obj) return undefined
  if (!("stdout" in obj) && !("stderr" in obj) && !("exit_code" in obj) && !("failed" in obj)) {
    return undefined
  }
  const exitCode = asNumber(obj.exit_code)
  return {
    stdout: asText(obj.stdout),
    stderr: asText(obj.stderr),
    failed: obj.failed === true,
    exitCode,
    error: asText(obj.error) || undefined,
  }
}

function parseSearchHits(raw: string): SearchHit[] | undefined {
  const val = parseJSON(raw)
  if (val === undefined) return undefined
  const rows = Array.isArray(val)
    ? val
    : isRecord(val)
      ? arrayOf(val.results) ?? arrayOf(val.items) ?? arrayOf(val.data)
      : undefined
  if (!rows) return undefined
  const hits: SearchHit[] = []
  for (const row of rows) {
    if (!isRecord(row)) continue
    const title = asText(row.title) || asText(row.name)
    const url = asText(row.url) || asText(row.link) || asText(row.href)
    const snippet =
      asText(row.snippet) ||
      asText(row.summary) ||
      asText(row.body) ||
      asText(row.content) ||
      asText(row.description)
    if (!title && !url && !snippet) continue
    hits.push({ title: title || url, url, snippet })
  }
  return hits.length ? hits : undefined
}

function errorPrefix(raw: string): string | undefined {
  const t = raw.trim()
  if (/^error:/i.test(t)) return t.replace(/^error:\s*/i, "")
  return undefined
}

function parseObject(raw: string): Record<string, unknown> | undefined {
  const v = parseJSON(raw)
  return isRecord(v) ? v : undefined
}

function parseJSON(raw: string): unknown {
  try {
    return JSON.parse(raw)
  } catch {
    return undefined
  }
}

function arrayOf(v: unknown): unknown[] | undefined {
  return Array.isArray(v) ? v : undefined
}

function isRecord(v: unknown): v is Record<string, unknown> {
  return Boolean(v) && typeof v === "object" && !Array.isArray(v)
}

function asText(v: unknown): string {
  if (typeof v === "string") return v.trim()
  if (typeof v === "number" || typeof v === "boolean") return String(v)
  return ""
}

function asNumber(v: unknown): number | undefined {
  if (typeof v === "number" && Number.isFinite(v)) return v
  if (typeof v === "string" && v.trim() && Number.isFinite(Number(v))) return Number(v)
  return undefined
}

function flatten(s: string): string {
  return s.replace(/\s+/g, " ").trim()
}

/** Tools whose successful result means the project's files on disk changed. */
export const MEMORY_WRITE_TOOLS = new Set(["memory", "skill_manage"])

/** A memory/skill tool result that actually wrote something. A duplicate add
 *  reports success with changed: false — that is not a write. */
export function memoryWriteLanded(result?: string): boolean {
  const obj = parseObject(result ?? "")
  if (!obj || obj.success !== true) return false
  return obj.changed !== false
}
