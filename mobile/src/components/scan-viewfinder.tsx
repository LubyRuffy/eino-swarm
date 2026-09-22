import { useEffect, useRef } from "react"

import { Button } from "@/components/ui/button"
import { t } from "@/lib/i18n"
import { playScanChime } from "@/lib/scan-chime"
import { pairlinkFromQR, scanCameraMessage, startLiveScan } from "@/lib/scan"

export function ScanViewfinder({
  onClose,
  onURI,
  onError,
}: {
  onClose: () => void
  onURI: (uri: string) => void
  onError: (message: string) => void
}) {
  const videoRef = useRef<HTMLVideoElement>(null)
  const onCloseRef = useRef(onClose)
  const onURIRef = useRef(onURI)
  const onErrorRef = useRef(onError)
  onCloseRef.current = onClose
  onURIRef.current = onURI
  onErrorRef.current = onError

  useEffect(() => {
    const video = videoRef.current
    if (!video) return
    // Parent renders pass new callbacks. Restarting here would drop the preview.
    let stop = () => {}
    let gone = false
    const finish = () => {
      if (gone) return
      gone = true
      stop()
    }
    void startLiveScan({
      video,
      onText: (text) => {
        if (gone) return
        const uri = pairlinkFromQR(text)
        if (!uri) return
        finish()
        playScanChime()
        onURIRef.current(uri)
      },
    }).then(
      (handle) => {
        stop = () => handle.stop()
        if (gone) handle.stop()
      },
      (err: unknown) => {
        if (gone) return
        gone = true
        onErrorRef.current(scanCameraMessage(err))
      },
    )
    return () => {
      finish()
    }
  }, [])

  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      if (event.key === "Escape") onCloseRef.current()
    }
    window.addEventListener("keydown", onKey)
    return () => window.removeEventListener("keydown", onKey)
  }, [])

  return (
    <div role="dialog" aria-modal="true" aria-label={t("scan.camera")} className="scan-stage fixed inset-0 z-50">
      <video
        ref={videoRef}
        className="pointer-events-none absolute inset-0 h-full w-full object-cover"
        autoPlay
        muted
        playsInline
        aria-hidden
      />
      <div className="relative flex h-full flex-col">
        <div className="scan-shade min-h-16 flex-1 pt-[env(safe-area-inset-top)]" />
        <div className="flex">
          <div className="scan-shade min-w-6 flex-1" />
          <div className="scan-frame" aria-hidden>
            <span className="scan-corner scan-corner-tl" />
            <span className="scan-corner scan-corner-tr" />
            <span className="scan-corner scan-corner-bl" />
            <span className="scan-corner scan-corner-br" />
            <span className="scan-beam" />
          </div>
          <div className="scan-shade min-w-6 flex-1" />
        </div>
        <div className="scan-shade flex min-h-0 flex-1 flex-col items-center px-6 pt-5">
          <p id="scan-aim" className="scan-aim text-center text-sm leading-relaxed">
            {t("scan.aim")}
          </p>
          <div className="mt-auto pb-[max(1.25rem,env(safe-area-inset-bottom))] pt-4">
            <Button type="button" variant="outline" className="scan-close" autoFocus onClick={() => onCloseRef.current()}>
              {t("scan.close")}
            </Button>
          </div>
        </div>
      </div>
    </div>
  )
}
