import { useLayoutEffect, useRef, type RefObject } from "react"

import { isFollowBottom } from "./follow-scroll"

/** Stick a live overflow box to its tail. A long exec that stayed pinned
 *  at the first 15 lines is a terminal that cannot scroll. Wheel-up or
 *  dragging away from the bottom unpins; scrolling back to the edge
 *  re-pins — same contract as the transcript, on this box only. */
export function useOutputTail(
  ref: RefObject<HTMLElement | null>,
  growthKey: string,
  follow: boolean,
) {
  const pinnedRef = useRef(true)
  const ignoreScroll = useRef(false)
  const skipPin = useRef(false)

  useLayoutEffect(() => {
    if (follow) pinnedRef.current = true
  }, [follow])

  useLayoutEffect(() => {
    const el = ref.current
    if (!el || !follow) return
    const onScroll = () => {
      if (ignoreScroll.current) return
      const atBottom = isFollowBottom(
        el.scrollHeight,
        el.scrollTop,
        el.clientHeight,
      )
      if (!atBottom) {
        skipPin.current = false
        pinnedRef.current = false
        return
      }
      if (skipPin.current) return
      pinnedRef.current = true
    }
    const onWheel = (e: WheelEvent) => {
      if (e.deltaY < 0) {
        pinnedRef.current = false
        skipPin.current = true
        return
      }
      skipPin.current = false
    }
    el.addEventListener("scroll", onScroll, { passive: true })
    el.addEventListener("wheel", onWheel, { passive: true })
    return () => {
      el.removeEventListener("scroll", onScroll)
      el.removeEventListener("wheel", onWheel)
    }
  }, [follow, ref])

  useLayoutEffect(() => {
    const el = ref.current
    if (!el || !follow || !pinnedRef.current) return
    ignoreScroll.current = true
    el.scrollTop = el.scrollHeight
    ignoreScroll.current = false
  }, [growthKey, follow, ref])
}
