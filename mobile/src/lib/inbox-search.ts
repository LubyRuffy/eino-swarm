import { inboxPreview } from "./inbox-preview"
import type { RunningView, ThreadView } from "./rpc"

/** The inbox filter runs on the roster the phone already holds: a keystroke
 *  over a relay would arrive after the next one. Rows are matched on what
 *  they paint — the preview, not the raw summary — so a query can never hit
 *  tool JSON the screen hides. */

function needle(query: string): string {
  return query.trim().toLowerCase()
}

function hit(q: string, fields: (string | undefined)[]): boolean {
  return fields.some((f) => (f ?? "").toLowerCase().includes(q))
}

export function searchThreads(threads: ThreadView[], query: string): ThreadView[] {
  const q = needle(query)
  if (!q) return threads
  return threads.filter((th) => hit(q, [th.title, th.id, inboxPreview(th.summary)]))
}

export function searchRunning(running: RunningView[], query: string): RunningView[] {
  const q = needle(query)
  if (!q) return running
  return running.filter((r) => hit(q, [r.title, r.thread_id, inboxPreview(r.action)]))
}
