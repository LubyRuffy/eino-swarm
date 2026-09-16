import { useRef } from "react"

import { holdSelection } from "@/lib/selection"
import { cn } from "@/lib/utils"

const STEP = 24

/** A full-height strip on one edge of a panel. Dragging moves the border;
 *  the arrow keys move it the same way so a keyboard user is not stuck. */
export function ResizeHandle({
  width,
  onWidthChange,
  onWidthCommit,
  edge,
  label,
  min,
  max,
}: {
  width: number
  onWidthChange: (width: number) => void
  /** Fires when the gesture ends. Live moves go through onWidthChange so a
   *  parent can skip React state (and storage) until the pointer is up. */
  onWidthCommit?: (width: number) => void
  /** Which edge of the panel the strip sits on. Arrow keys move the
   *  separator in that direction, so Left always pushes the border left. */
  edge: "left" | "right"
  label: string
  min: number
  max: number
}) {
  const sign = edge === "right" ? 1 : -1
  // Seeded once: the sidebar does not push width back through React during
  // a drag, and a later parent render must not snap the live value back.
  const latest = useRef(width)
  const clamp = (px: number) => Math.min(max, Math.max(min, Math.round(px)))
  const setLive = (px: number) => {
    const next = clamp(px)
    latest.current = next
    onWidthChange(next)
    return next
  }
  return (
    <div
      role="separator"
      aria-orientation="vertical"
      aria-label={label}
      tabIndex={0}
      onPointerDown={(e) => {
        if (e.button > 0) return
        // Without this the drag is a text selection through the transcript.
        e.preventDefault()
        const release = holdSelection()
        try {
          e.currentTarget.setPointerCapture(e.pointerId)
        } catch {
          // setPointerCapture throws when this event has no active pointer.
        }
        const startX = e.clientX
        const measured = e.currentTarget.parentElement?.getBoundingClientRect().width ?? 0
        const startWidth = measured > 0 ? measured : latest.current
        latest.current = startWidth
        const move = (ev: PointerEvent) =>
          setLive(startWidth + (ev.clientX - startX) * sign)
        const up = () => {
          release()
          onWidthCommit?.(latest.current)
          window.removeEventListener("pointermove", move)
          window.removeEventListener("pointerup", up)
        }
        window.addEventListener("pointermove", move)
        window.addEventListener("pointerup", up)
      }}
      onKeyDown={(e) => {
        if (e.key !== "ArrowLeft" && e.key !== "ArrowRight") return
        const next = setLive(
          latest.current + (e.key === "ArrowRight" ? STEP : -STEP) * sign,
        )
        onWidthCommit?.(next)
      }}
      className={cn(
        "absolute inset-y-0 z-20 w-2 cursor-col-resize select-none touch-none hover:bg-ring/40 focus-visible:bg-ring/60 focus-visible:outline-none",
        edge === "left" ? "-left-1" : "-right-1",
      )}
    />
  )
}
