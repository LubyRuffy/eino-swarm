import { useCallback, useLayoutEffect, useRef, type RefObject } from "react"

import { scrollTurnIntoView } from "./turn-nav"

/** Jump the transcript to a user turn. The rail lists turns the tail page
 *  has not fetched yet; scrolling in the fetch callback used to miss
 *  because React had not committed the row. Instant, or a live token
 *  cancels a smooth scroll mid-flight. */
export function useTurnJump({
  scrollerRef,
  growthKey,
  unpin,
  loadUntilTurn,
}: {
  scrollerRef: RefObject<HTMLElement | null>
  growthKey: string
  unpin: () => void
  loadUntilTurn: (turnId: string, clientHeight?: number) => Promise<boolean>
}) {
  const pending = useRef<string | null>(null)

  const tryScroll = useCallback(
    (id: string) => {
      const root = scrollerRef.current
      return Boolean(root && scrollTurnIntoView(root, id, true))
    },
    [scrollerRef],
  )

  useLayoutEffect(() => {
    const id = pending.current
    if (!id) return
    if (tryScroll(id)) pending.current = null
  }, [growthKey, tryScroll])

  return useCallback(
    (id: string) => {
      unpin()
      pending.current = id
      if (tryScroll(id)) {
        pending.current = null
        return
      }
      void loadUntilTurn(id, scrollerRef.current?.clientHeight).then((found) => {
        if (pending.current !== id) return
        if (!found) {
          pending.current = null
          return
        }
        // Store updated, commit maybe not. Leave pending; layout scrolls.
        if (tryScroll(id)) pending.current = null
      })
    },
    [unpin, loadUntilTurn, tryScroll, scrollerRef],
  )
}
