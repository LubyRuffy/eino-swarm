import { useEffect, useState } from "react"
import type { RemoteLink } from "./link"
import { publishPairedSelectionTranslation } from "./paired-capabilities"
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
        if (r.ok) {
          link.webExecutionSupported = r.web_execution_supported === true
          publishPairedSelectionTranslation(link, r.model_policy?.selection_translation)
        }
        if (r.ok && (r.startup === "restoring" || r.startup === "ready")) {
          const value: EngineStartup = {
            startup: r.startup,
            model_policy: r.model_policy,
            restore: r.restore,
            data_generation: typeof r.data_generation === "number" ? r.data_generation : undefined,
            graph_available: r.graph_available === true || undefined,
            composer_predictions: r.composer_predictions,
          }
          if (r.product_mode === "fofa" || r.product_mode === "local") value.product_mode = r.product_mode
          setSnapshot(previous => previous?.link === link && JSON.stringify(previous.value) === JSON.stringify(value) ? previous : { link, value })
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
