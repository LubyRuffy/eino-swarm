import { useEffect, useRef, useState } from "react"

import { Button } from "@/components/ui/button"
import { api, dismissDesktopVersion, dismissedDesktopVersion } from "@/lib/api"
import { DesktopUpdateController, watchDesktopUpdates, type DesktopUpdatePhase } from "@/lib/desktop-update"
import { desktopShell } from "@/lib/shell"
import { useT, type Translate } from "@/lib/use-t"

let ask: (() => void) | null = null

/** The app menu calls this. The banner owns the request. */
export function requestDesktopUpdateCheck() {
  ask?.()
}

export function DesktopUpdateBanner() {
  const t = useT()
  const [phase, setPhase] = useState<DesktopUpdatePhase>({ kind: "hidden" })
  const controller = useRef<DesktopUpdateController | null>(null)

  useEffect(() => {
    if (!desktopShell()) return
    const updater = new DesktopUpdateController({
      check: api.desktopUpdate,
      install: api.installDesktopUpdate,
      dismissedVersion: dismissedDesktopVersion,
      dismissVersion: dismissDesktopVersion,
      onChange: setPhase,
    })
    controller.current = updater
    const manual = () => updater.checkManually()
    ask = manual
    const stop = watchDesktopUpdates(updater)
    return () => {
      stop()
      if (ask === manual) ask = null
      controller.current = null
    }
  }, [])

  if (phase.kind === "hidden") return null
  const closable = phase.kind === "error" || phase.kind === "unsupported" || phase.kind === "current"

  return (
    <div data-testid="desktop-update" className="flex items-center gap-2 border-b border-border bg-card px-3 py-2">
      <p className="min-w-0 flex-1 text-sm" role="status">{phaseText(phase, t)}</p>
      {phase.kind === "available" ? (
        <>
          <Button type="button" variant="ghost" className="h-8 shrink-0 px-3"
            aria-label={t("update.later")} onClick={() => controller.current?.dismiss()}>
            {t("update.later")}
          </Button>
          <Button type="button" className="h-8 shrink-0 px-3"
            aria-label={t("update.upgrade")} onClick={() => controller.current?.install()}>
            {t("update.upgrade")}
          </Button>
        </>
      ) : null}
      {phase.kind === "error" ? (
        <Button type="button" className="h-8 shrink-0 px-3"
          aria-label={t("update.retryLabel")} onClick={() => controller.current?.checkManually()}>
          {t("update.retry")}
        </Button>
      ) : null}
      {closable ? (
        <Button type="button" variant="ghost" className="h-8 shrink-0 px-3"
          aria-label={t("update.closeLabel")} onClick={() => controller.current?.close()}>
          {t("update.close")}
        </Button>
      ) : null}
    </div>
  )
}

function phaseText(phase: DesktopUpdatePhase, t: Translate): string {
  switch (phase.kind) {
    case "hidden": return ""
    case "checking": return t("update.checking")
    case "current": return t("update.current")
    case "available": return t("update.available", { version: phase.version })
    case "working": return t("update.working")
    case "restarting": return t("update.restarting")
    case "unsupported": return phase.message || t("update.unsupported")
    case "error": return phase.message || t("update.failed")
  }
}
