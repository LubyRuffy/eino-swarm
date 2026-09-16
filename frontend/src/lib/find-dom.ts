import {
  FIND_HIGHLIGHT,
  FIND_HIGHLIGHT_CURRENT,
  FIND_IGNORE_ATTR,
  findMatchWindow,
  type FindMatch,
} from "./find"
import { prefersInstantScroll } from "./turn-nav"

const SKIP_TAGS = new Set(["SCRIPT", "STYLE", "NOSCRIPT", "TEXTAREA", "INPUT", "SELECT"])

export interface TextSlice {
  node: Text
  start: number
  end: number
}

export interface VisibleText {
  haystack: string
  slices: TextSlice[]
}

export interface FindPaint {
  total: number
  painted: number
  current?: Range
}

/** Concatenate visible text nodes so a match can span a markdown tag without
 *  counting as two hits. */
export function collectVisibleText(root: Node): VisibleText {
  const slices: TextSlice[] = []
  const parts: string[] = []
  let offset = 0
  const doc = root.ownerDocument ?? (root.nodeType === Node.DOCUMENT_NODE ? (root as Document) : document)
  const walker = doc.createTreeWalker(root, NodeFilter.SHOW_TEXT, {
    acceptNode(node) {
      const parent = node.parentElement
      if (!parent) return NodeFilter.FILTER_REJECT
      if (SKIP_TAGS.has(parent.tagName)) return NodeFilter.FILTER_REJECT
      if (parent.closest(`[${FIND_IGNORE_ATTR}]`)) return NodeFilter.FILTER_REJECT
      if (parent.closest("[aria-hidden='true']")) return NodeFilter.FILTER_REJECT
      if (!node.nodeValue) return NodeFilter.FILTER_REJECT
      return NodeFilter.FILTER_ACCEPT
    },
  })
  for (let node = walker.nextNode(); node; node = walker.nextNode()) {
    const text = node.nodeValue ?? ""
    const start = offset
    const end = start + text.length
    slices.push({ node: node as Text, start, end })
    parts.push(text)
    offset = end
  }
  return { haystack: parts.join(""), slices }
}

export function rangeFromOffsets(
  slices: TextSlice[],
  start: number,
  end: number,
): Range | undefined {
  if (end <= start || slices.length === 0) return undefined
  const first = sliceAtOffset(slices, start, "start")
  const last = sliceAtOffset(slices, end, "end") ?? first
  if (!first || !last) return undefined
  const range = first.node.ownerDocument.createRange()
  range.setStart(first.node, start - first.start)
  range.setEnd(last.node, end - last.start)
  return range
}

/** Slices are ordered and contiguous. Linear find + a reversed copy was
 *  O(matches × nodes) and showed up once a live turn had thousands of hits. */
function sliceAtOffset(
  slices: TextSlice[],
  offset: number,
  mode: "start" | "end",
): TextSlice | undefined {
  let lo = 0
  let hi = slices.length - 1
  while (lo <= hi) {
    const mid = (lo + hi) >> 1
    const s = slices[mid]
    if (mode === "start") {
      if (offset < s.start) hi = mid - 1
      else if (offset >= s.end) lo = mid + 1
      else return s
    } else if (offset <= s.start) hi = mid - 1
    else if (offset > s.end) lo = mid + 1
    else return s
  }
  return undefined
}

export function rangesForMatches(slices: TextSlice[], matches: FindMatch[]): Range[] {
  const out: Range[] = []
  for (const m of matches) {
    const range = rangeFromOffsets(slices, m.start, m.end)
    if (range) out.push(range)
  }
  return out
}

type HighlightStore = {
  set: (name: string, highlight: Highlight) => void
  delete: (name: string) => unknown
}

function highlightCtor(win: Window): (typeof Highlight) | undefined {
  const ctor = (win as Window & { Highlight?: typeof Highlight }).Highlight
  return typeof ctor === "function" ? ctor : undefined
}

function highlightStore(win: Window): HighlightStore | undefined {
  const css = (win as Window & { CSS?: typeof CSS }).CSS
  const highlights = css && "highlights" in css ? css.highlights : undefined
  if (!highlights || !highlightCtor(win)) return undefined
  return highlights
}

export function supportsCssHighlights(win: Window = window): boolean {
  return Boolean(highlightStore(win))
}

export function clearFindHighlights(win: Window = window): void {
  const store = highlightStore(win)
  if (!store) return
  store.delete(FIND_HIGHLIGHT)
  store.delete(FIND_HIGHLIGHT_CURRENT)
}

/** Count every hit, paint a window around the current one. Spreading thousands
 *  of Ranges into `new Highlight(...)` is what froze a live ⌘F. */
export function applyFindHighlights(
  root: HTMLElement,
  query: string,
  index: number,
  win: Window = window,
): FindPaint {
  clearFindHighlights(win)
  const needle = query.trim()
  if (!needle) return { total: 0, painted: 0 }
  const { haystack, slices } = collectVisibleText(root)
  const windowed = findMatchWindow(haystack, needle, index)
  if (windowed.total === 0) return { total: 0, painted: 0 }
  const ranges = rangesForMatches(slices, windowed.matches)
  if (ranges.length === 0) return { total: windowed.total, painted: 0 }
  const currentAt = Math.min(Math.max(0, windowed.current), ranges.length - 1)
  const current = ranges[currentAt]
  const rest = ranges.filter((_, i) => i !== currentAt)
  const store = highlightStore(win)
  const HighlightClass = highlightCtor(win)
  if (store && HighlightClass) {
    setHighlight(store, HighlightClass, FIND_HIGHLIGHT, rest)
    setHighlight(store, HighlightClass, FIND_HIGHLIGHT_CURRENT, [current])
  }
  return { total: windowed.total, painted: ranges.length, current }
}

function setHighlight(
  store: HighlightStore,
  HighlightClass: typeof Highlight,
  name: string,
  ranges: Range[],
): void {
  if (ranges.length === 0) {
    store.delete(name)
    return
  }
  const highlight = new HighlightClass(ranges[0])
  for (let i = 1; i < ranges.length; i++) highlight.add(ranges[i])
  store.set(name, highlight)
}

export function scrollRangeIntoView(
  scroller: HTMLElement,
  range: Range,
  win: Window = window,
): void {
  const rect =
    typeof range.getBoundingClientRect === "function"
      ? range.getBoundingClientRect()
      : undefined
  const instant = prefersInstantScroll(win)
  const fallbackNode =
    range.startContainer.nodeType === Node.ELEMENT_NODE
      ? (range.startContainer as Element)
      : range.startContainer.parentElement
  if (!rect || (rect.height === 0 && rect.width === 0)) {
    fallbackNode?.scrollIntoView?.({
      behavior: instant ? "auto" : "smooth",
      block: "center",
    })
    return
  }
  const root = scroller.getBoundingClientRect()
  const top =
    rect.top - root.top + scroller.scrollTop - scroller.clientHeight / 2 + rect.height / 2
  const next = Math.max(0, top)
  if (instant || typeof scroller.scrollTo !== "function") {
    scroller.scrollTop = next
    return
  }
  scroller.scrollTo({ top: next, behavior: "smooth" })
}
