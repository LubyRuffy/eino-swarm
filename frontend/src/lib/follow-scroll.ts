import {
  useCallback,
  useLayoutEffect,
  useRef,
  useState,
  type RefObject,
} from "react"

/** How close to the live edge still counts as "at the bottom".
 *  A generous slack plus a token-by-token follow traps the reader:
 *  they can never get far enough away before the next token yanks
 *  them back. Keep this tight; wheel-up unpins even inside it. */
export const FOLLOW_BOTTOM_PX = 24

export type FollowState = {
  pinned: boolean
  /** New content arrived while the reader was away from the live edge. */
  unread: boolean
}

export const INITIAL_FOLLOW: FollowState = { pinned: true, unread: false }

export type FollowEvent =
  | { type: "reset" }
  | { type: "jump" }
  | { type: "user-up" }
  | { type: "user-scroll"; atBottom: boolean }
  | { type: "content" }

export function isFollowBottom(
  scrollHeight: number,
  scrollTop: number,
  clientHeight: number,
  slack = FOLLOW_BOTTOM_PX,
): boolean {
  return scrollHeight - scrollTop - clientHeight <= slack
}

export function shouldShowJump(state: FollowState): boolean {
  return !state.pinned && state.unread
}

export function reduceFollow(state: FollowState, event: FollowEvent): FollowState {
  switch (event.type) {
    case "reset":
    case "jump":
      if (state.pinned && !state.unread) return state
      return { pinned: true, unread: false }
    case "user-up":
      if (!state.pinned) return state
      return { pinned: false, unread: state.unread }
    case "user-scroll":
      if (event.atBottom) {
        if (state.pinned && !state.unread) return state
        return { pinned: true, unread: false }
      }
      if (!state.pinned) return state
      return { pinned: false, unread: state.unread }
    case "content":
      if (state.pinned || state.unread) return state
      return { pinned: false, unread: true }
  }
}

/** True when a wheel/scroll started inside a nested overflow box (a live
 *  thought, a tool payload). Those gestures must not unpin the transcript. */
export function eventIsFromNestedScroller(
  target: EventTarget | null,
  root: HTMLElement,
): boolean {
  let node: HTMLElement | null =
    target instanceof HTMLElement
      ? target
      : target instanceof Node
        ? target.parentElement
        : null
  while (node && node !== root) {
    if (node.scrollHeight - node.clientHeight > 1) return true
    node = node.parentElement
  }
  return false
}

/** Own the live edge: follow while the reader is there, freeze the moment
 *  they wheel up, and only then offer a jump once new tokens have landed.
 *  The listener has to re-bind when `loaded` flips — the first paint is a
 *  skeleton with no scroller, and an empty-deps effect would never see it. */
export function useTranscriptFollow({
  scrollerRef,
  loaded,
  threadId,
  growthKey,
  lastUserId,
}: {
  scrollerRef: RefObject<HTMLDivElement | null>
  loaded: boolean
  threadId?: string
  growthKey: string
  lastUserId?: string
}) {
  const [follow, setFollow] = useState(INITIAL_FOLLOW)
  const followRef = useRef(follow)
  followRef.current = follow
  const ignoreScroll = useRef(false)
  const followRaf = useRef(0)
  const openingRaf = useRef(0)
  const openingRef = useRef(true)
  const seenUser = useRef<string | undefined>(undefined)

  const dispatchFollow = useCallback((event: FollowEvent) => {
    const next = reduceFollow(followRef.current, event)
    if (next === followRef.current) return
    followRef.current = next
    setFollow(next)
  }, [])

  const scrollToLatest = useCallback(() => {
    const el = scrollerRef.current
    if (!el) return
    ignoreScroll.current = true
    el.scrollTop = el.scrollHeight
    requestAnimationFrame(() => {
      ignoreScroll.current = false
    })
  }, [scrollerRef])

  const unpin = useCallback(() => {
    dispatchFollow({ type: "user-up" })
    if (followRaf.current) window.cancelAnimationFrame(followRaf.current)
  }, [dispatchFollow])

  const jumpToLatest = useCallback(() => {
    dispatchFollow({ type: "jump" })
    if (followRaf.current) window.cancelAnimationFrame(followRaf.current)
    scrollToLatest()
  }, [dispatchFollow, scrollToLatest])

  useLayoutEffect(() => {
    dispatchFollow({ type: "reset" })
    openingRef.current = true
    seenUser.current = undefined
  }, [threadId, dispatchFollow])

  useLayoutEffect(() => {
    if (seenUser.current === lastUserId) return
    const first = seenUser.current === undefined
    seenUser.current = lastUserId
    if (!lastUserId || first) return
    dispatchFollow({ type: "jump" })
  }, [lastUserId, dispatchFollow])

  useLayoutEffect(() => {
    const el = scrollerRef.current
    if (!el) return
    openingRef.current = true
    const onScroll = () => {
      // A freshly mounted overflow box fires scroll at 0 when it first
      // lays out. Treating that as the reader leaving pins the history
      // at the top and never offers Jump to latest — no unread landed.
      if (ignoreScroll.current || openingRef.current) return
      dispatchFollow({
        type: "user-scroll",
        atBottom: isFollowBottom(el.scrollHeight, el.scrollTop, el.clientHeight),
      })
    }
    const onWheel = (e: WheelEvent) => {
      if (e.deltaY >= 0) return
      if (eventIsFromNestedScroller(e.target, el)) return
      unpin()
    }
    el.addEventListener("scroll", onScroll, { passive: true })
    el.addEventListener("wheel", onWheel, { passive: true })
    let ro: ResizeObserver | undefined
    const inner = el.firstElementChild
    if (typeof ResizeObserver !== "undefined") {
      ro = new ResizeObserver(() => {
        if (followRef.current.pinned) scrollToLatest()
      })
      // The viewport shrinking (flex settling) does not resize the inner
      // column, so observe both or a switch lands at the top of a now-scrollable box.
      ro.observe(el)
      if (inner) ro.observe(inner)
    }
    if (followRef.current.pinned) scrollToLatest()
    openingRaf.current = window.requestAnimationFrame(() => {
      openingRaf.current = 0
      openingRef.current = false
      if (followRef.current.pinned) scrollToLatest()
    })
    return () => {
      el.removeEventListener("scroll", onScroll)
      el.removeEventListener("wheel", onWheel)
      ro?.disconnect()
      if (openingRaf.current) window.cancelAnimationFrame(openingRaf.current)
    }
  }, [loaded, threadId, dispatchFollow, unpin, scrollToLatest, scrollerRef])

  useLayoutEffect(() => {
    dispatchFollow({ type: "content" })
    if (!followRef.current.pinned) return
    const el = scrollerRef.current
    if (!el) return
    // scrollTop is a layout write we already owe; scrollIntoView would walk
    // the ancestor chain and force a second one per token.
    scrollToLatest()
    followRaf.current = window.requestAnimationFrame(() => {
      if (!followRef.current.pinned) return
      scrollToLatest()
    })
    return () => window.cancelAnimationFrame(followRaf.current)
  }, [growthKey, loaded, dispatchFollow, scrollToLatest, scrollerRef])

  return { showJump: shouldShowJump(follow), jumpToLatest, unpin }
}
