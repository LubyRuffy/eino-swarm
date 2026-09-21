/** One-line tool preview. Packed JSON is not a summary — pull a human
 *  field instead of dumping the envelope the way a raw notice would. */

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
