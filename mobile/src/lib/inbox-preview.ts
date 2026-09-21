import { splitToolCall } from "./ask"
import { flatten, jsonPreview, looksPacked } from "./tool-preview"

/** Inbox subtitles are human text. Tool names and packed envelopes stay
 *  in Trace; a phone row is findings, a command, or nothing so chrome can
 *  say Waiting. */

const SCHEDULED_CHECK = "This turn is a scheduled check."

const BOOKKEEPING = new Set([
  "schedule_wake",
  "schedule_task",
  "cancel_schedule",
  "memory",
  "skill_manage",
  "skill_view",
  "close_agent",
  "wait_agents",
  "ask_user",
  "spawn_agent",
  "resume_agent",
])

const SCHEDULE_TOOLS = new Set(["schedule_wake", "schedule_task", "cancel_schedule", "report_schedule"])

export function inboxPreview(raw?: string): string {
  const s = (raw ?? "").trim()
  if (!s) return ""
  if (s.startsWith(SCHEDULED_CHECK)) return ""
  if (looksToolCall(s)) return toolPreview(s)
  if (looksPacked(s)) return jsonPreview(s)
  return flatten(s)
}

/** undefined: not a schedule tool. Empty string: hide. Otherwise a notice. */
export function scheduleToolNotice(name: string, args: string): string | undefined {
  if (!SCHEDULE_TOOLS.has(name)) return undefined
  if (name === "report_schedule") return jsonPreview(args)
  return ""
}

function looksToolCall(s: string): boolean {
  const open = s.indexOf("(")
  if (open <= 0) return false
  const name = s.slice(0, open).trim()
  return /^[a-z_][A-Za-z0-9_]*$/.test(name)
}

function toolPreview(raw: string): string {
  const { name, args } = splitToolCall(raw)
  if (name === "report_schedule") return jsonPreview(args)
  if (BOOKKEEPING.has(name)) return ""
  if (!args.trim()) return ""
  const preview = jsonPreview(args)
  if (preview) return preview
  if (looksPacked(args)) return ""
  return flatten(args)
}
