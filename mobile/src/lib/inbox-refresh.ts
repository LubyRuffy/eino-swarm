import { useRef, type RefObject } from "react"
import { linkError } from "./client"
import { protectConfirmedReads, reduceInbox, type InboxGroupState, type InboxWindow } from "./inbox-window"
import type { RemoteLink } from "./link"
import { GROUP_ATTENTION, OpList, OpMore, type RemoteResponse, type ThreadView } from "./rpc"

type AttentionSnapshot = {
  rows: ThreadView[]
  head: string[]
  priorIds: Set<string>
  identities: Map<string, ThreadView>
  boundary?: ThreadView
  group: InboxGroupState
}

// Go emits RFC3339Nano. Comparing milliseconds or variable-width fractions can
// stop before an already loaded row in the same millisecond has been checked.
export function compareAttention(a: ThreadView, b: ThreadView): number {
  const time = (text: string) => {
    const match = /^(.*:\d{2})(?:\.(\d{1,9}))?(Z|[+-]\d{2}:\d{2})$/.exec(text)
    if (!match) throw new Error("invalid completion timestamp")
    const seconds = Date.parse(match[1] + match[3])
    if (!Number.isFinite(seconds)) throw new Error("invalid completion timestamp")
    return { seconds, fraction: (match[2] ?? "").padEnd(9, "0") }
  }
  const x = time(a.last_active_at), y = time(b.last_active_at)
  if (x.seconds !== y.seconds) return x.seconds - y.seconds
  if (x.fraction !== y.fraction) return x.fraction < y.fraction ? -1 : 1
  return a.id === b.id ? 0 : a.id < b.id ? -1 : 1
}

export async function readAttentionWindow(
  target: RemoteLink, first: RemoteResponse, prior: InboxWindow, current: () => boolean = () => true,
): Promise<AttentionSnapshot | null> {
  const boundary = prior.attention.reduce<ThreadView | undefined>((oldest, row) =>
    !oldest || compareAttention(row, oldest) < 0 ? row : oldest, undefined)
  const expanded = prior.attention.some((row) => !prior.attentionHead.includes(row.id))
  const head = (first.attention ?? []).map((row) => row.id)
  const rows = new Map((first.attention ?? []).map((row) => [row.id, row]))
  let page = first
  const seen = new Set<string>()
  while (expanded && boundary && page.attention_more
    && !(page.attention ?? []).some((row) => compareAttention(row, boundary) <= 0)) {
    const cursor = page.attention_cursor ?? ""
    if (!cursor || seen.has(cursor)) throw new Error("completion page did not advance")
    seen.add(cursor)
    const previous = page.attention?.at(-1)
    page = await target.rpc({ op: OpMore, group: GROUP_ATTENTION, cursor })
    if (!current()) return null
    if (!page.ok) throw new Error(page.error || page.code || "")
    const last = page.attention?.at(-1)
    if (page.attention_more && (!last || (previous && compareAttention(last, previous) >= 0))) {
      throw new Error("completion page did not advance")
    }
    for (const row of page.attention ?? []) rows.set(row.id, row)
  }
  return { rows: [...rows.values()], head, boundary, priorIds: new Set(prior.attention.map((row) => row.id)), identities: completionIdentities(prior),
    group: { id: GROUP_ATTENTION, more: Boolean(page.attention_more), cursor: page.attention_cursor ?? "" } }
}

export function reconcileInbox(current: InboxWindow, first: RemoteResponse, snapshot: AttentionSnapshot): InboxWindow {
  const next = reduceInbox(current, first, "replace")
  const observed = new Map(snapshot.rows.map((row) => [row.id, row]))
  // More may have extended the window while this refresh was awaiting a page.
  // Only the captured range is authoritative; keep the newer extension/cursor.
  const extension = current.attention.filter((row) => !snapshot.priorIds.has(row.id) && !observed.has(row.id)
    && (!snapshot.boundary || compareAttention(row, snapshot.boundary) < 0))
  const group = extension.length > 0 ? current.groups.find((g) => g.id === GROUP_ATTENTION) ?? snapshot.group : snapshot.group
  const newer = [...completionIdentities(current).values()].filter((row) =>
    (row.completed_seq ?? 0) > (snapshot.identities.get(row.id)?.completed_seq ?? 0)
    && (row.completed_seq ?? 0) > (observed.get(row.id)?.completed_seq ?? 0))
  // A later RPC can verify completion14 while this request still holds read12.
  // Capture request-start identities, so that older snapshot cannot erase14.
  for (const row of [...extension, ...newer]) observed.set(row.id, row)
  const rows = [...observed.values()].filter((row) => row.unread).sort((a, b) => compareAttention(b, a))
  return protectConfirmedReads({ ...next, attention: rows,
    attentionHead: snapshot.head, groups: [...next.groups.filter((g) => g.id !== GROUP_ATTENTION), group] })
}

function completionIdentities(window: InboxWindow): Map<string, ThreadView> {
  const rows = new Map<string, ThreadView>()
  for (const row of [...window.attention, ...window.head, ...window.tail]) {
    if ((row.completed_seq ?? 0) >= (rows.get(row.id)?.completed_seq ?? 0)) rows.set(row.id, row)
  }
  return rows
}

/** One list refresh per host at a time; an old host cannot paint the new inbox. */
export function useInboxRefresh(opts: {
  link: RefObject<RemoteLink | null>
  window: RefObject<InboxWindow>
  onPage: (page: RemoteResponse, next: InboxWindow, target: RemoteLink) => void
  onError: (message: string) => void
}) {
  const latest = useRef(opts)
  latest.current = opts
  const pending = useRef(new Set<RemoteLink>())
  return async (target: RemoteLink, first?: RemoteResponse) => {
    if (latest.current.link.current !== target || pending.current.has(target)) return null
    pending.current.add(target)
    const prior = latest.current.window.current
    const current = () => latest.current.link.current === target
    try {
      const page = first ?? await target.rpc({ op: OpList })
      if (!current()) return null
      if (!page.ok) throw new Error(page.error || page.code || "")
      const snapshot = await readAttentionWindow(target, page, prior, current)
      if (!snapshot || !current()) return null
      latest.current.onPage(page, reconcileInbox(latest.current.window.current, page, snapshot), target)
      return page
    } catch (error) {
      if (current()) latest.current.onError(linkError(error))
      return null
    } finally {
      pending.current.delete(target)
    }
  }
}
