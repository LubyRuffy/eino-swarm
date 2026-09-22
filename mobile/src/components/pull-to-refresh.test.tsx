import { act, fireEvent, render, screen } from "@testing-library/react"
import { describe, expect, it, vi } from "vitest"

import { PullToRefresh } from "./pull-to-refresh"

function scroller() {
  return screen.getByTestId("inbox-scroller")
}

function pull(el: HTMLElement, distance: number) {
  Object.defineProperty(el, "scrollTop", { value: 0, configurable: true })
  fireEvent.touchStart(el, { touches: [{ clientY: 0 }] })
  fireEvent.touchMove(el, { touches: [{ clientY: distance }] })
}

describe("PullToRefresh", () => {
  it("reloads when the finger drags far enough past the top", async () => {
    const onRefresh = vi.fn().mockResolvedValue(undefined)
    render(
      <PullToRefresh onRefresh={onRefresh}>
        <p>rows</p>
      </PullToRefresh>,
    )
    // Travel is halved by the resistance, so this is just past the trigger.
    pull(scroller(), 160)
    await act(async () => {
      fireEvent.touchEnd(scroller())
    })
    expect(onRefresh).toHaveBeenCalledTimes(1)
    expect(scroller().style.transform).toBe("")
  })

  // A short tug is how a list is scrolled, not how it is reloaded.
  it("ignores a tug that never reaches the trigger", async () => {
    const onRefresh = vi.fn().mockResolvedValue(undefined)
    render(
      <PullToRefresh onRefresh={onRefresh}>
        <p>rows</p>
      </PullToRefresh>,
    )
    pull(scroller(), 40)
    await act(async () => {
      fireEvent.touchEnd(scroller())
    })
    expect(onRefresh).not.toHaveBeenCalled()
  })

  it("does not arm mid-list, where the drag is a scroll", async () => {
    const onRefresh = vi.fn().mockResolvedValue(undefined)
    render(
      <PullToRefresh onRefresh={onRefresh}>
        <p>rows</p>
      </PullToRefresh>,
    )
    const el = scroller()
    Object.defineProperty(el, "scrollTop", { value: 300, configurable: true })
    fireEvent.touchStart(el, { touches: [{ clientY: 0 }] })
    fireEvent.touchMove(el, { touches: [{ clientY: 400 }] })
    await act(async () => {
      fireEvent.touchEnd(el)
    })
    expect(onRefresh).not.toHaveBeenCalled()
  })

  it("stops following the finger past the cap", () => {
    render(
      <PullToRefresh onRefresh={vi.fn()}>
        <p>rows</p>
      </PullToRefresh>,
    )
    pull(scroller(), 2000)
    expect(scroller().style.transform).toBe("translateY(96px)")
  })

  it("is an ordinary scroller when the screen cannot reload", async () => {
    render(
      <PullToRefresh>
        <p>rows</p>
      </PullToRefresh>,
    )
    pull(scroller(), 200)
    await act(async () => {
      fireEvent.touchEnd(scroller())
    })
    expect(scroller().style.transform).toBe("")
    expect(screen.getByText("rows")).toBeInTheDocument()
  })

  it("says it is refreshing while the reload is in flight", async () => {
    let finish = () => {}
    const onRefresh = vi.fn(() => new Promise<void>((r) => (finish = r)))
    render(
      <PullToRefresh onRefresh={onRefresh}>
        <p>rows</p>
      </PullToRefresh>,
    )
    pull(scroller(), 160)
    await act(async () => {
      fireEvent.touchEnd(scroller())
    })
    expect(screen.getByRole("status")).toBeInTheDocument()
    await act(async () => {
      finish()
    })
    expect(screen.queryByRole("status")).not.toBeInTheDocument()
  })
})
