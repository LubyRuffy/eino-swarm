import { useEffect, useRef, useState } from "react"
import { AlertCircle, Loader2 } from "lucide-react"

import { Button } from "@/components/ui/button"
import { api, dismissDesktopVersion, dismissedDesktopVersion } from "@/lib/api"
import { DesktopUpdateController, watchDesktopUpdates, type DesktopUpdatePhase } from "@/lib/desktop-update"
import { desktopShell } from "@/lib/shell"
import { useT, type Translate } from "@/lib/use-t"
import { cn } from "@/lib/utils"

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
      progress: api.desktopUpdateProgress,
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
  const working = phase.kind === "working"
  const downloading = working && (!phase.progress || phase.progress.stage === "downloading")
  const percent = working && phase.progress && phase.progress.total_bytes > 0
    ? Math.min(100, Math.floor(phase.progress.downloaded_bytes / phase.progress.total_bytes * 100))
    : undefined

  return (
    <div data-testid="desktop-update" className={cn("flex items-center gap-2 border-b border-border bg-card px-3 py-2",
      phase.kind === "error" && "border-destructive/30 bg-destructive/10 text-destructive")}>
      {phase.kind === "error" ? <AlertCircle className="size-4 shrink-0" aria-hidden /> : null}
      {working || phase.kind === "checking" || phase.kind === "restarting"
        ? <Loader2 className="size-4 shrink-0 motion-safe:animate-spin" aria-hidden /> : null}
      <div className="min-w-0 flex-1 space-y-2">
        <p className="break-words text-sm" role={phase.kind === "error" ? "alert" : "status"}>{phaseText(phase, t)}</p>
        {downloading ? (
          <div role="progressbar" aria-label={t("update.progressLabel")}
            aria-valuemin={0} aria-valuemax={100} aria-valuenow={percent}
            className="h-1 overflow-hidden rounded-full bg-muted">
            <div className={cn("h-full bg-primary transition-[width]", percent === undefined && "w-1/3 motion-safe:animate-pulse")}
              style={percent === undefined ? undefined : { width: `${percent}%` }} />
          </div>
        ) : null}
      </div>
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
        <Button type="button" variant="destructive" className="h-8 shrink-0 px-3"
          aria-label={t(phase.version ? "update.retryInstallLabel" : "update.retryLabel")} onClick={() => controller.current?.retry()}>
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
    case "working": {
      const progress = phase.progress
      if (progress?.stage === "checking") return t("update.preparing")
      if (progress?.stage === "installing") return t("update.installing")
      if (progress?.stage === "restarting") return t("update.restarting")
      if (!progress) return t("update.working")
      const downloaded = formatBytes(progress.downloaded_bytes)
      if (progress.total_bytes <= 0) return t("update.downloaded", { downloaded })
      return t("update.downloadProgress", {
        downloaded, total: formatBytes(progress.total_bytes),
        percent: Math.min(100, Math.floor(progress.downloaded_bytes / progress.total_bytes * 100)),
      })
    }
    case "restarting": return t("update.restarting")
    case "unsupported": return phase.message || t("update.unsupported")
    case "error": return phase.message || t("update.failed")
  }
}

function formatBytes(bytes: number): string {
  if (bytes < 1024) return `${bytes} B`
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`
  return `${(bytes / (1024 * 1024)).toFixed(1)} MB`
}
