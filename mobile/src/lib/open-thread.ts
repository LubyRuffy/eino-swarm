import { t } from "@/lib/i18n"
import type { RemoteLink } from "@/lib/link"
import { linkError, remoteError } from "@/lib/client"
import { OpOpen, OpReady, OpWatch } from "@/lib/rpc"
import { applyOpenDetail, applyPendingOpenDetail, applyPush, emptyView, type PhoneView } from "@/lib/session"
import { clearLastThreadId, loadLastThreadId, saveLastThreadId } from "@/lib/store"

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
  const still = () => view().threadId === id
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
    return true
  } catch (e) {
    if (!still()) return false
    setError(linkError(e))
    if (!staying) setView(emptyView())
    return false
  }
}
