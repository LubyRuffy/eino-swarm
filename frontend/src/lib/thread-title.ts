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

/** Opening a minted findings conversation must put it in Recents even when
 *  the list was fetched before that fire existed. */
export function upsertThread(threads: Thread[], thread: Thread): Thread[] {
  const i = threads.findIndex((t) => t.id === thread.id)
  if (i === -1) return [thread, ...threads]
  const next = threads.slice()
  next[i] = preferNamedTitles([next[i]], [thread])[0] ?? thread
  return next
}
