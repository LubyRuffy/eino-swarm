import type { SwarmEvent, Thread } from "@/lib/types"

/** Fold plan-mode events into the thread the banner reads. */
export function applyPlanThreadFlags(
  threads: Thread[],
  threadId: string,
  ev: SwarmEvent,
): Thread[] {
  switch (ev.kind) {
    case "plan":
      return threads.map((t) =>
        t.id === threadId ? { ...t, plan_mode: true } : t,
      )
    case "plan_updated":
      return threads.map((t) =>
        t.id === threadId
          ? { ...t, plan_mode: true, plan_markdown: ev.text ?? t.plan_markdown }
          : t,
      )
    case "plan_implemented":
      return threads.map((t) =>
        t.id === threadId ? { ...t, plan_mode: false } : t,
      )
    case "plan_cancelled":
      return threads.map((t) =>
        t.id === threadId ? { ...t, plan_mode: false } : t,
      )
    default:
      return threads
  }
}
