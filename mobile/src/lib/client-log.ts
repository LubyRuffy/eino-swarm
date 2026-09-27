import type { ClientEntry } from "@/lib/rpc"

export type ClientPage = {
  entries: ClientEntry[]
  older?: boolean
  before?: number
}

/** expanded means Earlier was used, so a tail poll must not drop that page. */
export type ClientLog = {
  opening: ClientEntry[]
  body: ClientEntry[]
  older: boolean
  before: number
  expanded: boolean
}

function splitOpening(entries: ClientEntry[]): { opening: ClientEntry[]; rest: ClientEntry[] } {
  let end = 0
  while (end < entries.length && entries[end]?.role === "user") end++
  if (end === 0) return { opening: [], rest: entries }
  return { opening: entries.slice(0, end), rest: entries.slice(end) }
}

function entryKey(entry: ClientEntry): string {
  if (typeof entry.at !== "number") return ""
  return `${entry.at}:${entry.n ?? 0}`
}

export function applyClientPage(
  prev: ClientLog | null,
  page: ClientPage,
  kind: "tail" | "older",
): ClientLog {
  const { opening, rest } = splitOpening(page.entries ?? [])
  if (kind === "older") {
    return {
      opening: opening.length > 0 ? opening : (prev?.opening ?? []),
      body: mergeEntries(prev?.body ?? [], rest),
      older: Boolean(page.older),
      before: page.before ?? 0,
      expanded: true,
    }
  }
  if (prev?.expanded) {
    return {
      opening: opening.length > 0 ? opening : prev.opening,
      body: mergeEntries(prev.body, rest),
      older: prev.older,
      before: prev.before,
      expanded: true,
    }
  }
  return {
    opening,
    body: rest,
    older: Boolean(page.older),
    before: page.before ?? 0,
    expanded: false,
  }
}

function mergeEntries(current: ClientEntry[], incoming: ClientEntry[]): ClientEntry[] {
  const map = new Map<string, ClientEntry>()
  for (const entry of current) {
    const key = entryKey(entry)
    if (key) map.set(key, entry)
  }
  for (const entry of incoming) {
    const key = entryKey(entry)
    if (key) map.set(key, entry)
  }
  if (map.size === 0) return incoming.length > 0 ? incoming : current
  return [...map.values()].sort((a, b) => (a.at ?? 0) - (b.at ?? 0) || (a.n ?? 0) - (b.n ?? 0))
}
