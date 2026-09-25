import { t } from "@/lib/i18n"
import type { RemoteLink } from "@/lib/link"
import { linkError, remoteError } from "@/lib/client"
import { OpOpen, OpReady, OpWatch } from "@/lib/rpc"
import { applyOpenDetail, applyPush, emptyView, type PhoneView } from "@/lib/session"
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
    const r = await target.rpc({ op: OpOpen, thread_id: id })
    if (!still()) return false
    if (!r.detail) {
      setError(r.error ? remoteError(r.error) : t("err.open"))
      if (id === loadLastThreadId()) clearLastThreadId()
      if (!staying) setView(emptyView())
      return false
    }
    saveLastThreadId(id)
    bumpLoad()
    clearOlder()
    commitView(applyOpenDetail(view(), r.detail))
    const w = await target.rpc({ op: OpWatch, thread_id: id })
    if (!still()) return false
    if (!w.ok) {
      setError(w.error || w.code ? remoteError(w.error || w.code || "") : t("err.watch"))
      if (!staying) setView(emptyView())
      return false
    }
    commitView(applyPush(view(), { ...w, op: w.op || OpReady }))
    return true
  } catch (e) {
    if (!still()) return false
    setError(linkError(e))
    if (!staying) setView(emptyView())
    return false
  }
}
