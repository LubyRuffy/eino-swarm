import type { ThreadDetail } from "./rpc"

export type GoalState = "pursuing" | "done" | "blocked" | "paused"

export function goalState(d: {
  goal?: string
  goal_complete?: boolean
  goal_blocked?: boolean
  goal_capped?: boolean
  goal_idle?: boolean
}): GoalState | undefined {
  if (!(d.goal ?? "").trim()) return undefined
  if (d.goal_complete) return "done"
  if (d.goal_blocked) return "blocked"
  if (d.goal_capped || d.goal_idle) return "paused"
  return "pursuing"
}

/** Elapsed since the objective was set, Codex-style `1d 10h 39m 16s`. */
export function formatGoalAge(from: Date, now = new Date()): string {
  let s = Math.max(0, Math.floor((now.getTime() - from.getTime()) / 1000))
  const d = Math.floor(s / 86400)
  s %= 86400
  const h = Math.floor(s / 3600)
  s %= 3600
  const m = Math.floor(s / 60)
  s %= 60
  const parts: string[] = []
  if (d) parts.push(`${d}d`)
  if (h || d) parts.push(`${h}h`)
  if (m || h || d) parts.push(`${m}m`)
  parts.push(`${s}s`)
  return parts.join(" ")
}

export function parseGoalReason(text?: string): string {
  const raw = text?.trim() ?? ""
  if (!raw) return ""
  try {
    const v = JSON.parse(raw) as { reason?: unknown }
    return typeof v.reason === "string" ? v.reason : ""
  } catch {
    return raw
  }
}

export function applyGoalDetail(
  detail: ThreadDetail,
  ev: { kind: string; text?: string; created_at?: string },
): ThreadDetail {
  switch (ev.kind) {
    case "goal":
      return {
        ...detail,
        goal: ev.text ?? "",
        goal_on: Boolean((ev.text ?? "").trim()),
        goal_complete: false,
        goal_blocked: false,
        goal_block_reason: "",
        goal_capped: false,
        goal_idle: false,
        goal_started_at: ev.created_at || detail.goal_started_at,
      }
    case "goal_edited":
      return { ...detail, goal: ev.text ?? detail.goal, goal_on: Boolean((ev.text ?? detail.goal ?? "").trim()) }
    case "goal_complete":
      return {
        ...detail,
        goal_on: Boolean((detail.goal ?? "").trim()),
        goal_complete: true,
        goal_blocked: false,
        goal_block_reason: "",
        goal_capped: false,
        goal_idle: false,
      }
    case "goal_blocked":
      return {
        ...detail,
        goal_on: Boolean((detail.goal ?? "").trim()),
        goal_blocked: true,
        goal_capped: false,
        goal_idle: false,
        goal_block_reason: parseGoalReason(ev.text),
      }
    case "goal_capped":
      return { ...detail, goal_capped: true }
    case "goal_idle":
      return { ...detail, goal_idle: true }
    case "goal_resumed":
      return {
        ...detail,
        goal_on: Boolean((detail.goal ?? "").trim()),
        goal_complete: false,
        goal_blocked: false,
        goal_block_reason: "",
        goal_capped: false,
        goal_idle: false,
      }
    case "goal_continued":
      return { ...detail, goal_idle: false, goal_on: Boolean((detail.goal ?? "").trim()) }
    default:
      return detail
  }
}
