import {
  useCallback,
  useEffect,
  useLayoutEffect,
  useRef,
  type RefObject,
} from "react"

import { readerNearOlderHistory, shouldLoadOlderHistory } from "@/lib/thread-log"
import { TURN_NAV_ATTR, offsetInScroller } from "@/lib/turn-nav"

/** Load older history when the live edge does not fill the pane, or when
 *  the reader reaches the top sentinel. Prepending must not jump the
 *  viewport — the height delta is added to scrollTop while unpinned. */
export function useHistoryWindow({
  scrollerRef,
  loaded,
  hasMore,
  loading,
  pinned,
  growthKey,
  loadOlder,
}: {
  scrollerRef: RefObject<HTMLDivElement | null>
  loaded: boolean
  hasMore: boolean
  loading: boolean
  pinned: boolean
  growthKey: string
  loadOlder: (clientHeight?: number) => Promise<void>
}) {
  const sentinelRef = useRef<HTMLDivElement>(null)
  const heightBefore = useRef(0)
  const wasPinned = useRef(pinned)

  const requestOlder = useCallback(
    (opts?: { fromSentinel?: boolean; fromScroll?: boolean }) => {
      const el = scrollerRef.current
      if (!el || !hasMore || loading) return
      // The sentinel's rootMargin is 80px. IntersectionObserver fires once
      // on enter; a scrollTop<48 gate ate that shot, then dragging to 0
      // did nothing because the sentinel never left the root.
      // Scroll uses a wider lead: the oldest loaded turn-nav row on screen
      // is already the end of this page, even when scrollTop is still huge.
      const pixel = shouldLoadOlderHistory(
        hasMore,
        loading,
        el.scrollTop,
        el.scrollHeight,
        el.clientHeight,
      )
      const near =
        Boolean(opts?.fromScroll) &&
        !pinned &&
        readerNearOlderHistory(
          el.scrollTop,
          el.scrollHeight,
          el.clientHeight,
          oldestTurnFromViewportTop(el),
        )
      if (!opts?.fromSentinel && !pixel && !near) return
      heightBefore.current = el.scrollTop > 48 ? el.scrollHeight : 0
      void loadOlder(el.clientHeight)
    },
    [hasMore, loading, loadOlder, pinned, scrollerRef],
  )

  useLayoutEffect(() => {
    const el = scrollerRef.current
    if (!el || !heightBefore.current || pinned) {
      heightBefore.current = 0
      return
    }
    // Fetch started a few pixels from the top; the reader may already be
    // parked at 0. Adding the delta would throw them back onto the old
    // first row and the "content above" looks missing.
    if (el.scrollTop < 48) {
      heightBefore.current = 0
      return
    }
    el.scrollTop += el.scrollHeight - heightBefore.current
    heightBefore.current = 0
  }, [growthKey, pinned, scrollerRef])

  useEffect(() => {
    if (!loaded) return
    requestOlder()
  }, [loaded, growthKey, requestOlder])

  // The scroll event that leaves the live edge still sees pinned=true.
  // Check once the flag flips, or that gesture never pages.
  useEffect(() => {
    const leftLiveEdge = wasPinned.current && !pinned
    wasPinned.current = pinned
    if (!loaded || !leftLiveEdge) return
    requestOlder({ fromScroll: true })
  }, [loaded, pinned, requestOlder])

  useEffect(() => {
    const el = scrollerRef.current
    if (!el || !loaded) return
    const onScroll = () => requestOlder({ fromScroll: true })
    el.addEventListener("scroll", onScroll, { passive: true })
    return () => el.removeEventListener("scroll", onScroll)
  }, [loaded, requestOlder, scrollerRef])

  useEffect(() => {
    const root = scrollerRef.current
    const sentinel = sentinelRef.current
    if (!root || !sentinel || !loaded || typeof IntersectionObserver === "undefined") {
      return
    }
    const io = new IntersectionObserver(
      (entries) => {
        if (entries.some((e) => e.isIntersecting)) requestOlder({ fromSentinel: true })
      },
      { root, rootMargin: "80px 0px 0px 0px" },
    )
    io.observe(sentinel)
    return () => io.disconnect()
  }, [loaded, hasMore, loading, requestOlder, scrollerRef])

  return sentinelRef
}

/** First user row in the loaded slice. Distance from the viewport top;
 *  negative once that row has scrolled above the pane. */
function oldestTurnFromViewportTop(root: HTMLElement): number | undefined {
  const marker = root.querySelector(`[${TURN_NAV_ATTR}]`)
  if (!(marker instanceof HTMLElement)) return undefined
  return offsetInScroller(marker, root) - root.scrollTop
}
