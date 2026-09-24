import { isFollowBottom } from "./follow-scroll"
import { plainUserText } from "./quote"
import type { Block } from "./transcript"

/** Fewer than this and a jump rail is just a decoration. */
export const TURN_NAV_MIN = 2

/** Compact gap-2 ticks fit this many before they overflow a cluster and
 *  grow a second scrollbar. Past that they share a fixed height. */
export const TURN_NAV_TICK_PACK = 10

/** Hover-list preview: wider than a tick label because the row wraps. */
export const TURN_NAV_LIST_PREVIEW = 140

export function packTurnNavTicks(count: number): boolean {
  return count > TURN_NAV_TICK_PACK
}

export const TURN_NAV_ATTR = "data-turn-nav"

export interface TurnNavItem {
  id: string
  text: string
}

/** Human user_message rows only. Steering is a nudge inside a turn, not
 *  a place to jump. Engine-started sessions (/goal continue, a wait fire)
 *  never mint a user row, so they share the last human tick. */
export function turnNavItems(blocks: Block[]): TurnNavItem[] {
  const items: TurnNavItem[] = []
  for (const b of blocks) {
    if (b.kind !== "user") continue
    const text = b.text.trim()
    if (!text) continue
    items.push({ id: b.turnId, text: plainUserText(b.text) })
  }
  return items
}

type TurnNavSource = {
  id: string
  user_text: string
  goal_continue?: boolean
  schedule_continue?: boolean
}

/** Turns API is the full list even when the transcript only has a tail page.
 *  `user_text` on an engine-started turn is the protocol prompt, not a
 *  human send — counting those used to clone a scheduled-check tick for
 *  every fire. */
export function turnNavItemsFromTurns(turns: TurnNavSource[]): TurnNavItem[] {
  const items: TurnNavItem[] = []
  for (const t of turns) {
    if (!isHumanNavTurn(t)) continue
    items.push({ id: t.id, text: plainUserText(t.user_text) })
  }
  return items
}

export function isHumanNavTurn(t: TurnNavSource): boolean {
  if (t.goal_continue || t.schedule_continue) return false
  return t.user_text.trim() !== ""
}

/** The complete rail: prefer the turn list when it knows about jumps the
 *  loaded slice has not fetched yet. */
export function resolveTurnNavItems(
  blocks: Block[],
  turns: TurnNavSource[] = [],
): TurnNavItem[] {
  const fromBlocks = turnNavItems(blocks)
  const fromTurns = turnNavItemsFromTurns(turns)
  return fromTurns.length > fromBlocks.length ? fromTurns : fromBlocks
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

/** A send owns the rail once its top sits in this band under the pane top.
 *  Seeing a later bubble lower down does not count: at scrollTop 0 the first
 *  answer can still show the next input, and lighting that tick disagreed
 *  with the scrollbar. The bottom edge and a 40% line both did that. */
const NAV_TOP_BAND = 48

/** The turn that owns the viewport. Items must already be in document order.
 *  Following the live edge is always the latest turn — measuring at scrollTop
 *  0 before the opener has jumped to the bottom used to light the first tick.
 *  Otherwise the probe is a short band under the top of the pane: the last
 *  send that has reached it. A later send that is only visible below stays
 *  inactive. Missing rows (a tail-loaded conversation) are skipped rather
 *  than treated as offset 0, which would pin the rail to the top of the
 *  history. */
export function activeNavId(
  items: { id: string; top?: number }[],
  scrollTop: number,
  clientHeight: number,
  scrollHeight = 0,
  pinned = false,
): string | undefined {
  if (items.length === 0) return undefined
  const last = items[items.length - 1]
  if (pinned) return last.id
  if (scrollHeight > 0 && isFollowBottom(scrollHeight, scrollTop, clientHeight)) {
    return last.id
  }
  const placed = items.filter((item) => typeof item.top === "number")
  if (placed.length === 0) return last.id
  const probe = scrollTop + NAV_TOP_BAND
  let current = placed[0].id
  for (const item of placed) {
    if ((item.top ?? 0) <= probe) current = item.id
    else break
  }
  return current
}

export function prefersInstantScroll(win: Window = window): boolean {
  return Boolean(win.matchMedia?.("(prefers-reduced-motion: reduce)")?.matches)
}

/** Pin the send to the top of THIS scroller. Returns false when it is not
 *  in the pane — a stale id after a conversation switch, not a throw.
 *  Native scrollIntoView also yanks the window and honours scroll-margin,
 *  so a thought fold sitting above the first tick stayed on screen. */
export function scrollTurnIntoView(
  root: HTMLElement,
  id: string,
  instant = false,
): boolean {
  const el = root.querySelector(turnNavSelector(id))
  if (!(el instanceof HTMLElement)) return false
  const top = Math.max(0, offsetInScroller(el, root))
  const view = root.ownerDocument.defaultView
  const reduce = view ? prefersInstantScroll(view) : instant
  if (!instant && !reduce && typeof root.scrollTo === "function") {
    root.scrollTo({ top, behavior: "smooth" })
    return true
  }
  root.scrollTop = top
  return true
}

/** Distance from the scroller's content top to this row. offsetTop is relative
 *  to the offsetParent, which is some inner wrapper, not the thing that scrolls. */
export function offsetInScroller(el: HTMLElement, root: HTMLElement): number {
  return el.getBoundingClientRect().top - root.getBoundingClientRect().top + root.scrollTop
}
