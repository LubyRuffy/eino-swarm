import type { ComposerExtra } from "@/components/composer"

import { LinkFault, linkError, remoteError } from "./client"
import type { RemoteLink } from "./link"
import { OpSend } from "./rpc"
import type { PhoneView } from "./session"
import { markRunning } from "./session"
import { sendComposed } from "./turn-send"

type TurnDeps = {
  link: () => RemoteLink | null
  view: () => PhoneView
  commit: (view: PhoneView) => void
  fail: (err: unknown) => void
  setError: (message: string) => void
  setPending: (pending: boolean) => void
}

export async function sendPhoneQueue(
  deps: TurnDeps,
  op: string,
  threadId: string,
  followupId?: string,
) {
  const target = deps.link()
  if (!target?.alive()) return
  try {
    const r = await target.rpc({ op, thread_id: threadId, followup_id: followupId })
    if (!r.ok) {
      deps.setError(remoteError(r.error || r.code || ""))
      return
    }
    if (r.followups) deps.commit({ ...deps.view(), followups: r.followups })
  } catch (e) {
    deps.fail(e)
  }
}

export async function sendPhoneTurn(
  deps: TurnDeps,
  op: string,
  threadId: string,
  text: string,
  extra?: ComposerExtra,
) {
  const target = deps.link()
  if (!target?.alive()) {
    deps.setError(linkError(new LinkFault("offline", "network")))
    throw new Error("offline")
  }
  deps.setPending(true)
  try {
    const r = await sendComposed((req) => target.rpc(req), { op, thread_id: threadId, text }, extra)
    if (!r.ok) {
      deps.setError(remoteError(r.error || r.code || ""))
      throw new Error("refused")
    }
    const queued = op === OpSend ? markRunning(deps.view()) : deps.view()
    deps.commit(r.followups ? { ...queued, followups: r.followups } : queued)
  } catch (e) {
    if (e instanceof Error && (e.message === "refused" || e.message === "offline")) throw e
    deps.fail(e)
    throw e
  } finally {
    deps.setPending(false)
  }
}
