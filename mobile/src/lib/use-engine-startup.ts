import { useEffect, useState } from "react"
import type { RemoteLink } from "./link"
import { OpMeta, type EngineStartup } from "./rpc"

/** Read-only startup snapshots never replay a refused or uncertain mutation. */
export function useEngineStartup(link: RemoteLink | null): EngineStartup | undefined {
  const [snapshot, setSnapshot] = useState<{ link: RemoteLink; value: EngineStartup }>()
  useEffect(() => {
    if (!link) return
    let stopped = false, pending = false, unsupported = false
    const read = async () => {
      if (stopped || pending || unsupported || !link.alive() || document.hidden) return
      pending = true
      try {
        const r = await link.rpc({ op: OpMeta }, { dropOnTimeout: false, timeoutMs: 5000 })
        if (stopped) return
        if (r.code === "unknown_op") { unsupported = true; return }
        if (r.ok && (r.startup === "restoring" || r.startup === "ready")) {
          setSnapshot({ link, value: { startup: r.startup, restore: r.restore } })
        }
      } catch {
        // Loss of metadata is not readiness. Keep the last acknowledged state.
      } finally { pending = false }
    }
    void read()
    const timer = window.setInterval(() => void read(), 1000)
    document.addEventListener("visibilitychange", read)
    return () => { stopped = true; window.clearInterval(timer); document.removeEventListener("visibilitychange", read) }
  }, [link])
  return snapshot?.link === link ? snapshot.value : undefined
}
