import { useEffect, useRef, useState, type RefObject } from "react"
import { linkError, remoteError } from "./client"
import type { InboxWindow } from "./inbox-window"
import type { RemoteLink } from "./link"
import { OpEvent, OpRead, type RemoteResponse, type ThreadDetail, type ThreadView } from "./rpc"
import type { PhoneView } from "./session"

export function primaryCompletion(resp: RemoteResponse, view: PhoneView): number {
  const ev = resp.event
  if (resp.op !== OpEvent || !ev || !view.caughtUp || !view.detail) return 0
  if ((resp.thread_id && resp.thread_id !== view.threadId) || ev.thread_id !== view.threadId) return 0
  if (ev.seq <= view.lastSeq || ev.seq <= 0 || !["done", "error"].includes(ev.kind)) return 0
  if ((ev.agent_id && ev.agent_id !== "manager") || (ev.role && ev.role !== "manager")) return 0
  return ev.seq
}

// An older read reply cannot clear a completion that arrived while it was in flight.
export function mergeCompletion<T extends Pick<ThreadDetail, "id" | "unread" | "completed_seq">>(
  current: T, reply: ThreadDetail, baseline = 0,
): T {
  const seq = current.completed_seq ?? 0, next = reply.completed_seq ?? 0
  if (current.id !== reply.id || seq < baseline || seq > next
    || (seq === next && current.unread === false && reply.unread)) return current
  return { ...current, unread: reply.unread, completed_seq: reply.completed_seq }
}

export function readInbox(prev: InboxWindow, detail: ThreadDetail, view?: PhoneView, baseline = 0): InboxWindow {
  if (view?.detail?.id === detail.id && ((view.detail.completed_seq ?? 0) < baseline
    || (view.detail.completed_seq ?? 0) > (detail.completed_seq ?? 0))) return prev
  const merge = (row: ThreadView) => mergeCompletion(row, detail, baseline)
  const seq = detail.completed_seq ?? 0
  const confirmedReads = detail.unread === false && seq > 0
    ? { ...prev.confirmedReads, [detail.id]: Math.max(prev.confirmedReads[detail.id] ?? 0, seq) } : prev.confirmedReads
  return { ...prev, confirmedReads, head: prev.head.map(merge), tail: prev.tail.map(merge),
    attention: prev.attention.map(merge).filter((row) => row.unread) }
}

type ReadIntent = { target: RemoteLink; id: string; seq: number; baseline: number }

/** Reading follows a rendered, visible detail, never an open/ready used by recovery. */
export function useCompletionRead(opts: {
  link: RefObject<RemoteLink | null>
  view: RefObject<PhoneView>
  contentVisible?: RefObject<boolean>
  visible: boolean
  onRead: (detail: ThreadDetail, baseline: number) => void
  onError: (message: string) => void
}) {
  const [intent, setIntent] = useState<ReadIntent | null>(null)
  const observed = useRef<ReadIntent | null>(null)
  const latest = useRef(opts)
  latest.current = opts
  const request = (target: RemoteLink, id: string, seq: number) => {
    if (seq <= 0) return
    const next = { target, id, seq, baseline: latest.current.view.current.detail?.completed_seq ?? 0 }
    observed.current = next
    setIntent(next)
  }
  const observe = (target: RemoteLink, resp: RemoteResponse) => {
    const { visible, view, contentVisible } = latest.current
    if (!visible || contentVisible?.current === false || document.visibilityState === "hidden") return
    request(target, view.current.threadId ?? "", primaryCompletion(resp, view.current))
  }
  useEffect(() => {
    if (!intent) return
    const current = latest.current
    const view = current.view.current
    if (!current.visible || current.contentVisible?.current === false || document.visibilityState === "hidden" || current.link.current !== intent.target
      || view.threadId !== intent.id || !view.detail || !view.caughtUp
      || !view.events.some((ev) => ev.seq === intent.seq && ev.thread_id === intent.id
        && ["done", "error"].includes(ev.kind) && (!ev.agent_id || ev.agent_id === "manager")
        && (!ev.role || ev.role === "manager"))) {
      setIntent(null)
      return
    }
    // Remove the intent before awaiting: an unrelated render is not another read.
    setIntent(null)
    void intent.target.rpc({ op: OpRead, thread_id: intent.id, completed_seq: intent.seq }).then((resp) => {
      const next = latest.current
      if (next.link.current !== intent.target) return
      if (!resp.ok || !resp.detail || resp.detail.id !== intent.id) {
        next.onError(remoteError(resp.error || resp.code || "read failed"))
        return
      }
      const newer = observed.current
      if (newer?.target === intent.target && newer.id === intent.id && newer.seq > intent.seq
        && (resp.detail.completed_seq ?? 0) < newer.seq) return
      next.onRead(resp.detail, intent.baseline)
    }).catch((error) => {
      if (latest.current.link.current === intent.target) latest.current.onError(linkError(error))
    })
  }, [intent])
  return { request, observe }
}
