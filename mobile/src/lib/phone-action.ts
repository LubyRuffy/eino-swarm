import { remoteError } from "./client"
import type { RemoteLink } from "./link"
import { OpCancelWait, OpOpen } from "./rpc"
import { markRunning, type PhoneView } from "./session"

type Deps = {
  link: () => RemoteLink | null
  view: () => PhoneView
  commit: (view: PhoneView) => void
  fail: (err: unknown) => void
  setError: (message: string) => void
}
/** A refused action cannot mark work running. Replies belong to their original PC/thread. */
export async function runPhoneAction(deps: Deps, op: string, threadId: string) {
  const target = deps.link()
  if (!target?.alive()) return
  try {
    const r = await target.rpc({ op, thread_id: threadId })
    if (deps.link() !== target || deps.view().threadId !== threadId) return
    if (!r.ok) { deps.setError(remoteError(r.error || r.code || "")); return }
    if (op !== OpCancelWait) { deps.commit(markRunning(deps.view())); return }
    const opened = await target.rpc({ op: OpOpen, thread_id: threadId })
    if (deps.link() !== target || deps.view().threadId !== threadId) return
    if (opened.ok && opened.detail) deps.commit({ ...deps.view(), detail: opened.detail })
  } catch (e) { deps.fail(e) }
}
