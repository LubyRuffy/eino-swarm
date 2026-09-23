import { App } from "@capacitor/app"
import { Capacitor } from "@capacitor/core"
import { useEffect, useRef, useState } from "react"

import { Button } from "@/components/ui/button"
import {
  checkForAppUpdate,
  dismissUpdate,
  downloadStatus,
  installUpdate,
  listenDownloadProgress,
  type DownloadProgress,
  type UpdateOffer,
} from "@/lib/app-update"
import { t } from "@/lib/i18n"

export function UpdateNotice() {
  const [offer, setOffer] = useState<UpdateOffer | null>(null)
  const [busy, setBusy] = useState(false)
  const [progress, setProgress] = useState<DownloadProgress | null>(null)
  const [error, setError] = useState<string>()
  const seen = useRef(0)
  const busyRef = useRef(false)

  useEffect(() => {
    let live = true
    const run = () => {
      const id = ++seen.current
      void checkForAppUpdate().then((next) => {
        if (!live || id !== seen.current) return
        setOffer(next)
        setError(undefined)
      })
    }
    run()
    if (Capacitor.getPlatform() === "web") {
      return () => {
        live = false
      }
    }
    const handle = App.addListener("appStateChange", (state) => {
      if (state.isActive) run()
    })
    return () => {
      live = false
      void handle.then((h) => h.remove())
    }
  }, [])

  if (!offer) return null

  const status = busy ? downloadStatus(progress) : null

  const onUpgrade = () => {
    if (busyRef.current) return
    busyRef.current = true
    setBusy(true)
    setProgress(null)
    setError(undefined)
    void (async () => {
      const stop = await listenDownloadProgress(setProgress)
      try {
        const result = await installUpdate(offer)
        if (result === "permission") setError(t("update.permission"))
        else if (result === "failed") setError(t("update.failed"))
      } finally {
        stop()
        busyRef.current = false
        setBusy(false)
      }
    })()
  }

  const onLater = () => {
    seen.current += 1
    dismissUpdate(offer.version)
    setOffer(null)
    setError(undefined)
  }

  return (
    <div data-testid="app-update" className="flex flex-col gap-2 border-b border-border bg-card px-4 py-2">
      <div className="flex items-center gap-2">
        <p className="min-w-0 flex-1 text-sm" role="status">
          {status
            ? status.text
            : error
              ? error
              : t("update.available", { version: offer.version })}
        </p>
        <Button
          type="button"
          variant="ghost"
          className="h-8 shrink-0 px-3"
          aria-label={t("update.later")}
          disabled={busy}
          onClick={onLater}
        >
          {t("update.later")}
        </Button>
        <Button
          type="button"
          className="h-8 shrink-0 px-3"
          aria-label={t("update.upgrade")}
          disabled={busy}
          onClick={onUpgrade}
        >
          {t("update.upgrade")}
        </Button>
      </div>
      {status?.percent != null ? (
        <div
          role="progressbar"
          aria-valuemin={0}
          aria-valuemax={100}
          aria-valuenow={status.percent}
          aria-label={status.text}
          className="h-1 overflow-hidden rounded-full bg-muted"
        >
          <div className="h-full bg-primary" style={{ width: `${status.percent}%` }} />
        </div>
      ) : null}
    </div>
  )
}
