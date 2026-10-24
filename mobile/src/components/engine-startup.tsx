import { AlertTriangle, Loader2 } from "lucide-react"
import { t } from "@/lib/i18n"
import type { EngineStartup } from "@/lib/rpc"

export function EngineStartupBanner({ startup }: { startup?: EngineStartup }) {
  const restoring = startup?.startup === "restoring"
  const failed = startup?.restore?.failed ?? 0
  if (!restoring && !failed) return null
  return <div role={restoring ? "status" : "alert"} data-testid="engine-startup" className="flex shrink-0 items-center gap-2 border-b border-border bg-muted px-3 py-2 text-sm text-muted-foreground">
    {restoring ? <Loader2 className="size-4 motion-safe:animate-spin" aria-hidden /> : <AlertTriangle className="size-4" aria-hidden />}
    {restoring ? t("startup.restoring", { completed: startup?.restore?.completed ?? 0, total: startup?.restore?.total ?? 0 }) : t("startup.failed", { failed })}
  </div>
}
