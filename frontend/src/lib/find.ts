import type { Block } from "./transcript"

/** CSS Custom Highlight names. The stylesheet keys off these; renaming one
 *  without the CSS is a find bar that reports hits and paints nothing. */
export const FIND_HIGHLIGHT = "zwai-find"
export const FIND_HIGHLIGHT_CURRENT = "zwai-find-current"

/** Walkers skip this subtree so the find bar, clocks and duplicated marquee
 *  copies cannot inflate the hit count. */
export const FIND_IGNORE_ATTR = "data-find-ignore"

/** One character matches almost every token in a tool dump. Expanding those
 *  rows puts megabytes into the DOM and freezes a live turn. */
export const FIND_REVEAL_MIN = 2

/** CSS Highlights with thousands of Ranges (and `Highlight(...spread)`)
 *  stalls the webview. Count every hit; paint only a window around the current
 *  one. */
export const FIND_PAINT_CAP = 96

/** Streamed tokens replace text nodes. Re-painting find on every delta is a
 *  layout storm; wait until the stream pauses. */
export const FIND_REPAINT_MS = 120

export interface FindMatch {
  start: number
  end: number
}

export type FindShortcut =
  | { action: "open" }
  | { action: "select-query" }
  | { action: "next"; delta: number }
  | { action: "close" }

/** Case-insensitive, non-overlapping. Overlapping would count "aa" in "aaa"
 *  twice and the highlight would sit on itself. */
export function findMatches(haystack: string, query: string): FindMatch[] {
  return collectWindow(haystack, query, 0, Number.POSITIVE_INFINITY)
}

/** Same walk as findMatches, but does not allocate a Range-sized array.
 *  The live count can be 10k+; the highlight list must not. */
export function countMatches(haystack: string, query: string): number {
  const needle = query.trim()
  if (!needle) return 0
  const hay = haystack.toLowerCase()
  const q = needle.toLowerCase()
  let n = 0
  let from = 0
  while (from <= hay.length - q.length) {
    const i = hay.indexOf(q, from)
    if (i < 0) break
    n++
    from = i + q.length
  }
  return n
}

export function paintWindow(
  index: number,
  total: number,
  cap = FIND_PAINT_CAP,
): { start: number; end: number } {
  if (total <= 0) return { start: 0, end: 0 }
  const width = Math.max(1, cap)
  if (total <= width) return { start: 0, end: total }
  const half = Math.floor(width / 2)
  const start = Math.min(Math.max(0, index - half), total - width)
  return { start, end: start + width }
}

export function findMatchWindow(
  haystack: string,
  query: string,
  index: number,
  cap = FIND_PAINT_CAP,
): { total: number; matches: FindMatch[]; current: number } {
  const total = countMatches(haystack, query)
  if (total === 0) return { total: 0, matches: [], current: -1 }
  const at = clampMatch(index, total)
  const { start, end } = paintWindow(at, total, cap)
  return { total, matches: collectWindow(haystack, query, start, end), current: at - start }
}

function collectWindow(
  haystack: string,
  query: string,
  fromIndex: number,
  untilIndex: number,
): FindMatch[] {
  const needle = query.trim()
  if (!needle || untilIndex <= fromIndex) return []
  const hay = haystack.toLowerCase()
  const q = needle.toLowerCase()
  const matches: FindMatch[] = []
  let n = 0
  let from = 0
  while (from <= hay.length - q.length && n < untilIndex) {
    const i = hay.indexOf(q, from)
    if (i < 0) break
    if (n >= fromIndex) matches.push({ start: i, end: i + q.length })
    n++
    from = i + q.length
  }
  return matches
}

export function stepMatch(index: number, total: number, delta: number): number {
  if (total <= 0) return 0
  return ((index + delta) % total + total) % total
}

export function clampMatch(index: number, total: number): number {
  if (total <= 0) return 0
  if (index < 0) return 0
  if (index >= total) return total - 1
  return index
}

/** Codex-style "1 / 8 results". Empty query is nothing to announce. */
export function findCountLabel(index: number, total: number, query: string): string {
  if (!query.trim()) return ""
  if (total <= 0) return "No results"
  return `${index + 1} / ${total} results`
}

export function seedQuery(selected: string, current: string): string {
  const line = selected.trim().split("\n", 1)[0]?.trim() ?? ""
  if (!line || line.length > 200) return current
  return line
}

/** ⌘F must be classified here so the window handler can preventDefault before
 *  the webview's own find bar eats it. Dialogs are the caller's problem. */
export function findShortcut(
  e: Pick<KeyboardEvent, "key" | "metaKey" | "ctrlKey" | "shiftKey" | "altKey">,
  open: boolean,
): FindShortcut | undefined {
  if (e.altKey) return undefined
  const mod = e.metaKey || e.ctrlKey
  const key = e.key.length === 1 ? e.key.toLowerCase() : e.key

  if (key === "Escape" && open) return { action: "close" }

  if (mod && key === "f" && !e.shiftKey) {
    return open ? { action: "select-query" } : { action: "open" }
  }

  if (!open) return undefined

  if (key === "F3" || (mod && key === "g")) {
    return { action: "next", delta: e.shiftKey ? -1 : 1 }
  }
  return undefined
}

/** Body text that is not on screen until the row is expanded. Searching the
 *  live DOM would miss a finished thought and a tool payload. */
export function hiddenSearchText(block: Block): string {
  if (block.kind === "reasoning") return block.text
  if (block.kind === "tool" && block.tool) {
    return [block.tool.args, block.tool.result ?? ""].filter(Boolean).join("\n")
  }
  return ""
}

export function revealBlockIds(blocks: Block[], query: string): string[] {
  const needle = query.trim()
  if (needle.length < FIND_REVEAL_MIN) return []
  for (const block of blocks) {
    if (countMatches(hiddenSearchText(block), needle) > 0) return [block.id]
  }
  return []
}
