import { useCallback, useLayoutEffect, useRef, type RefObject } from "react"

import { scrollTurnIntoView, scrollTurnToStart } from "./turn-nav"

/** Jump the transcript to a user turn. The rail lists turns the tail page
 *  has not fetched yet; scrolling in the fetch callback used to miss
 *  because React had not committed the row. Instant, or a live token
 *  cancels a smooth scroll mid-flight.
 *
 *  A click that already found its row still has to stay pending: a sentinel
 *  page started at the top prepends without restoring once the jump has
 *  moved scrollTop, and clearing pending made that click look dead. */
export function useTurnJump({
  scrollerRef,
  growthKey,
  threadId,
  unpin,
  loadUntilTurn,
  startId,
}: {
  scrollerRef: RefObject<HTMLElement | null>
  growthKey: string
  threadId?: string
  unpin: () => void
  loadUntilTurn: (turnId: string, clientHeight?: number) => Promise<boolean>
  /** Earliest human turn. That jump opens the top of the log instead of
   *  pinning the bubble over whatever sits above it. */
  startId?: string
}) {
  const pending = useRef<string | null>(null)

  const tryScroll = useCallback(
    (id: string) => {
      const root = scrollerRef.current
      if (!root) return false
      if (startId && id === startId) return scrollTurnToStart(root, id)
      return scrollTurnIntoView(root, id, true)
    },
    [scrollerRef, startId],
  )

  useLayoutEffect(() => {
    pending.current = null
  }, [threadId])

  useLayoutEffect(() => {
    const root = scrollerRef.current
    if (!root) return
    const onWheel = () => {
      pending.current = null
    }
    root.addEventListener("wheel", onWheel, { passive: true })
    return () => root.removeEventListener("wheel", onWheel)
  }, [scrollerRef, threadId])

  useLayoutEffect(() => {
    const id = pending.current
    if (!id) return
    tryScroll(id)
    const view = scrollerRef.current?.ownerDocument.defaultView
    if (!view) return
    // A prepend that started because the jump parked near the top skips
    // scroll compensation. Measuring in this layout can still see the
    // pre-prepend box. Apply again after paint: a later turn re-pins, the
    // earliest turn stays at scrollTop 0 so that prefix stays on screen.
    const raf = view.requestAnimationFrame(() => {
      if (pending.current !== id) return
      tryScroll(id)
    })
    return () => view.cancelAnimationFrame(raf)
  }, [growthKey, tryScroll, scrollerRef])

  return useCallback(
    (id: string) => {
      unpin()
      pending.current = id
      if (tryScroll(id)) return
      void loadUntilTurn(id, scrollerRef.current?.clientHeight).then((found) => {
        if (pending.current !== id) return
        if (!found) {
          pending.current = null
          return
        }
        // Store updated, commit maybe not. Leave pending; layout scrolls.
        tryScroll(id)
      })
    },
    [unpin, loadUntilTurn, tryScroll, scrollerRef],
  )
}
