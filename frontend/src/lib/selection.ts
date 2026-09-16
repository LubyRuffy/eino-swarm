/** Transcript (and sub-agent) scrollers that may be quoted into the composer.
 *  A selection that is not fully inside one of these is just a selection. */
export const QUOTE_SOURCE_ATTR = "data-quote-source"

export interface PageSelection {
  text: string
  rect: SelectionRect
}

export interface SelectionRect {
  top: number
  left: number
  width: number
  height: number
  bottom: number
}

/** Strip the junk a browser selection picks up (nbsp, CR) without flattening
 *  the line breaks the user actually highlighted. */
export function normalizeSelectedText(raw: string): string {
  return raw
    .replace(/\u00a0/g, " ")
    .replace(/\r\n/g, "\n")
    .replace(/\r/g, "\n")
    .trim()
}

export function quoteSourceOf(range: Range, doc: Document = document): Element | null {
  const nodes = doc.querySelectorAll(`[${QUOTE_SOURCE_ATTR}]`)
  for (const root of nodes) {
    if (root.contains(range.startContainer) && root.contains(range.endContainer)) {
      return root
    }
  }
  return null
}

/** Read a quote-worthy selection. Empty, collapsed, or spanning outside a
 *  quote source (composer, sidebar, the Add to chat pill itself) is nothing. */
export function readTextSelection(doc: Document = document): PageSelection | null {
  const sel = doc.defaultView?.getSelection()
  if (!sel || sel.isCollapsed || sel.rangeCount === 0) return null
  const range = sel.getRangeAt(0)
  if (!quoteSourceOf(range, doc)) return null
  const text = normalizeSelectedText(sel.toString())
  if (!text) return null
  // jsdom's Range has no layout, so tests (and a headless tab) still get the
  // text; the pill just sits at 0,0 instead of covering the highlight.
  const rect =
    typeof range.getBoundingClientRect === "function"
      ? range.getBoundingClientRect()
      : { top: 0, left: 0, width: 0, height: 0, bottom: 0 }
  return {
    text,
    rect: {
      top: rect.top,
      left: rect.left,
      width: rect.width,
      height: rect.height,
      bottom: rect.bottom,
    },
  }
}

/** Sit the Add to chat pill on the selection, above when there is room so it
 *  does not cover the words the user just highlighted. */
export function menuPosition(
  rect: SelectionRect,
  viewport: { width: number; height: number },
): { top: number; left: number; place: "above" | "below" } {
  const gap = 8
  const menuH = 40
  const center = rect.left + rect.width / 2
  const place = rect.top < menuH + gap ? "below" : "above"
  let top = place === "above" ? rect.top - gap : rect.bottom + gap
  if (viewport.height > 0) {
    top = Math.min(Math.max(top, gap), Math.max(gap, viewport.height - gap))
  }
  const left =
    viewport.width > 0
      ? Math.min(Math.max(center, 8), Math.max(8, viewport.width - 8))
      : Math.max(center, 8)
  return { top, left, place }
}

/** A pointer drag across the page looks like click-and-drag to the browser,
 *  so it paints a selection through whatever text the cursor crosses. Hold
 *  selection off until the gesture ends. */
export function holdSelection(doc: Document = document): () => void {
  const selection = doc.defaultView?.getSelection()
  selection?.removeAllRanges()
  const body = doc.body
  const prevUser = body.style.userSelect
  const prevWebkit = body.style.webkitUserSelect
  body.style.userSelect = "none"
  body.style.webkitUserSelect = "none"
  const block = (event: Event) => event.preventDefault()
  doc.addEventListener("selectstart", block)
  return () => {
    body.style.userSelect = prevUser
    body.style.webkitUserSelect = prevWebkit
    doc.removeEventListener("selectstart", block)
    selection?.removeAllRanges()
  }
}
