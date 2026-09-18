import { act, fireEvent, render, screen } from "@testing-library/react"
import { useRef } from "react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import { useHistoryWindow } from "./use-history-window"

type Observer = {
  callback: IntersectionObserverCallback
  targets: Element[]
}

let observers: Observer[] = []

class FakeIntersectionObserver {
  callback: IntersectionObserverCallback
  targets: Element[] = []

  constructor(callback: IntersectionObserverCallback) {
    this.callback = callback
    observers.push(this)
  }

  observe(target: Element) {
    this.targets.push(target)
  }

  unobserve() {}

  disconnect() {
    observers = observers.filter((row) => row !== this)
  }

  takeRecords(): IntersectionObserverEntry[] {
    return []
  }
}

function Harness({
  loaded = true,
  hasMore,
  loading = false,
  pinned = false,
  growthKey = "1:1",
  loadOlder,
}: {
  loaded?: boolean
  hasMore: boolean
  loading?: boolean
  pinned?: boolean
  growthKey?: string
  loadOlder: (clientHeight?: number) => Promise<void>
}) {
  const scrollerRef = useRef<HTMLDivElement>(null)
  const sentinelRef = useHistoryWindow({
    scrollerRef,
    loaded,
    hasMore,
    loading,
    pinned,
    growthKey,
    loadOlder,
  })
  return (
    <div data-testid="scroller" ref={scrollerRef}>
      {hasMore ? <div data-testid="history-sentinel" ref={sentinelRef} /> : null}
      <div>body</div>
    </div>
  )
}

function mockScroller(
  el: HTMLElement,
  size: { scrollHeight: number; clientHeight: number; scrollTop: number },
) {
  Object.defineProperty(el, "scrollHeight", {
    configurable: true,
    get: () => size.scrollHeight,
  })
  Object.defineProperty(el, "clientHeight", {
    configurable: true,
    get: () => size.clientHeight,
  })
  Object.defineProperty(el, "scrollTop", {
    configurable: true,
    get: () => size.scrollTop,
    set: (value: number) => {
      size.scrollTop = value
    },
  })
}

function fireSentinel(isIntersecting: boolean) {
  for (const observer of observers) {
    observer.callback(
      observer.targets.map((target) => ({
        isIntersecting,
        target,
        time: 0,
        intersectionRatio: isIntersecting ? 1 : 0,
        boundingClientRect: target.getBoundingClientRect(),
        intersectionRect: target.getBoundingClientRect(),
        rootBounds: null,
      })),
      observer as unknown as IntersectionObserver,
    )
  }
}

describe("useHistoryWindow", () => {
  beforeEach(() => {
    observers = []
    vi.stubGlobal("IntersectionObserver", FakeIntersectionObserver)
  })

  afterEach(() => {
    vi.unstubAllGlobals()
  })

  function mountAt(
    loadOlder: (clientHeight?: number) => Promise<void>,
    size: { scrollHeight: number; clientHeight: number; scrollTop: number },
  ) {
    // Start unloaded so the fill-the-pane effect does not fire against jsdom's 0x0 box.
    const view = render(<Harness loaded={false} hasMore loadOlder={loadOlder} />)
    const el = screen.getByTestId("scroller")
    mockScroller(el, size)
    view.rerender(<Harness loaded hasMore loadOlder={loadOlder} />)
    return el
  }

  it("pages when the top sentinel intersects even 80px from the top", () => {
    // IO rootMargin is 80px. The old scrollTop<48 gate swallowed that first
    // callback, then never fired again because the sentinel stayed in view.
    const loadOlder = vi.fn(async () => undefined)
    mountAt(loadOlder, { scrollHeight: 2000, clientHeight: 400, scrollTop: 80 })
    act(() => fireSentinel(true))
    expect(loadOlder).toHaveBeenCalledTimes(1)
    expect(loadOlder).toHaveBeenCalledWith(400)
  })

  it("pages from a scroll to the top after the sentinel already intersected", () => {
    const loadOlder = vi.fn(async () => undefined)
    const el = mountAt(loadOlder, {
      scrollHeight: 2000,
      clientHeight: 400,
      scrollTop: 80,
    })
    act(() => fireSentinel(true))
    loadOlder.mockClear()
    el.scrollTop = 0
    fireEvent.scroll(el)
    expect(loadOlder).toHaveBeenCalledTimes(1)
  })

  it("does not page from a scroll still in the middle of the log", () => {
    const loadOlder = vi.fn(async () => undefined)
    const el = mountAt(loadOlder, {
      scrollHeight: 2000,
      clientHeight: 400,
      scrollTop: 400,
    })
    fireEvent.scroll(el)
    expect(loadOlder).not.toHaveBeenCalled()
  })

  it("leaves the reader on the newly loaded rows when they already reached the top", () => {
    const loadOlder = vi.fn(async () => undefined)
    const size = { scrollHeight: 2000, clientHeight: 400, scrollTop: 80 }
    const view = render(
      <Harness loaded={false} hasMore growthKey="1" loadOlder={loadOlder} />,
    )
    const el = screen.getByTestId("scroller")
    mockScroller(el, size)
    view.rerender(<Harness loaded hasMore growthKey="1" loadOlder={loadOlder} />)
    act(() => fireSentinel(true))
    size.scrollTop = 0
    size.scrollHeight = 3500
    view.rerender(<Harness loaded hasMore growthKey="2" loadOlder={loadOlder} />)
    expect(size.scrollTop).toBe(0)
  })

  it("keeps the same row on screen when older history prepends mid-scroll", () => {
    const loadOlder = vi.fn(async () => undefined)
    const size = { scrollHeight: 2000, clientHeight: 400, scrollTop: 80 }
    const view = render(
      <Harness loaded={false} hasMore growthKey="1" loadOlder={loadOlder} />,
    )
    const el = screen.getByTestId("scroller")
    mockScroller(el, size)
    view.rerender(<Harness loaded hasMore growthKey="1" loadOlder={loadOlder} />)
    act(() => fireSentinel(true))
    size.scrollHeight = 3500
    view.rerender(<Harness loaded hasMore growthKey="2" loadOlder={loadOlder} />)
    expect(size.scrollTop).toBe(1580)
  })
})
