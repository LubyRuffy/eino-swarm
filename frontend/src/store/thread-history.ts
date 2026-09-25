import { api } from "@/lib/api"
import {
  extraRosterEvents,
  historyNewestSeq as newestOf,
  historyOldestSeq as oldestOf,
  LOG_LIMIT_MAX,
  LOG_ROW_PX,
  logPageSize,
  mergeLogEvents,
  uniqueStoredEvents,
} from "@/lib/thread-log"
import {
  emptyTranscript,
  MANAGER_ID,
  reduceEvent,
  type TranscriptState,
} from "@/lib/transcript"
import { managerHasVisibleBlocks } from "@/lib/welcome"
import type { SwarmEvent } from "@/lib/types"

/** Stored events for the open conversation, oldest first. Live deltas stay
 *  on the transcript; a prepend rebuilds from this list. Roster rows that
 *  fell out of the viewport sit beside it so paging `before` still walks
 *  the tool log, not a spawned seq from an hour ago. Worker logs fetched
 *  on click sit beside it the same way. */
let threadId = ""
let events: SwarmEvent[] = []
let roster: SwarmEvent[] = []
let agentLogs: SwarmEvent[] = []
const loadedAgents = new Set<string>()

/** One in-flight older page. A jump must join this instead of seeing
 *  `historyLoading` and treating the busy sentinel as the end of the log. */
let olderLoad: Promise<void> | undefined

/** Stored seqs inside a resent cut. A late replay must not put them back. */
let cutFrom = 0
let cutThrough = 0

export function resetThreadHistory() {
  threadId = ""
  events = []
  roster = []
  agentLogs = []
  loadedAgents.clear()
  cutFrom = 0
  cutThrough = 0
}

export function historyNewestSeq(): number {
  return newestOf(events)
}

export function historyOldestSeq(): number {
  return oldestOf(events)
}

export function agentLogIsLoaded(agentId: string): boolean {
  return loadedAgents.has(agentId)
}

function pageSeqs(): Set<number> {
  return new Set(events.filter((ev) => ev.seq > 0).map((ev) => ev.seq))
}

function foldedTranscript(): TranscriptState {
  const inPage = pageSeqs()
  const merged = mergeLogEvents(uniqueStoredEvents([...roster, ...agentLogs]), events)
  let state = emptyTranscript()
  for (const ev of merged) {
    state = reduceEvent(state, ev, inPage.has(ev.seq) ? "full" : "roster")
  }
  return state
}

export function applyTail(
  id: string,
  page: SwarmEvent[],
  extra: SwarmEvent[] = [],
): TranscriptState {
  threadId = id
  events = page.filter((ev) => (ev.seq ?? 0) > 0)
  roster = extraRosterEvents(extra, events)
  agentLogs = []
  loadedAgents.clear()
  return foldedTranscript()
}

export function prependOlder(id: string, older: SwarmEvent[]): TranscriptState | undefined {
  if (threadId !== id) return undefined
  const have = new Set(events.map((ev) => ev.seq))
  const add = older.filter((ev) => (ev.seq ?? 0) > 0 && !have.has(ev.seq))
  events = [...add, ...events]
  roster = extraRosterEvents(roster, events)
  agentLogs = extraRosterEvents(agentLogs, events)
  return foldedTranscript()
}

export function rememberStored(id: string, ev: SwarmEvent) {
  if (threadId !== id || (ev.seq ?? 0) <= 0) return
  if (cutThrough > 0 && ev.seq >= cutFrom && ev.seq <= cutThrough) return
  events.push(ev)
  roster = extraRosterEvents(roster, [ev])
  agentLogs = extraRosterEvents(agentLogs, [ev])
}

export function rememberRewind(id: string, cut: number, through = 0) {
  if (threadId !== id) return
  cutFrom = cut
  if (through > cutThrough) cutThrough = through
  const keep = (seq: number) => seq < cut || (cutThrough > 0 && seq > cutThrough)
  events = events.filter((ev) => keep(ev.seq))
  roster = roster.filter((ev) => keep(ev.seq))
  agentLogs = agentLogs.filter((ev) => keep(ev.seq))
}

export function applyAgentLog(id: string, extra: SwarmEvent[]): TranscriptState | undefined {
  if (threadId !== id) return undefined
  agentLogs = uniqueStoredEvents([...agentLogs, ...extra])
  return foldedTranscript()
}

type HistorySlice = {
  activeId?: string
  historyLoading: boolean
  historyHasMore: boolean
  transcript: TranscriptState
  loadOlder: (clientHeight?: number) => Promise<void>
}

export async function loadOlderHistory(
  get: () => HistorySlice,
  set: (partial: {
    historyLoading?: boolean
    historyHasMore?: boolean
    transcript?: TranscriptState
    error?: string
  }) => void,
  fail: (e: unknown) => string,
  clientHeight?: number,
): Promise<void> {
  const want = get().activeId
  if (!want) return
  for (;;) {
    if (olderLoad) {
      await olderLoad
      continue
    }
    if (get().activeId !== want || !get().historyHasMore) return
    const before = historyOldestSeq()
    if (before <= 0) {
      set({ historyHasMore: false })
      return
    }
    let release: () => void = () => {}
    const mine = new Promise<void>((resolve) => {
      release = resolve
    })
    olderLoad = mine
    set({ historyLoading: true })
    try {
      const page = await api.threadLog(want, {
        before,
        limit: logPageSize(clientHeight ?? 0),
      })
      if (get().activeId !== want) return
      const transcript = prependOlder(want, page.events ?? [])
      if (!transcript) {
        set({ historyLoading: false })
        return
      }
      // Empty body with has_more still true never moved the cursor, so the
      // sentinel stayed hot and the reader could not get past this page.
      const advanced = historyOldestSeq() < before
      set({
        transcript,
        historyHasMore: advanced && Boolean(page.has_more),
        historyLoading: false,
      })
    } catch (e) {
      if (get().activeId !== want) return
      set({ historyLoading: false, error: fail(e) })
    } finally {
      if (olderLoad === mine) olderLoad = undefined
      release()
    }
    return
  }
}

export async function loadUntilTurnHistory(
  get: () => HistorySlice,
  turnId: string,
  clientHeight?: number,
): Promise<boolean> {
  const id = get().activeId
  if (!id || !turnId) return false
  const hasTurn = () =>
    (get().transcript.agents[MANAGER_ID]?.blocks ?? []).some(
      (b) => b.turnId === turnId && b.kind === "user",
    )
  if (hasTurn()) return true
  // The rail already knows the turn exists. Crawl max pages, not the
  // 24-row viewport, or a tool-heavy gap looks like a dead click.
  const page = Math.max(clientHeight ?? 0, LOG_LIMIT_MAX * LOG_ROW_PX)
  while (get().activeId === id && !hasTurn()) {
    if (!get().historyHasMore) break
    const before = historyOldestSeq()
    await get().loadOlder(page)
    if (get().activeId !== id) return false
    if (historyOldestSeq() >= before) break
  }
  return hasTurn()
}

/** Keep paging until the manager has a chat row, or the log runs out.
 *  A long-running tail is often only worker tool events; stopping at that
 *  page would leave the pane looking brand new. */
export async function loadUntilVisibleHistory(get: () => HistorySlice): Promise<void> {
  const id = get().activeId
  while (
    get().activeId === id &&
    get().historyHasMore &&
    !managerHasVisibleBlocks(get().transcript)
  ) {
    const before = historyOldestSeq()
    await get().loadOlder()
    if (get().activeId !== id || historyOldestSeq() >= before) break
  }
}

type AgentLogSlice = {
  activeId?: string
  historyHasMore: boolean
  transcript: TranscriptState
}

export async function loadAgentHistory(
  get: () => AgentLogSlice,
  set: (partial: {
    transcript?: TranscriptState
    agentLogLoading?: string
    error?: string
  }) => void,
  fail: (e: unknown) => string,
  agentId: string,
): Promise<void> {
  const id = get().activeId
  if (!id || !agentId || agentId === MANAGER_ID) return
  if (loadedAgents.has(agentId)) return
  const agent = get().transcript.agents[agentId]
  const hasBody = (agent?.blocks ?? []).some((b) => b.kind !== "user")
  // A complete page walk is contiguous; skip only when this worker already
  // has rows. Roster-only "starting" with an empty pane is the hole the
  // on-demand log exists to fill — even after has_more flipped false.
  if (!get().historyHasMore && hasBody) {
    loadedAgents.add(agentId)
    return
  }
  set({ agentLogLoading: agentId })
  try {
    const page = await api.agentLog(id, agentId)
    if (get().activeId !== id) return
    loadedAgents.add(agentId)
    const transcript = applyAgentLog(id, page.events ?? [])
    set({
      transcript: transcript ?? get().transcript,
      agentLogLoading: undefined,
    })
  } catch (e) {
    if (get().activeId !== id) return
    set({ agentLogLoading: undefined, error: fail(e) })
  }
}
