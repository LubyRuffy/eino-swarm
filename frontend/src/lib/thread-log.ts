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
