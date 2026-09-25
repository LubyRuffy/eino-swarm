import type { ClientEntry } from "./local-clients"
import type { TranscriptModePref } from "./appearance"

export type ClientRow =
  | { type: "entry"; entry: ClientEntry; index: number }
  | { type: "work"; entries: ClientEntry[]; index: number }

function foldable(role: string): boolean {
  return role === "thinking" || role === "tool"
}

/** User mode folds consecutive thinking and tool lines. A reply stays
 *  visible and splits the group, same as the conversation work fold. */
export function foldClientEntries(
  entries: ClientEntry[],
  mode: TranscriptModePref,
): ClientRow[] {
  if (mode === "developer") {
    return entries.map((entry, index) => ({ type: "entry", entry, index }))
  }
  const rows: ClientRow[] = []
  let group: ClientEntry[] = []
  let groupAt = 0
  const flush = () => {
    if (group.length === 0) return
    rows.push({ type: "work", entries: group, index: groupAt })
    group = []
  }
  entries.forEach((entry, index) => {
    if (foldable(entry.role)) {
      if (group.length === 0) groupAt = index
      group.push(entry)
      return
    }
    flush()
    rows.push({ type: "entry", entry, index })
  })
  flush()
  return rows
}

/** The opening request stays at the top of the column. Later user lines
 *  stay in the scroll with the work they belong to. */
export function splitOpeningRequest(entries: ClientEntry[]): {
  opening: ClientEntry[]
  rest: ClientEntry[]
} {
  let end = 0
  while (end < entries.length && entries[end].role === "user") end++
  if (end === 0) return { opening: [], rest: entries }
  return { opening: entries.slice(0, end), rest: entries.slice(end) }
}

export function clientWorkCounts(entries: ClientEntry[]): { thoughts: number; tools: number } {
  let thoughts = 0
  let tools = 0
  for (const e of entries) {
    if (e.role === "thinking") thoughts++
    if (e.role === "tool") tools++
  }
  return { thoughts, tools }
}
