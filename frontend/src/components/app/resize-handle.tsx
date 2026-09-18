import { useRef } from "react"

import { holdSelection } from "@/lib/selection"
import { cn } from "@/lib/utils"

const STEP = 24

/** A strip on one edge of a panel. Dragging moves the border; the arrow
 *  keys move it the same way so a keyboard user is not stuck. `width` is
 *  the size along the axis being moved — height for a top/bottom edge. */
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
  edge: "left" | "right" | "top" | "bottom"
  label: string
  min: number
  max: number
}) {
  const vertical = edge === "top" || edge === "bottom"
  const sign = edge === "right" || edge === "bottom" ? 1 : -1
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
      aria-orientation={vertical ? "horizontal" : "vertical"}
      aria-label={label}
      tabIndex={0}
      onPointerDown={(e) => {
        if (e.button > 0) return
        e.preventDefault()
        const release = holdSelection()
        try {
          e.currentTarget.setPointerCapture(e.pointerId)
        } catch {
          // setPointerCapture throws when this event has no active pointer.
        }
        const startX = e.clientX
        const startY = e.clientY
        const box = e.currentTarget.parentElement?.getBoundingClientRect()
        const measured = vertical ? box?.height ?? 0 : box?.width ?? 0
        const startSize = measured > 0 ? measured : latest.current
        latest.current = startSize
        const move = (ev: PointerEvent) => {
          const delta = vertical
            ? (ev.clientY - startY) * (edge === "bottom" ? 1 : -1)
            : (ev.clientX - startX) * sign
          setLive(startSize + delta)
        }
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
        if (vertical) {
          if (e.key !== "ArrowUp" && e.key !== "ArrowDown") return
          const dir = e.key === "ArrowUp" ? 1 : -1
          const next = setLive(
            latest.current + STEP * dir * (edge === "top" ? 1 : -1),
          )
          onWidthCommit?.(next)
          return
        }
        if (e.key !== "ArrowLeft" && e.key !== "ArrowRight") return
        const next = setLive(
          latest.current + (e.key === "ArrowRight" ? STEP : -STEP) * sign,
        )
        onWidthCommit?.(next)
      }}
      className={cn(
        "absolute z-20 select-none touch-none hover:bg-ring/40 focus-visible:bg-ring/60 focus-visible:outline-none",
        vertical
          ? "inset-x-0 h-2 cursor-row-resize"
          : "inset-y-0 w-2 cursor-col-resize",
        edge === "left" && "-left-1",
        edge === "right" && "-right-1",
        edge === "top" && "-top-1",
        edge === "bottom" && "-bottom-1",
      )}
    />
  )
}
