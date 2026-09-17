import { api } from "@/lib/api"
import {
  historyNewestSeq as newestOf,
  historyOldestSeq as oldestOf,
  logPageSize,
} from "@/lib/thread-log"
import {
  emptyTranscript,
  MANAGER_ID,
  reduceEvents,
  type TranscriptState,
} from "@/lib/transcript"
import type { SwarmEvent } from "@/lib/types"

/** Stored events for the open conversation, oldest first. Live deltas stay
 *  on the transcript; a prepend rebuilds from this list. */
let threadId = ""
let events: SwarmEvent[] = []

export function resetThreadHistory() {
  threadId = ""
  events = []
}

export function historyNewestSeq(): number {
  return newestOf(events)
}

export function historyOldestSeq(): number {
  return oldestOf(events)
}

export function applyTail(id: string, page: SwarmEvent[]): TranscriptState {
  threadId = id
  events = page.filter((ev) => (ev.seq ?? 0) > 0)
  return reduceEvents(emptyTranscript(), page)
}

export function prependOlder(id: string, older: SwarmEvent[]): TranscriptState | undefined {
  if (threadId !== id) return undefined
  const have = new Set(events.map((ev) => ev.seq))
  const add = older.filter((ev) => (ev.seq ?? 0) > 0 && !have.has(ev.seq))
  events = [...add, ...events]
  return reduceEvents(emptyTranscript(), events)
}

export function rememberStored(id: string, ev: SwarmEvent) {
  if (threadId !== id || (ev.seq ?? 0) <= 0) return
  events.push(ev)
}

export function rememberRewind(id: string, cut: number) {
  if (threadId !== id) return
  events = events.filter((ev) => ev.seq < cut)
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
  const id = get().activeId
  if (!id || get().historyLoading || !get().historyHasMore) return
  const before = historyOldestSeq()
  if (before <= 0) {
    set({ historyHasMore: false })
    return
  }
  set({ historyLoading: true })
  try {
    const page = await api.threadLog(id, {
      before,
      limit: logPageSize(clientHeight ?? 0),
    })
    if (get().activeId !== id) return
    const transcript = prependOlder(id, page.events ?? [])
    if (!transcript) {
      set({ historyLoading: false })
      return
    }
    set({
      transcript,
      historyHasMore: Boolean(page.has_more),
      historyLoading: false,
    })
  } catch (e) {
    if (get().activeId !== id) return
    set({ historyLoading: false, error: fail(e) })
  }
}

export async function loadUntilTurnHistory(
  get: () => HistorySlice,
  turnId: string,
  clientHeight?: number,
): Promise<boolean> {
  const hasTurn = () =>
    (get().transcript.agents[MANAGER_ID]?.blocks ?? []).some(
      (b) => b.turnId === turnId && b.kind === "user",
    )
  const page = clientHeight && clientHeight > 0 ? clientHeight : 20_000
  while (get().historyHasMore && !hasTurn()) {
    const before = historyOldestSeq()
    await get().loadOlder(page)
    if (historyOldestSeq() >= before) break
  }
  return hasTurn()
}
