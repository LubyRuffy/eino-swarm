import { useCallback, useEffect, useRef, type RefObject } from "react"
import { openLink, type RemoteLink } from "./link"
import { gatewayUpgradePending } from "./gateway-link-upgrade"
import { sameLinkAuthority } from "./link-authority"
import { loadSavedLink, type SavedLink } from "./store"

type Options = {
  link: RefObject<RemoteLink | null>; authority: RefObject<SavedLink | null>
  generation: RefObject<number>; unlinking: RefObject<boolean>; retryTimer: RefObject<number>
  recoverRef: RefObject<(force?: boolean) => Promise<void>>
  install: (next: RemoteLink | null) => void
  attach: (next: RemoteLink) => void
  restore: (next: RemoteLink) => Promise<void>
  firstList: (next: RemoteLink) => Promise<void>
  busy: (busy: boolean) => void; reconnecting: (busy: boolean) => void
  error: (error?: string) => void; message: (error: unknown) => string
}

/** One installed transport and one generation-scoped opening operation. */
export function usePhoneLinkLifecycle(options: Options) {
  const latest = useRef(options)
  latest.current = options
  const opening = useRef<{ generation: number } | null>(null)
  const attempts = useRef(0)
  const close = useCallback(() => {
    const o = latest.current
    const previous = o.link.current
    o.link.current = null
    o.authority.current = null
    o.install(null)
    previous?.close()
  }, [])

  const run = useCallback(async (boot: boolean, force = false) => {
    const o = latest.current
    if (o.unlinking.current || opening.current?.generation === o.generation.current) return
    const saved = loadSavedLink()
    if (!saved || gatewayUpgradePending(saved)) { o.busy(false); return }
    if (!boot && !force && o.link.current?.alive() && sameLinkAuthority(saved, o.authority.current)) return
    const operation = { generation: o.generation.current }
    opening.current = operation
    const valid = () => operation.generation === o.generation.current && sameLinkAuthority(saved, loadSavedLink())
    if (boot) o.busy(true)
    else o.reconnecting(true)
    // Retire before dialing: a credential change must never leave two sockets.
    close()
    o.authority.current = saved
    try {
      const next = await openLink(saved)
      if (!valid()) { next.close(); return }
      o.authority.current = saved
      o.link.current = next
      o.attach(next)
      o.install(next)
      attempts.current = 0
      o.error(undefined)
      if (boot) await o.firstList(next)
      else await o.restore(next)
    } catch (error) {
      if (!valid()) return
      if (!o.link.current) o.authority.current = null
      o.error(o.message(error))
      if (!gatewayUpgradePending(saved)) {
        const delay = boot ? 1000 : Math.min(15_000, 1000 * 2 ** Math.min(attempts.current++, 4))
        window.clearTimeout(o.retryTimer.current)
        o.retryTimer.current = window.setTimeout(() => { void o.recoverRef.current() }, delay)
      }
    } finally {
      if (opening.current === operation) {
        opening.current = null
        if (operation.generation === o.generation.current) {
          o.busy(false); o.reconnecting(false)
          // onDisconnect can arrive while firstList/restore still holds this
          // operation. Replay that recovery after the RPC phase has settled.
          if (valid() && o.link.current && !o.link.current.alive() && !gatewayUpgradePending(saved)) {
            window.clearTimeout(o.retryTimer.current)
            o.retryTimer.current = window.setTimeout(() => { void o.recoverRef.current() }, 1000)
          }
        }
      }
    }
  }, [close])
  const boot = useCallback(() => run(true), [run])
  const recover = useCallback((force = false) => run(false, force), [run])
  options.recoverRef.current = recover
  useEffect(() => {
    void boot()
    return () => {
      const o = latest.current
      o.generation.current++
      window.clearTimeout(o.retryTimer.current)
      const previous = o.link.current
      o.link.current = null
      o.authority.current = null
      previous?.close()
    }
  }, [boot])
  return { boot, recover, close }
}
