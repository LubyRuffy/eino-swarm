import {
  useCallback,
  useEffect,
  useLayoutEffect,
  useRef,
  type RefObject,
} from "react"

import { shouldLoadOlderHistory } from "@/lib/thread-log"

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

  const requestOlder = useCallback(() => {
    const el = scrollerRef.current
    if (
      !el ||
      !shouldLoadOlderHistory(
        hasMore,
        loading,
        el.scrollTop,
        el.scrollHeight,
        el.clientHeight,
      )
    ) {
      return
    }
    heightBefore.current = el.scrollTop > 48 ? el.scrollHeight : 0
    void loadOlder(el.clientHeight)
  }, [hasMore, loading, loadOlder, scrollerRef])

  useLayoutEffect(() => {
    const el = scrollerRef.current
    if (!el || !heightBefore.current || pinned) {
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

  useEffect(() => {
    const root = scrollerRef.current
    const sentinel = sentinelRef.current
    if (!root || !sentinel || !loaded || typeof IntersectionObserver === "undefined") {
      return
    }
    const io = new IntersectionObserver(
      (entries) => {
        if (entries.some((e) => e.isIntersecting)) requestOlder()
      },
      { root, rootMargin: "80px 0px 0px 0px" },
    )
    io.observe(sentinel)
    return () => io.disconnect()
  }, [loaded, hasMore, loading, requestOlder, scrollerRef])

  return sentinelRef
}
