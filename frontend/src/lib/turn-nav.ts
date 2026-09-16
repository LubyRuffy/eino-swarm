import type { Block } from "./transcript"

/** Fewer than this and a jump rail is just a decoration. */
export const TURN_NAV_MIN = 2

export const TURN_NAV_ATTR = "data-turn-nav"

export interface TurnNavItem {
  id: string
  text: string
}

/** User turns only. Steering is a nudge inside a turn, not a place to jump. */
export function turnNavItems(blocks: Block[]): TurnNavItem[] {
  const items: TurnNavItem[] = []
  for (const b of blocks) {
    if (b.kind !== "user") continue
    const text = b.text.trim()
    if (!text) continue
    items.push({ id: b.turnId, text: b.text })
  }
  return items
}

/** One-line label for a tick or a list row. Newlines would blow the rail up. */
export function previewText(text: string, max = 72): string {
  const one = text.replace(/\s+/g, " ").trim()
  if (one.length <= max) return one
  return `${one.slice(0, Math.max(0, max - 1)).trimEnd()}…`
}

export function turnNavSelector(id: string): string {
  const escaped =
    typeof CSS !== "undefined" && typeof CSS.escape === "function"
      ? CSS.escape(id)
      : id.replace(/\\/g, "\\\\").replace(/"/g, '\\"')
  return `[${TURN_NAV_ATTR}="${escaped}"]`
}

/** The turn that owns the viewport. Items must already be in document order.
 *  Near the bottom of the scroller always means the latest turn: composer
 *  padding keeps that user row below a top probe even while you are
 *  watching it. */
export function activeNavId(
  items: { id: string; top: number }[],
  scrollTop: number,
  clientHeight: number,
  scrollHeight = 0,
): string | undefined {
  if (items.length === 0) return undefined
  const last = items[items.length - 1]
  if (scrollHeight > 0 && scrollTop + clientHeight >= scrollHeight - 16) {
    return last.id
  }
  const probe = scrollTop + Math.min(96, Math.max(24, clientHeight * 0.2))
  let current = items[0].id
  for (const item of items) {
    if (item.top <= probe) current = item.id
    else break
  }
  return current
}

export function prefersInstantScroll(win: Window = window): boolean {
  return Boolean(win.matchMedia?.("(prefers-reduced-motion: reduce)")?.matches)
}

/** Scroll the user row into view. Returns false when it is not in this scroller
 *  — a stale id after a conversation switch, not a throw. */
export function scrollTurnIntoView(
  root: HTMLElement,
  id: string,
  instant = false,
): boolean {
  const el = root.querySelector(turnNavSelector(id))
  if (!(el instanceof HTMLElement)) return false
  const view = root.ownerDocument.defaultView
  const reduce = view ? prefersInstantScroll(view) : instant
  el.scrollIntoView({
    behavior: instant || reduce ? "auto" : "smooth",
    block: "start",
  })
  return true
}

/** Distance from the scroller's content top to this row. offsetTop is relative
 *  to the offsetParent, which is some inner wrapper, not the thing that scrolls. */
export function offsetInScroller(el: HTMLElement, root: HTMLElement): number {
  return el.getBoundingClientRect().top - root.getBoundingClientRect().top + root.scrollTop
}
