import { afterEach, describe, expect, it, vi } from "vitest"

import { THREAD_LIST_SYNC_MS, startSidebarSync } from "./sidebar-sync"

afterEach(() => {
  vi.useRealTimers()
  vi.unstubAllGlobals()
})

describe("startSidebarSync", () => {
  it("re-reads the listing on an interval while the window is visible", () => {
    vi.useFakeTimers()
    const refresh = vi.fn()
    const stop = startSidebarSync(refresh)
    expect(refresh).not.toHaveBeenCalled()
    vi.advanceTimersByTime(THREAD_LIST_SYNC_MS - 1)
    expect(refresh).not.toHaveBeenCalled()
    vi.advanceTimersByTime(1)
    expect(refresh).toHaveBeenCalledTimes(1)
    vi.advanceTimersByTime(THREAD_LIST_SYNC_MS)
    expect(refresh).toHaveBeenCalledTimes(2)
    stop()
    vi.advanceTimersByTime(THREAD_LIST_SYNC_MS)
    expect(refresh).toHaveBeenCalledTimes(2)
  })

  it("skips a tick while the window is hidden and catches up when it returns", () => {
    vi.useFakeTimers()
    let hidden = true
    vi.stubGlobal("document", {
      get hidden() {
        return hidden
      },
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
    })
    const refresh = vi.fn()
    const stop = startSidebarSync(refresh)
    vi.advanceTimersByTime(THREAD_LIST_SYNC_MS)
    expect(refresh).not.toHaveBeenCalled()
    hidden = false
    vi.advanceTimersByTime(THREAD_LIST_SYNC_MS)
    expect(refresh).toHaveBeenCalledTimes(1)
    stop()
  })

  it("re-reads as soon as the window becomes visible", () => {
    const listeners = new Map<string, EventListener>()
    vi.stubGlobal("document", {
      hidden: true,
      addEventListener: (type: string, fn: EventListener) => {
        listeners.set(type, fn)
      },
      removeEventListener: (type: string) => {
        listeners.delete(type)
      },
    })
    const refresh = vi.fn()
    const stop = startSidebarSync(refresh)
    Object.defineProperty(document, "hidden", { configurable: true, value: false })
    listeners.get("visibilitychange")?.(new Event("visibilitychange"))
    expect(refresh).toHaveBeenCalledTimes(1)
    stop()
    expect(listeners.size).toBe(0)
  })
})
