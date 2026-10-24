import { useEffect } from "react"
import { AlertTriangle, Loader2 } from "lucide-react"
import { api } from "@/lib/api"
import { ENGINE_RESTORING_EVENT } from "@/lib/engine-startup"
import { useT } from "@/lib/use-t"
import { useApp } from "@/store/app"

/** Engine reads and presence stay available while unfinished work is restored. */
export function EngineStartupBanner() {
  const meta = useApp((s) => s.meta)
  const restoring = meta?.startup === "restoring"
  const t = useT()
  useEffect(() => {
    const refused = () => useApp.setState((s) => s.meta ? { meta: { ...s.meta, startup: "restoring" } } : {})
    window.addEventListener(ENGINE_RESTORING_EVENT, refused)
    return () => window.removeEventListener(ENGINE_RESTORING_EVENT, refused)
  }, [])
  useEffect(() => {
    if (!restoring) return
    let stopped = false, pending = false
    const read = async () => {
      if (stopped || pending || document.hidden) return
      pending = true
      try {
        const snapshot = await api.meta()
        if (!stopped) useApp.setState({ meta: snapshot })
      } catch {
        // A dropped read cannot establish readiness or authorize resubmission.
      } finally { pending = false }
    }
    const timer = window.setInterval(() => void read(), 1000)
    document.addEventListener("visibilitychange", read)
    return () => { stopped = true; window.clearInterval(timer); document.removeEventListener("visibilitychange", read) }
  }, [restoring])
  const failed = meta?.restore?.failed ?? 0
  if (!restoring && !failed) return null
  return <div role={restoring ? "status" : "alert"} data-testid="engine-startup" className="flex shrink-0 items-center gap-2 border-b border-border bg-muted px-4 py-2 text-sm text-muted-foreground">
    {restoring ? <Loader2 className="size-4 motion-safe:animate-spin" aria-hidden /> : <AlertTriangle className="size-4" aria-hidden />}
    {restoring ? t("startup.restoring", { completed: meta?.restore?.completed ?? 0, total: meta?.restore?.total ?? 0 }) : t("startup.failed", { failed })}
  </div>
}
