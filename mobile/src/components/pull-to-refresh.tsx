import { useRef, useState, type ReactNode } from "react"
import { RefreshCw } from "lucide-react"

import { cn } from "@/lib/cn"
import { t } from "@/lib/i18n"

/** How far past the top the finger must travel before the list reloads. */
const TRIGGER_PX = 64
/** Past the trigger the sheet stops following the finger, so an overscroll
 *  flick cannot drag the list halfway off the screen. */
const MAX_PULL_PX = 96

export function PullToRefresh({
  onRefresh,
  className,
  children,
}: {
  onRefresh?: () => Promise<void> | void
  className?: string
  children: ReactNode
}) {
  const [pull, setPull] = useState(0)
  const [busy, setBusy] = useState(false)
  const scroller = useRef<HTMLDivElement>(null)
  const from = useRef<number | null>(null)

  const armed = pull >= TRIGGER_PX

  const release = async () => {
    from.current = null
    if (!armed || !onRefresh || busy) {
      setPull(0)
      return
    }
    setBusy(true)
    setPull(TRIGGER_PX)
    try {
      await onRefresh()
    } finally {
      setBusy(false)
      setPull(0)
    }
  }

  return (
    <div className={cn("relative flex min-h-0 flex-col overflow-hidden", className)}>
      <div
        aria-hidden={!busy}
        role={busy ? "status" : undefined}
        className="pointer-events-none absolute inset-x-0 top-0 z-10 flex justify-center overflow-hidden transition-[height] duration-150"
        style={{ height: pull }}
      >
        <RefreshCw
          className={cn(
            "mt-2 size-4 text-muted-foreground",
            busy && "motion-safe:animate-spin",
            armed && !busy && "text-foreground",
          )}
        />
        <span className="sr-only">{busy ? t("home.refreshing") : t("home.pullToRefresh")}</span>
      </div>
      <div
        ref={scroller}
        data-testid="inbox-scroller"
        className="min-h-0 flex-1 overflow-y-auto overscroll-y-contain transition-transform duration-150 [-webkit-overflow-scrolling:touch]"
        style={pull ? { transform: `translateY(${pull}px)` } : undefined}
        onTouchStart={(e) => {
          if (!onRefresh || busy) return
          from.current = (scroller.current?.scrollTop ?? 0) <= 0 ? e.touches[0].clientY : null
        }}
        onTouchMove={(e) => {
          const start = from.current
          if (start == null) return
          // Resistance: the sheet lags the finger, so a real scroll still wins.
          const travel = (e.touches[0].clientY - start) * 0.5
          if (travel <= 0) {
            if (pull) setPull(0)
            return
          }
          setPull(Math.min(travel, MAX_PULL_PX))
        }}
        onTouchEnd={() => void release()}
        onTouchCancel={() => void release()}
      >
        {children}
      </div>
    </div>
  )
}
