import { useEffect, useState } from "react"

import { Button } from "@/components/ui/button"
import {
  api,
  dismissDesktopVersion,
  dismissedDesktopVersion,
  type DesktopUpdate,
} from "@/lib/api"
import { desktopShell } from "@/lib/shell"
import { useT } from "@/lib/use-t"

type Phase =
  | { kind: "hidden" }
  | { kind: "checking" }
  | { kind: "current" }
  | { kind: "available"; version: string }
  | { kind: "working" }
  | { kind: "restarting" }
  | { kind: "error"; message: string }

let ask: (() => void) | null = null

/** The app menu calls this. The banner owns the request. */
export function requestDesktopUpdateCheck() {
  ask?.()
}

export function DesktopUpdateBanner() {
  const t = useT()
  const [phase, setPhase] = useState<Phase>({ kind: "hidden" })

  useEffect(() => {
    if (!desktopShell()) return
    let live = true
    const run = (fresh: boolean) => {
      void api.desktopUpdate(fresh).then(
        (result) => {
          if (!live) return
          setPhase(phaseFrom(result, fresh))
        },
        (err: unknown) => {
          if (!live) return
          setPhase({ kind: "error", message: err instanceof Error ? err.message : String(err) })
        },
      )
    }
    ask = () => {
      setPhase({ kind: "checking" })
      run(true)
    }
    run(false)
    return () => {
      live = false
      if (ask) ask = null
    }
  }, [])

  if (phase.kind === "hidden") return null

  const text =
    phase.kind === "checking"
      ? t("update.checking")
      : phase.kind === "current"
        ? t("update.current")
        : phase.kind === "available"
          ? t("update.available", { version: phase.version })
          : phase.kind === "working"
            ? t("update.working")
            : phase.kind === "restarting"
              ? t("update.restarting")
              : phase.message

  const offer = phase.kind === "available" ? phase.version : ""

  return (
    <div
      data-testid="desktop-update"
      className="flex items-center gap-2 border-b border-border bg-card px-3 py-2"
    >
      <p className="min-w-0 flex-1 text-sm" role="status">
        {text}
      </p>
      {offer ? (
        <>
          <Button
            type="button"
            variant="ghost"
            className="h-8 shrink-0 px-3"
            aria-label={t("update.later")}
            onClick={() => {
              dismissDesktopVersion(offer)
              setPhase({ kind: "hidden" })
            }}
          >
            {t("update.later")}
          </Button>
          <Button
            type="button"
            className="h-8 shrink-0 px-3"
            aria-label={t("update.upgrade")}
            onClick={() => {
              setPhase({ kind: "working" })
              void api.installDesktopUpdate(offer).then(
                () => setPhase({ kind: "restarting" }),
                (err: unknown) =>
                  setPhase({
                    kind: "error",
                    message: err instanceof Error ? err.message : String(err),
                  }),
              )
            }}
          >
            {t("update.upgrade")}
          </Button>
        </>
      ) : null}
    </div>
  )
}

export function phaseFrom(result: DesktopUpdate, fresh: boolean): Phase {
  if (result.status === "available" && result.offer?.version) {
    if (!fresh && dismissedDesktopVersion() === result.offer.version) {
      return { kind: "hidden" }
    }
    return { kind: "available", version: result.offer.version }
  }
  if (!fresh) return { kind: "hidden" }
  if (result.status === "current") return { kind: "current" }
  if (result.status === "unsupported") return { kind: "hidden" }
  return { kind: "error", message: result.message || "" }
}
