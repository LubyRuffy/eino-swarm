/** Splice one row to a new index. Same from and to is a no-op so a drop
 *  on the row you picked up does not rewrite ranks. */
export function moveItem<T>(items: T[], from: number, to: number): T[] {
  if (
    from === to ||
    from < 0 ||
    to < 0 ||
    from >= items.length ||
    to >= items.length
  ) {
    return items
  }
  const next = items.slice()
  const [item] = next.splice(from, 1)
  next.splice(to, 0, item)
  return next
}

/** Move the row with fromId to the index currently occupied by toId. */
export function reorderById<T extends { id: string }>(
  items: T[],
  fromId: string,
  toId: string,
): T[] {
  return moveItem(
    items,
    items.findIndex((item) => item.id === fromId),
    items.findIndex((item) => item.id === toId),
  )
}

/** Matches the server's rankStep: first dragged row is 1000, not 0.
 *  Zero stays the "never dragged" sentinel so recency still applies. */
export const RANK_STEP = 1000

export function applyPinnedOrder<T extends { id: string; sort_rank?: number }>(
  items: T[],
  ids: string[],
): T[] {
  const byId = new Map(items.map((item) => [item.id, item]))
  const next: T[] = []
  const seen = new Set<string>()
  ids.forEach((id, i) => {
    const item = byId.get(id)
    if (!item) return
    next.push({ ...item, sort_rank: (i + 1) * RANK_STEP })
    seen.add(id)
  })
  for (const item of items) {
    if (!seen.has(item.id)) next.push(item)
  }
  return next
}
