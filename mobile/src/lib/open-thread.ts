import { t } from "@/lib/i18n"
import type { RemoteLink } from "@/lib/link"
import { linkError, remoteError } from "@/lib/client"
import { OpLog, OpOpen, OpReady, OpWatch } from "@/lib/rpc"
import { applyOpenDetail, applyPendingOpenDetail, applyPush, emptyView, prependOlder, type PhoneView } from "@/lib/session"
import { clearLastThreadId, loadLastThreadId, saveLastThreadId } from "@/lib/store"
import { hasPhoneHistoryContent } from "@/lib/phone-history"

// A second open of the same thread invalidates the first request as well.
const openings = new WeakMap<RemoteLink, object>()

/** Opens one thread and watches it. A cleared threadId means Back already
 *  left: a check that only bails when some other id is current treats "left"
 *  as still here, and the late open paints the conversation back over the inbox. */
export async function openPhoneThread(opts: {
  target: RemoteLink
  id: string
  view: () => PhoneView
  setView: (next: PhoneView) => void
  commitView: (next: PhoneView) => void
  setError: (message: string) => void
  bumpLoad: () => void
  clearOlder: () => void
}): Promise<boolean> {
  const { target, id, view, setView, commitView, setError, bumpLoad, clearOlder } = opts
  const staying = view().threadId === id && Boolean(view().detail)
  const request = {}
  openings.set(target, request)
  const still = () => openings.get(target) === request && view().threadId === id
  try {
    const w = await target.rpc({ op: OpWatch, thread_id: id, include_detail: true })
    if (!still()) return false
    if (!w.ok) {
      setError(w.error || w.code ? remoteError(w.error || w.code || "") : t("err.watch"))
      if (!staying) setView(emptyView())
      return false
    }
    // Older hosts ignore include_detail. Reuse their established watch and
    // apply its snapshot before awaiting the header so later live updates
    // cannot be overwritten by an older ready or lose seq=0 stream fragments.
    let baseline: PhoneView | null = null
    if (!w.detail) {
      commitView(applyPush(view(), { ...w, op: w.op || OpReady }))
      baseline = view()
    }
    const r = w.detail ? w : await target.rpc({ op: OpOpen, thread_id: id })
    if (!still()) return false
    if (!r.detail || r.detail.id !== id) {
      setError(r.error ? remoteError(r.error) : t("err.open"))
      if (id === loadLastThreadId()) clearLastThreadId()
      if (!staying) setView(emptyView())
      return false
    }
    saveLastThreadId(id)
    bumpLoad()
    clearOlder()
    const current = view()
    const next = baseline ? applyPendingOpenDetail(current, r.detail, baseline) : applyOpenDetail(current, r.detail)
    commitView(w.detail ? applyPush(next, { ...w, op: w.op || OpReady }) : next)
    // The live-edge page is bounded by raw events, so a worker-only tail can
    // leave the main transcript empty even while older conversation exists.
    while (still() && view().hasMore && !hasPhoneHistoryContent(view().blocks)) {
      const current = view()
      const before = current.oldestSeq > 0 ? current.oldestSeq : current.lastSeq
      try {
        const older = await target.rpc({ op: OpLog, thread_id: id, before })
        if (!still()) return false
        if (!older.ok) {
          setError(remoteError(older.error || older.code || ""))
          return true
        }
        const latest = view()
        if (!latest.hasMore) return true
        const cursor = latest.oldestSeq > 0 ? latest.oldestSeq : latest.lastSeq
        // A manual page may already have advanced while this read was pending.
        // Its late cursor cannot exhaust or move that newer history window.
        if (before > 0 && cursor > 0 && cursor < before) continue
        commitView(prependOlder(latest, older.events ?? [], Boolean(older.more), older.seq ?? 0))
      } catch (error) {
        if (!still()) return false
        setError(linkError(error))
        return true
      }
    }
    return still()
  } catch (e) {
    if (!still()) return false
    setError(linkError(e))
    if (!staying) setView(emptyView())
    return false
  }
}
