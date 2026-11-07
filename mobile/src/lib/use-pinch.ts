import { useLayoutEffect, useRef, type RefObject } from "react"

export type PinchPoint = { x: number; y: number; distance: number }
type PinchMove = (point: PinchPoint) => void
type PinchStart = (point: PinchPoint) => PinchMove | undefined

function geometry(touches: TouchList): PinchPoint {
  const first = touches[0]
  const second = touches[1]
  return {
    x: (first.clientX + second.clientX) / 2,
    y: (first.clientY + second.clientY) / 2,
    distance: Math.hypot(first.clientX - second.clientX, first.clientY - second.clientY),
  }
}

export function usePinch(ref: RefObject<HTMLElement | null>, onStart: PinchStart, enabled = true) {
  const latest = useRef(onStart)
  useLayoutEffect(() => { latest.current = onStart }, [onStart])

  useLayoutEffect(() => {
    const element = ref.current
    if (!element || !enabled) return

    let onMove: PinchMove | undefined
    let claimed = false
    const prevent = (event: TouchEvent) => {
      if (event.cancelable) event.preventDefault()
    }
    const start = (event: TouchEvent) => {
      const { touches } = event
      if (touches.length === 1) {
        onMove = undefined
        claimed = false
      }
      if (claimed) {
        if (touches.length !== 2) onMove = undefined
        prevent(event)
        return
      }
      if (touches.length !== 2 || !Array.from(touches).every(({ target }) => target instanceof Node && element.contains(target))) return
      const point = geometry(touches)
      if (!point.distance) return
      onMove = latest.current(point)
      if (!onMove) return
      claimed = true
      prevent(event)
    }
    const move = (event: TouchEvent) => {
      if (!claimed) return
      prevent(event)
      if (event.touches.length === 2) onMove?.(geometry(event.touches))
      else onMove = undefined
    }
    const end = (event: TouchEvent) => {
      // Consume the last lift too, so a pinch over a tap target cannot click it.
      if (claimed) prevent(event)
      onMove = undefined
      if (event.touches.length === 0 || event.type === "touchcancel") claimed = false
    }

    element.addEventListener("touchstart", start, { passive: false })
    element.addEventListener("touchmove", move, { passive: false })
    element.addEventListener("touchend", end, { passive: false })
    element.addEventListener("touchcancel", end, { passive: false })
    return () => {
      element.removeEventListener("touchstart", start)
      element.removeEventListener("touchmove", move)
      element.removeEventListener("touchend", end)
      element.removeEventListener("touchcancel", end)
    }
  }, [enabled, ref])
}
