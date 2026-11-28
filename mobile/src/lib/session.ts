import { applyGoalDetail } from "./goal"
import { parseIterationConfirmation } from "./iteration-limit"
import {
  OpEvent,
  OpReady,
  type RemoteEvent,
  type FollowupView,
  type HistoryWork,
  type RemoteResponse,
  type ThreadDetail,
  type WatchStatus,
} from "./rpc"
import { applyWakeDetail, laterScheduleDue } from "./schedule"
import { applyEvent, pendingAsk, type CompactBlock } from "./transcript"

/** One open thread on the phone. threadId is set before watch returns,
 *  because catch-up events can beat React's setState. Events that arrive
 *  before ready are queued, not painted — otherwise the transcript plays
 *  from the first user message and then yanks to the tail. */
export type PhoneView = {
  threadId: string | null
  lastSeq: number
  oldestSeq: number
  hasMore: boolean
  caughtUp: boolean
  queued: RemoteEvent[]
  events: RemoteEvent[]
  /** Spawned identities for workers still running outside the loaded page.
   *  They are not part of the history cursor. */
  roster: RemoteEvent[]
  blocks: CompactBlock[]
  historyWork: HistoryWork[]
  detail: ThreadDetail | null
  followups: FollowupView[]
}

export function emptyView(): PhoneView {
  return {
    threadId: null,
    lastSeq: 0,
    oldestSeq: 0,
    hasMore: false,
    caughtUp: false,
    queued: [],
    events: [],
    roster: [],
    blocks: [],
    historyWork: [],
    detail: null,
    followups: [],
  }
}

export function openView(detail: ThreadDetail): PhoneView {
  return { ...emptyView(), threadId: detail.id, detail }
}

export function applyOpenDetail(view: PhoneView, detail: ThreadDetail): PhoneView {
  if (view.threadId === detail.id && view.detail) {
    return { ...view, detail }
  }
  return openView(detail)
}

/** A legacy header arrives after ready. Fill metadata without replaying the
 * snapshot or replacing actual live changes, including fields changed twice. */
export function applyPendingOpenDetail(view: PhoneView, detail: ThreadDetail, before: PhoneView): PhoneView {
  let merged = { ...detail }
  const known = new Set(before.events.map((event) => `${event.thread_id}:${event.seq}`))
  for (const event of view.events) {
    // Identity excludes already-present rows; the snapshot boundary separately
    // excludes historical pages, which cannot change the current header.
    if (known.has(`${event.thread_id}:${event.seq}`) || event.seq < before.lastSeq) continue
    merged = patchDetail(merged, event, view.blocks)
  }
  if (before.detail && view.detail) {
    for (const key of Object.keys({ ...before.detail, ...view.detail }) as (keyof ThreadDetail)[]) {
      if (view.detail[key] !== before.detail[key]) Object.assign(merged, { [key]: view.detail[key] })
    }
  }
  return applyOpenDetail(view, merged)
}

export function markRunning(view: PhoneView): PhoneView {
  if (!view.detail || view.detail.running) return view
  return {
    ...view,
    detail: {
      ...view.detail,
      running: { thread_id: view.detail.id, title: view.detail.title },
    },
  }
}

export function applyPush(view: PhoneView, resp: RemoteResponse): PhoneView {
  if (!view.threadId) return view
  if (resp.thread_id && resp.thread_id !== view.threadId) return view
  if (resp.op === OpReady) return applyReady(view, resp)
  if (resp.op === OpEvent && resp.event) {
    if (!view.caughtUp) {
      return { ...view, queued: view.queued.concat(resp.event) }
    }
    return applyLiveEvent(view, resp.event, resp.status)
  }
  return view
}

export function prependOlder(
  view: PhoneView,
  events: RemoteEvent[],
  more: boolean,
  cursor = 0,
  work: HistoryWork[] = [],
): PhoneView {
  const incoming = events
    .filter((ev) => {
      if (ev.seq <= 0) return false
      if (view.threadId && ev.thread_id && ev.thread_id !== view.threadId) return false
      return view.oldestSeq === 0 || ev.seq < view.oldestSeq
    })
    .sort((a, b) => a.seq - b.seq)
  const next = cursor > 0 && (!view.oldestSeq || cursor < view.oldestSeq) ? cursor : 0
  if (incoming.length === 0 && work.length === 0)
    return { ...view, oldestSeq: next || view.oldestSeq, hasMore: more && next > 0 }
  const merged = mergeEvents(view.events, incoming)
  const roster = pageRoster(view.roster, merged)
  const blocks = restoreStreaming(foldPage(merged, roster), view.blocks)
  return {
    ...view,
    events: merged,
    roster,
    oldestSeq: next || merged[0]?.seq || view.oldestSeq,
    hasMore: more,
    historyWork: mergeHistoryWork(view.historyWork, work, merged),
    blocks,
    detail: askDetail(view.detail, blocks),
  }
}

function applyReady(view: PhoneView, resp: RemoteResponse): PhoneView {
  const fromReady = resp.events ?? []
  const stored = mergeEvents(view.events, mergeEvents(fromReady, view.queued.filter((ev) => ev.seq > 0)))
  const deltas = view.queued.filter((ev) => ev.seq <= 0)
  // A ready that carries roster replaces it, including an explicit empty list.
  // An older host omits the field; keep the sidecar already on screen.
  const roster = pageRoster(Array.isArray(resp.roster) ? resp.roster : view.roster, stored)
  let blocks = foldPage(stored, roster)
  for (const ev of deltas) blocks = applyEvent(blocks, ev)
  let detail = view.detail
  let lastSeq = Math.max(view.lastSeq, resp.seq ?? 0)
  for (const ev of stored.concat(deltas)) {
    if (detail) detail = patchDetail(detail, ev, blocks)
    if (ev.seq > lastSeq) lastSeq = ev.seq
  }
  if (resp.status && detail) {
    detail = askDetail(applyWatchStatus(detail, resp.status, true), blocks)
  }
  const oldest = oldestOf(stored, lastSeq, Boolean(resp.more), view.oldestSeq)
  return {
    ...view,
    lastSeq,
    oldestSeq: resp.history_before ? Math.min(resp.history_before, oldest || Infinity) : oldest,
    hasMore: resp.history_before ? Boolean(resp.more) :
      Boolean(resp.more) ||
      (fromReady.length === 0 && view.hasMore) ||
      (stored[0]?.seq ?? 0) > 1,
    caughtUp: true,
    queued: [],
    events: stored,
    roster,
    blocks,
    historyWork: mergeHistoryWork(view.historyWork, resp.history_work ?? [], stored),
    detail,
    followups: resp.status?.followups ?? view.followups,
  }
}

function mergeHistoryWork(loaded: HistoryWork[], added: HistoryWork[], events: RemoteEvent[]): HistoryWork[] {
  if (!added.length && !loaded.length) return loaded
  const ranges = new Map([...loaded, ...added].map((range) => [range.from, range]))
  for (const event of events) {
    const cut = rewindCut(event)
    if (!cut) continue
    for (const [from, range] of ranges) {
      if (range.through < cut || from >= event.seq) continue
      if (from >= cut) ranges.delete(from)
      else ranges.set(from, { ...range, through: cut - 1 })
    }
  }
  return [...ranges.values()].sort((left, right) => left.from - right.from)
}

function oldestOf(
  stored: RemoteEvent[],
  lastSeq: number,
  more: boolean,
  fallback: number,
): number {
  let n = fallback
  if (stored[0]?.seq) n = stored[0].seq
  else if (more && lastSeq > 0) n = lastSeq + 1
  // A log cursor that already walked older than this snapshot must not
  // jump forward because a later ready reused lastSeq+1 as the window.
  if (fallback > 0 && (n <= 0 || fallback < n)) return fallback
  return n
}

function applyLiveEvent(view: PhoneView, ev: RemoteEvent, status?: WatchStatus): PhoneView {
  if (ev.seq > 0 && view.events.some((row) => row.seq === ev.seq)) return view
  // The sidecar already represents this row. Putting it in the page would
  // pull the Earlier cursor back to that older seq.
  if (ev.seq > 0 && view.roster.some((row) => row.seq === ev.seq)) return view
  const cut = rewindCut(ev)
  if (cut > 0) return applyRewind(view, ev, cut)
  const events = rememberEvent(view.events, ev)
  const roster = pageRoster(view.roster, events)
  const historical = ev.seq > 0 && ev.seq < view.lastSeq
  const blocks = historical
    ? restoreStreaming(foldPage(events, roster), view.blocks)
    : applyEvent(view.blocks, ev)
  let detail = view.detail
  if (detail) {
    if (!historical) detail = patchDetail(detail, ev, blocks)
    if (status) detail = applyWatchStatus(detail, status, false)
    detail = askDetail(detail, blocks)
  }
  return {
    ...view,
    lastSeq: ev.seq > view.lastSeq ? ev.seq : view.lastSeq,
    oldestSeq:
      ev.seq > 0 && (view.oldestSeq === 0 || ev.seq < view.oldestSeq)
        ? ev.seq
        : view.oldestSeq,
    events,
    roster,
    blocks,
    detail,
    followups: status?.followups ?? view.followups,
  }
}

/** An earlier message was resent, so the host removed every row
 *  from the cut on, along with the turn that was streaming. Rows below a cut
 *  at or before the oldest loaded one are still stored and stay pageable. */
function applyRewind(view: PhoneView, ev: RemoteEvent, cut: number): PhoneView {
  const events = ev.seq > 0 ? rememberEvent(view.events, ev) : view.events.filter((row) => row.seq < cut)
  const roster = pageRoster(view.roster, events).filter((row) => row.seq < cut || (ev.seq > 0 && row.seq >= ev.seq))
  const blocks = foldPage(events, roster)
  const unloaded = view.oldestSeq === 0 || view.oldestSeq >= cut
  const removedTurns = new Set(view.events.filter((row) =>
    row.seq >= cut && (ev.seq <= 0 || row.seq < ev.seq)).map((row) => row.turn_id))
  const detail = view.detail && {
    ...view.detail,
    turns: view.detail.turns?.filter((turn) => !removedTurns.has(turn.id)),
  }
  return {
    ...view,
    lastSeq: Math.max(view.lastSeq, ev.seq),
    oldestSeq: unloaded ? cut : view.oldestSeq,
    hasMore: unloaded ? view.hasMore || cut > 1 : view.hasMore,
    events,
    roster,
    blocks,
    historyWork: view.historyWork.flatMap((range) => range.from >= cut ? [] : [{ ...range, through: Math.min(range.through, cut - 1) }]),
    detail: askDetail(detail, blocks),
  }
}

function rewindCut(ev: RemoteEvent): number {
  if (ev.kind !== "rewound") return 0
  const cut = Number.parseInt(ev.text ?? "", 10)
  return Number.isFinite(cut) && cut > 0 ? cut : 0
}

/** A stored rewind marker removes the rows from its cut up to itself. */
function withoutRewound(rows: RemoteEvent[]): RemoteEvent[] {
  let kept = rows
  for (const marker of rows) {
    const cut = rewindCut(marker)
    if (cut > 0) kept = kept.filter((row) => row.seq < cut || row.seq >= marker.seq)
  }
  return kept
}

function rememberEvent(events: RemoteEvent[], ev: RemoteEvent): RemoteEvent[] {
  if (ev.seq <= 0) return events
  if (events.some((row) => row.seq === ev.seq)) return events
  return mergeEvents(events, [ev])
}

function pageRoster(roster: RemoteEvent[] | undefined, events: RemoteEvent[]): RemoteEvent[] {
  const seen = new Set(events.map((ev) => ev.seq))
  const extra = (roster ?? []).filter((ev) => ev.seq > 0 && !seen.has(ev.seq))
  return withoutRewound(extra.concat(events.filter((ev) => ev.kind === "rewound"))).filter((ev) => ev.kind !== "rewound")
}

function foldPage(events: RemoteEvent[], roster: RemoteEvent[] | undefined): CompactBlock[] {
  return foldEvents(mergeEvents(pageRoster(roster, events), events))
}

function mergeEvents(base: RemoteEvent[], extra: RemoteEvent[]): RemoteEvent[] {
  const bySeq = new Map<number, RemoteEvent>()
  for (const ev of base) {
    if (ev.seq > 0) bySeq.set(ev.seq, ev)
  }
  for (const ev of extra) {
    if (ev.seq > 0) bySeq.set(ev.seq, ev)
  }
  return withoutRewound([...bySeq.values()].sort((a, b) => a.seq - b.seq))
}

function foldEvents(events: RemoteEvent[]): CompactBlock[] {
  let blocks: CompactBlock[] = []
  for (const ev of events) blocks = applyEvent(blocks, ev)
  return blocks
}

function restoreStreaming(blocks: CompactBlock[], prev: CompactBlock[]): CompactBlock[] {
  let next = blocks.slice()
  for (const live of prev.filter((b) => b.streaming || b.messageId && b.sourceSeq === 0)) {
    if (live.messageId && (live.kind === "reasoning" || live.kind === "answer")) {
      next = applyEvent(next, {
        thread_id: "", agent_id: live.agentId, message_id: live.messageId, seq: 0,
        kind: live.kind === "reasoning" ? live.streaming ? "reasoning_delta" : "reasoning" : live.streaming ? "delta" : "agent_message",
        text: live.text, created_at: "",
      })
      continue
    }
    if (next.some((b) => b.generation && b.id === `${live.id}:generation`)) continue
    const i = next.findIndex((b) => b.id === live.id)
    if (i >= 0) next[i] = live
    else next.push(live)
  }
  return next
}

function applyWatchStatus(detail: ThreadDetail, st: WatchStatus, snapshot: boolean): ThreadDetail {
  const next: ThreadDetail = {
    ...detail,
    running: st.running
      ? {
          thread_id: detail.id,
          title: detail.title,
          turn_id: st.turn_id,
          ask_user: st.awaiting_answer || Boolean(detail.running?.ask_user),
          awaiting_continue: st.awaiting_continue,
          waiting: st.waiting,
        }
      : undefined,
  }
  if (snapshot || st.waiting) {
    next.waiting = Boolean(st.waiting)
    if (st.waiting) {
      next.wake = st.wake
        ? {
            id: st.wake.id,
            title: st.wake.title ?? detail.wake?.title,
            prompt: st.wake.prompt ?? detail.wake?.prompt,
            next_run_at: laterScheduleDue(detail.wake?.next_run_at, st.wake.next_run_at),
          }
        : detail.wake
    } else if (snapshot) {
      next.wake = undefined
    }
  }
  return next
}

function patchDetail(
  detail: ThreadDetail,
  ev: RemoteEvent,
  blocks: CompactBlock[],
): ThreadDetail {
  let next = applyGoalDetail(applyWakeDetail(detail, ev), ev)
  if (ev.kind === "title" && ev.text) {
    next = { ...next, title: ev.text }
  }
  if (ev.kind === "plan") {
    next = { ...next, plan_on: true }
  }
  if (ev.kind === "plan_cancelled" || ev.kind === "plan_implemented") {
    next = { ...next, plan_on: false }
  }
  if (ev.kind === "turn") {
    next = {
      ...next,
      running: {
        thread_id: next.id,
        title: next.title,
        turn_id: ev.turn_id,
        ask_user: false,
      },
    }
  }
  if (ev.kind === "done" || ev.kind === "error") {
    next = { ...next, running: undefined }
  }
  if (next.running && (!ev.turn_id || !next.running.turn_id || ev.turn_id === next.running.turn_id)) {
    if (ev.kind === "max_iterations") {
      let awaiting = Boolean(parseIterationConfirmation(ev.text))
      try {
        const pause = JSON.parse(ev.text) as { reason?: unknown; automatic_resume?: unknown } | null
        if (pause?.reason === "workers_uncollected") awaiting = pause.automatic_resume !== true
      } catch { /* Only a recognized pause can change current controls. */ }
      if (awaiting) next = { ...next, running: { ...next.running, awaiting_continue: true } }
    } else if (["max_iterations_continued", "workers_collection_resumed", "workers_collection_declined"].includes(ev.kind)) {
      next = { ...next, running: { ...next.running, awaiting_continue: false } }
    }
  }
  return askDetail(next, blocks)!
}

function askDetail(detail: ThreadDetail | null, blocks: CompactBlock[]): ThreadDetail | null {
  if (!detail?.running) return detail
  const questions = blocks.filter((b) => !b.agentId && b.kind === "question" && (!detail.running?.turn_id || b.turnId === detail.running.turn_id))
  if (!questions.length) return detail
  return { ...detail, running: { ...detail.running, ask_user: Boolean(pendingAsk(questions)) } }
}
