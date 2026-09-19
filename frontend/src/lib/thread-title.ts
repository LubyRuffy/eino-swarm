import type { Thread } from "@/lib/types"

/** A `done` refresh can race the namer: the list still has the placeholder
 *  (`title_auto`) after a `title` event already named the row. Keep the
 *  generated name until the API catches up. */
export function preferNamedTitles(local: Thread[], incoming: Thread[]): Thread[] {
  const named = new Map(
    local.filter((t) => t.title_auto === false).map((t) => [t.id, t.title]),
  )
  return incoming.map((t) => {
    const keep = named.get(t.id)
    if (keep && t.title_auto) {
      return { ...t, title: keep, title_auto: false }
    }
    return t
  })
}

/** Reconcile a listing with the sidebar. A fetch that raced a live overlay
 *  would otherwise idle a folder that is still working. `setThreadRunning(false)`
 *  (stream done, or opening that conversation idle) is what clears it. */
export function mergeThreadList(local: Thread[], incoming: Thread[]): Thread[] {
  const named = preferNamedTitles(local, incoming)
  const prev = new Map(local.map((t) => [t.id, t]))
  return named.map((t) => {
    const was = prev.get(t.id)
    if (was?.running && !t.running) return { ...t, running: true }
    return t
  })
}

/** Opening a minted findings conversation must put it in Recents even when
 *  the list was fetched before that fire existed. */
export function upsertThread(threads: Thread[], thread: Thread): Thread[] {
  const i = threads.findIndex((t) => t.id === thread.id)
  if (i === -1) return [thread, ...threads]
  const next = threads.slice()
  next[i] = preferNamedTitles([next[i]], [thread])[0] ?? thread
  return next
}

/** Sidebar progress is `thread.running`. The live header only knows the
 *  open conversation, so a turn that starts — or is still going when we
 *  leave — has to be stamped here or the folder looks idle until you
 *  click back in. */
export function setThreadRunning(
  threads: Thread[],
  id: string,
  running: boolean,
): Thread[] {
  let changed = false
  const next = threads.map((thread) => {
    if (thread.id !== id || thread.running === running) return thread
    changed = true
    return { ...thread, running }
  })
  return changed ? next : threads
}
