import type { SwarmEvent, Thread } from "@/lib/types"

/** Fold a standing-objective event into the sidebar/banner thread flags.
 *  Status clocks stay in the store: a `goal_continued` that only flips
 *  `goal_idle` would still freeze the header at 1s. */
export function applyGoalThreadFlags(
  threads: Thread[],
  threadId: string,
  ev: SwarmEvent,
): Thread[] {
  switch (ev.kind) {
    case "goal":
      return threads.map((t) =>
        t.id === threadId
          ? {
              ...t,
              goal: ev.text ?? "",
              goal_complete: false,
              goal_capped: false,
              goal_idle: false,
              goal_blocked: false,
              goal_block_reason: "",
            }
          : t,
      )
    case "goal_edited":
      return threads.map((t) =>
        t.id === threadId ? { ...t, goal: ev.text ?? "" } : t,
      )
    case "goal_complete":
      return threads.map((t) =>
        t.id === threadId
          ? {
              ...t,
              goal_complete: true,
              goal_capped: false,
              goal_idle: false,
              goal_blocked: false,
              goal_block_reason: "",
            }
          : t,
      )
    case "goal_capped":
      return threads.map((t) =>
        t.id === threadId ? { ...t, goal_capped: true } : t,
      )
    case "goal_idle":
      return threads.map((t) =>
        t.id === threadId ? { ...t, goal_idle: true } : t,
      )
    case "goal_blocked":
      return threads.map((t) =>
        t.id === threadId
          ? {
              ...t,
              goal_blocked: true,
              goal_capped: false,
              goal_idle: false,
              goal_block_reason: parseGoalReason(ev.text),
            }
          : t,
      )
    case "goal_resumed":
      return threads.map((t) =>
        t.id === threadId
          ? {
              ...t,
              goal_complete: false,
              goal_blocked: false,
              goal_capped: false,
              goal_idle: false,
              goal_block_reason: "",
            }
          : t,
      )
    case "goal_continued":
      return threads.map((t) =>
        t.id === threadId ? { ...t, goal_idle: false } : t,
      )
    default:
      return threads
  }
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
