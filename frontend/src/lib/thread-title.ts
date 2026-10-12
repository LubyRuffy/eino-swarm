import type { Thread } from "@/lib/types"

/** A `done` refresh can race the namer: the list still has the placeholder
 *  (`title_auto`) after a `title` event already named the row. Keep the
 *  generated name until the API catches up. A listing that omits
 *  `title_auto` is treated the same as still machine-owned — GET used to
 *  drop the field, and that stomped the name back to the opening line. */
export function preferNamedTitles(local: Thread[], incoming: Thread[]): Thread[] {
  const previousById = new Map(local.map((row) => [row.id, row]))
  const named = new Map(
    local.filter((t) => t.title_auto === false).map((t) => [t.id, t.title]),
  )
  return incoming.map((t) => {
    const previous = previousById.get(t.id)
    // Read watermarks only advance at a fixed completion. A listing that
    // began before the ACK may arrive last, but must not relight that result.
    if (previous?.completed_seq === t.completed_seq && previous?.unread === false && t.unread) {
      t = { ...t, unread: false }
    }
    const keep = named.get(t.id)
    if (keep && t.title_auto !== false) {
      return { ...t, title: keep, title_auto: false }
    }
    return t
  })
}

/** The open conversation's live stamp. A listing fetch that raced Enter
 *  or `done` would otherwise idle a working folder or relight a finished
 *  one. Other rows take the listing as-is: that is how a turn that
 *  started in the background lights up without a click. */
export type ThreadListOverlay = {
  id: string
  running: boolean
  awaitingAnswer?: boolean
  waiting?: boolean
}

export function threadListOverlay(
  activeId: string | undefined,
  status: { running: boolean; awaiting_answer?: boolean; waiting?: boolean },
): ThreadListOverlay | undefined {
  if (!activeId) return undefined
  return {
    id: activeId,
    running: status.running,
    awaitingAnswer: status.running && Boolean(status.awaiting_answer),
    waiting: !status.running && Boolean(status.waiting),
  }
}

/** Reconcile a listing with the sidebar. Background rows trust
 *  `running` / `awaiting_answer` / `waiting` from GET /api/threads. The open
 *  conversation keeps `overlay` so a start/done/arm race cannot flicker. */
export function mergeThreadList(
  local: Thread[],
  incoming: Thread[],
  overlay?: ThreadListOverlay,
  baseline?: Thread[],
): Thread[] {
  const current = new Map(local.map((row) => [row.id, row]))
  const before = new Map(baseline?.map((row) => [row.id, row]))
  const reconciled = baseline ? incoming.map((row) => {
    const now = current.get(row.id)
    const started = before.get(row.id)
    // A response that began before a new result/read/rewind cannot undo it.
    // A real server rewind still applies when the local fields did not change.
    if (!now || (now.completed_seq === started?.completed_seq && now.unread === started?.unread)) return row
    return { ...row, completed_seq: now.completed_seq, unread: now.unread }
  }) : incoming
  const named = preferNamedTitles(local, reconciled)
  if (!overlay) return named
  return named.map((t) => {
    if (t.id !== overlay.id) return t
    const awaiting = overlay.running && Boolean(overlay.awaitingAnswer)
    const waiting = !overlay.running && Boolean(overlay.waiting || t.waiting)
    if (
      t.running === overlay.running &&
      Boolean(t.awaiting_answer) === awaiting &&
      Boolean(t.waiting) === waiting
    ) {
      return t
    }
    return {
      ...t,
      running: overlay.running,
      awaiting_answer: awaiting,
      waiting,
    }
  })
}

/** Opening a minted findings conversation must put it in Recents even when
 *  the list was fetched before that fire existed. */
export function upsertThread(threads: Thread[], thread: Thread): Thread[] {
  const i = threads.findIndex((t) => t.id === thread.id)
  if (i === -1) return [thread, ...threads]
  const next = threads.slice()
  next[i] = preferNamedTitles([next[i]], [thread])[0] ?? thread
  return next
}

/** Sidebar progress is `thread.running`. The live header only knows the
 *  open conversation, so a turn that starts — or is still going when we
 *  leave — has to be stamped here or the folder looks idle until the
 *  next listing refresh. `awaitingAnswer` is the ask_user overlay: same
 *  row, different mark, so a blocked question is not a working pulse. */
export function setThreadRunning(
  threads: Thread[],
  id: string,
  running: boolean,
  awaitingAnswer = false,
): Thread[] {
  let changed = false
  const nextAnswer = running && awaitingAnswer
  const next = threads.map((thread) => {
    if (thread.id !== id) return thread
    if (thread.running === running && Boolean(thread.awaiting_answer) === nextAnswer) {
      return thread
    }
    changed = true
    return { ...thread, running, awaiting_answer: nextAnswer }
  })
  return changed ? next : threads
}

/** Asking ids for the sidebar. Listing `awaiting_answer` covers other
 *  conversations; the open conversation's live status can race the list. */
export function askingThreadIds(
  threads: Array<{ id: string; awaiting_answer?: boolean }>,
  activeId?: string,
  awaitingAnswer?: boolean,
): Set<string> {
  const ids = new Set<string>()
  for (const thread of threads) {
    if (thread.awaiting_answer) ids.add(thread.id)
  }
  if (awaitingAnswer && activeId) ids.add(activeId)
  return ids
}
