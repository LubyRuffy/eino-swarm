import { useEffect, useMemo, useSyncExternalStore } from "react"
import { composerDraftSession } from "./composer-draft"

const subscribeNone = () => () => {}
const noSnapshot = () => undefined

export function useComposerDraft(key?: string) {
  const session = useMemo(() => key ? composerDraftSession(key) : undefined, [key])
  const snapshot = useSyncExternalStore(session?.subscribe ?? subscribeNone, session?.getSnapshot ?? noSnapshot)
  useEffect(() => { if (session) void session.load().catch(() => undefined) }, [session])
  return { session, snapshot }
}
