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
