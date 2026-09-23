import { useEffect, useRef, useState } from "react"
import { createPortal } from "react-dom"

import { Button } from "@/components/ui/button"
import {
  checkAppVersionNow,
  downloadStatus,
  installUpdate,
  listenDownloadProgress,
  type DownloadProgress,
  type UpdateOffer,
  type VersionCheckResult,
} from "@/lib/app-update"
import { t } from "@/lib/i18n"

type Phase = VersionCheckResult | { status: "checking" }

export function VersionCheck({ onClose }: { onClose: () => void }) {
  const [phase, setPhase] = useState<Phase>({ status: "checking" })
  const [busy, setBusy] = useState(false)
  const [progress, setProgress] = useState<DownloadProgress | null>(null)
  const [installError, setInstallError] = useState<string>()
  const busyRef = useRef(false)
  const onCloseRef = useRef(onClose)
  onCloseRef.current = onClose

  useEffect(() => {
    let live = true
    void checkAppVersionNow().then((result) => {
      if (live) setPhase(result)
    })
    return () => {
      live = false
    }
  }, [])

  // The dialog sits above Add a PC. Android back must close it, not the screen under it.
  useEffect(() => {
    const prev = window.__zwaiAndroidBack
    const hook = () => {
      onCloseRef.current()
      return true
    }
    window.__zwaiAndroidBack = hook
    return () => {
      if (window.__zwaiAndroidBack !== hook) return
      if (prev) window.__zwaiAndroidBack = prev
      else delete window.__zwaiAndroidBack
    }
  }, [])

  const offer = phase.status === "available" ? phase.offer : null
  const download = busy ? downloadStatus(progress) : null
  const statusText = download
    ? download.text
    : phase.status === "checking"
      ? t("update.checking")
      : phase.status === "current"
        ? t("update.current")
        : offer
          ? t("update.ask", { version: offer.version })
          : ""
  const failed = !download && (phase.status === "error" ? phase.message : installError)

  const onUpgrade = () => {
    if (!offer || busyRef.current) return
    busyRef.current = true
    setBusy(true)
    setProgress(null)
    setInstallError(undefined)
    void runUpgrade(offer, setProgress, (message) => {
      busyRef.current = false
      setBusy(false)
      if (message) setInstallError(message)
      else onCloseRef.current()
    })
  }

  // Home scrolls inside overflow-hidden. A fixed sheet kept there gets clipped.
  return createPortal(
    <div className="fixed inset-0 z-50 flex flex-col justify-end">
      <button
        type="button"
        className="absolute inset-0 bg-foreground/40"
        aria-label={t("home.close")}
        onClick={onClose}
      />
      <div
        role="dialog"
        aria-modal="true"
        aria-labelledby="version-check-title"
        aria-busy={phase.status === "checking" || busy}
        className="relative z-10 rounded-t-2xl border border-border bg-background px-5 pb-[env(safe-area-inset-bottom)] pt-4"
      >
        <h2 id="version-check-title" className="text-lg font-semibold tracking-tight">
          {t("update.check")}
        </h2>
        {statusText ? (
          <p className="mt-2 text-sm" role="status">
            {statusText}
          </p>
        ) : null}
        {failed ? (
          <p className="mt-2 text-sm text-destructive" role="alert">
            {failed}
          </p>
        ) : null}
        {phase.status === "checking" || (download && download.percent == null) ? (
          <div
            role="progressbar"
            aria-label={statusText || t("update.checking")}
            className="mt-3 h-1 overflow-hidden rounded-full bg-muted"
          >
            <div className="h-full w-1/3 animate-pulse bg-primary" />
          </div>
        ) : null}
        {download?.percent != null ? (
          <div
            role="progressbar"
            aria-valuemin={0}
            aria-valuemax={100}
            aria-valuenow={download.percent}
            aria-label={download.text}
            className="mt-3 h-1 overflow-hidden rounded-full bg-muted"
          >
            <div className="h-full bg-primary" style={{ width: `${download.percent}%` }} />
          </div>
        ) : null}
        <div className="mt-4 flex justify-end gap-2 pb-4">
          {offer ? (
            <>
              <Button
                type="button"
                variant="ghost"
                disabled={busy}
                aria-label={t("update.later")}
                onClick={onClose}
              >
                {t("update.later")}
              </Button>
              <Button
                type="button"
                disabled={busy}
                aria-label={t("update.upgrade")}
                onClick={onUpgrade}
              >
                {t("update.upgrade")}
              </Button>
            </>
          ) : (
            <Button type="button" variant="outline" aria-label={t("home.close")} onClick={onClose}>
              {t("home.close")}
            </Button>
          )}
        </div>
      </div>
    </div>,
    document.body,
  )
}

async function runUpgrade(
  offer: UpdateOffer,
  onProgress: (progress: DownloadProgress) => void,
  done: (message?: string) => void,
) {
  const stop = await listenDownloadProgress(onProgress)
  try {
    const result = await installUpdate(offer)
    if (result === "permission") done(t("update.permission"))
    else if (result === "failed") done(t("update.failed"))
    else done()
  } finally {
    stop()
  }
}
