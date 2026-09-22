import { isFollowBottom } from "./follow-scroll"
import type { SwarmEvent } from "./types"

/** One viewport of tool rows, plus a little overscan. Opening a conversation
 *  asks for this many events from the live edge; scrolling up asks again. */
export const LOG_ROW_PX = 40
export const LOG_LIMIT_MIN = 24
export const LOG_LIMIT_MAX = 200
export const LOG_LIMIT_DEFAULT = 80

export type ThreadLog = {
  events: SwarmEvent[]
  has_more: boolean
  /** spawned / finished / cleanup rows that fell out of this viewport.
   *  Only the live-edge page sends it; paging must not move the cursor. */
  roster?: SwarmEvent[]
}

/** Rows the Agents tab needs that are not already in the loaded page.
 *  Mixing them into the paging buffer would walk `before` from a spawned
 *  seq and skip the tools in between. */
export function extraRosterEvents(
  roster: SwarmEvent[] | undefined,
  page: SwarmEvent[],
): SwarmEvent[] {
  const have = new Set(
    page.filter((ev) => (ev.seq ?? 0) > 0).map((ev) => ev.seq),
  )
  return (roster ?? []).filter((ev) => (ev.seq ?? 0) > 0 && !have.has(ev.seq))
}

/** Fold order: roster first (by seq), then the contiguous page. */
export function mergeLogEvents(
  roster: SwarmEvent[] | undefined,
  page: SwarmEvent[],
): SwarmEvent[] {
  const stored = uniqueStoredEvents(page)
  const extra = extraRosterEvents(roster, stored)
  if (extra.length === 0) return stored
  return uniqueStoredEvents([...extra, ...stored]).sort((a, b) => a.seq - b.seq)
}

/** Stored rows, first seq wins. Sidecars plus the page can name the same
 *  spawned row twice; reducing it twice would look like a twin. */
export function uniqueStoredEvents(rows: SwarmEvent[]): SwarmEvent[] {
  const have = new Set<number>()
  const out: SwarmEvent[] = []
  for (const ev of rows) {
    if ((ev.seq ?? 0) <= 0 || have.has(ev.seq)) continue
    have.add(ev.seq)
    out.push(ev)
  }
  return out
}

export function logPageSize(clientHeight: number): number {
  if (clientHeight <= 0) return LOG_LIMIT_DEFAULT
  const rows = Math.ceil(clientHeight / LOG_ROW_PX) + 4
  return Math.min(LOG_LIMIT_MAX, Math.max(LOG_LIMIT_MIN, rows))
}

export function historyOldestSeq(events: SwarmEvent[]): number {
  for (const ev of events) {
    if (ev.seq > 0) return ev.seq
  }
  return 0
}

export function historyNewestSeq(events: SwarmEvent[]): number {
  let n = 0
  for (const ev of events) {
    if (ev.seq > n) n = ev.seq
  }
  return n
}

/** Near the top of the scroller, or the whole log still fits: fetch an
 *  older page. The live edge stays put until the reader actually scrolls. */
export function shouldLoadOlderHistory(
  hasMore: boolean,
  loading: boolean,
  scrollTop: number,
  scrollHeight: number,
  clientHeight: number,
): boolean {
  if (!hasMore || loading) return false
  if (scrollHeight <= clientHeight + 8) return true
  return scrollTop < 48
}

/** The reader has left the live edge and is close to the loaded slice.
 *  `scrollTop < 48` never fired while the third turn-nav from the end was
 *  already on screen and a partial older turn still sat above it.
 *  `oldestTurnFromTop` is that row's distance from the viewport top
 *  (negative once it has scrolled above). */
export function readerNearOlderHistory(
  scrollTop: number,
  scrollHeight: number,
  clientHeight: number,
  oldestTurnFromTop?: number,
): boolean {
  if (clientHeight <= 0) return false
  if (isFollowBottom(scrollHeight, scrollTop, clientHeight)) return false
  if (scrollTop < clientHeight) return true
  if (oldestTurnFromTop === undefined) return false
  return oldestTurnFromTop < clientHeight && oldestTurnFromTop > -clientHeight
}
