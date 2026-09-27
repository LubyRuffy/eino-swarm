import { splitOpeningRequest } from "@/lib/client-fold"
import type { ClientEntry } from "@/lib/local-clients"

export type ClientPage = {
  entries: ClientEntry[]
  older?: boolean
  before?: number
}

/** What is on screen. expanded means the reader already asked for a page
 *  above the live tail, so a later tail poll must not throw that page away. */
export type ClientLog = {
  opening: ClientEntry[]
  body: ClientEntry[]
  older: boolean
  before: number
  expanded: boolean
}

export function clientEntryKey(entry: ClientEntry): string {
  if (typeof entry.at !== "number") return ""
  return `${entry.at}:${entry.n ?? 0}`
}

export function applyClientPage(
  prev: ClientLog | null,
  page: ClientPage,
  kind: "tail" | "older",
): ClientLog {
  const { opening, rest } = splitOpeningRequest(page.entries ?? [])
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
    const key = clientEntryKey(entry)
    if (key) map.set(key, entry)
  }
  for (const entry of incoming) {
    const key = clientEntryKey(entry)
    if (key) map.set(key, entry)
  }
  if (map.size === 0) return incoming.length > 0 ? incoming : current
  return [...map.values()].sort((a, b) => (a.at ?? 0) - (b.at ?? 0) || (a.n ?? 0) - (b.n ?? 0))
}
