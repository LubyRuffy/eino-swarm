import { App } from "@capacitor/app"
import { Capacitor } from "@capacitor/core"
import { useEffect, useRef, useState } from "react"

import { Button } from "@/components/ui/button"
import {
  checkForAppUpdate,
  dismissUpdate,
  installUpdate,
  type UpdateOffer,
} from "@/lib/app-update"
import { t } from "@/lib/i18n"

export function UpdateNotice() {
  const [offer, setOffer] = useState<UpdateOffer | null>(null)
  const [busy, setBusy] = useState(false)
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

  const onUpgrade = () => {
    if (busyRef.current) return
    busyRef.current = true
    setBusy(true)
    setError(undefined)
    void installUpdate(offer).then((result) => {
      busyRef.current = false
      setBusy(false)
      if (result === "permission") setError(t("update.permission"))
      else if (result === "failed") setError(t("update.failed"))
    })
  }

  const onLater = () => {
    seen.current += 1
    dismissUpdate(offer.version)
    setOffer(null)
    setError(undefined)
  }

  return (
    <div
      data-testid="app-update"
      className="flex items-center gap-2 border-b border-border bg-card px-4 py-2"
    >
      <p className="min-w-0 flex-1 text-sm" role="status">
        {busy
          ? t("update.downloading")
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
  )
}
