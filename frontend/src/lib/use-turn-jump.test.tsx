import { act, fireEvent, render, screen } from "@testing-library/react"
import { useRef } from "react"
import { describe, expect, it, vi } from "vitest"

import { useTurnJump } from "./use-turn-jump"

function Harness({
  ids,
  loadUntilTurn,
}: {
  ids: string[]
  loadUntilTurn: (id: string, clientHeight?: number) => Promise<boolean>
}) {
  const scrollerRef = useRef<HTMLDivElement>(null)
  const jumpTo = useTurnJump({
    scrollerRef,
    growthKey: ids.join(","),
    unpin: () => undefined,
    loadUntilTurn,
  })
  return (
    <div data-testid="scroller" ref={scrollerRef}>
      {ids.map((id) => (
        <div key={id} data-turn-nav={id}>
          {id}
        </div>
      ))}
      <button type="button" onClick={() => jumpTo("tn_early")}>
        jump
      </button>
    </div>
  )
}

function watchScrollTop(el: HTMLElement) {
  const calls: number[] = []
  let value = el.scrollTop
  Object.defineProperty(el, "scrollTop", {
    configurable: true,
    get: () => value,
    set: (next: number) => {
      value = Number(next)
      calls.push(value)
    },
  })
  return calls
}

describe("useTurnJump", () => {
  it("scrolls a mounted row without paging", () => {
    const loadUntilTurn = vi.fn(async () => false)
    render(<Harness ids={["tn_early", "tn_late"]} loadUntilTurn={loadUntilTurn} />)
    const calls = watchScrollTop(screen.getByTestId("scroller"))
    fireEvent.click(screen.getByRole("button", { name: "jump" }))
    expect(loadUntilTurn).not.toHaveBeenCalled()
    expect(calls.length).toBeGreaterThan(0)
  })

  it("pages then scrolls once the row mounts, not when the fetch resolves", async () => {
    // The store has the user row before React commits it. Scrolling in
    // that then() is a no-op — the rail looks like it ignored the click.
    let finish!: (found: boolean) => void
    const loadUntilTurn = vi.fn(
      () =>
        new Promise<boolean>((resolve) => {
          finish = resolve
        }),
    )
    const view = render(<Harness ids={["tn_late"]} loadUntilTurn={loadUntilTurn} />)
    const calls = watchScrollTop(screen.getByTestId("scroller"))
    fireEvent.click(screen.getByRole("button", { name: "jump" }))
    expect(loadUntilTurn).toHaveBeenCalledTimes(1)
    expect(calls).toEqual([])

    await act(async () => {
      finish(true)
    })
    expect(calls).toEqual([])

    view.rerender(<Harness ids={["tn_early", "tn_late"]} loadUntilTurn={loadUntilTurn} />)
    expect(calls.length).toBeGreaterThan(0)
  })

  it("drops a pending jump when paging never finds the row", async () => {
    let finish!: (found: boolean) => void
    const loadUntilTurn = vi.fn(
      () =>
        new Promise<boolean>((resolve) => {
          finish = resolve
        }),
    )
    const view = render(<Harness ids={["tn_late"]} loadUntilTurn={loadUntilTurn} />)
    const calls = watchScrollTop(screen.getByTestId("scroller"))
    fireEvent.click(screen.getByRole("button", { name: "jump" }))
    await act(async () => {
      finish(false)
    })
    view.rerender(<Harness ids={["tn_early", "tn_late"]} loadUntilTurn={loadUntilTurn} />)
    expect(calls).toEqual([])
  })

  it("does not take its ids from a hardcoded sample", () => {
    const loadUntilTurn = vi.fn(async () => false)
    render(<Harness ids={["tn_early", "tn_late"]} loadUntilTurn={loadUntilTurn} />)
    expect(document.body.textContent).not.toMatch(/notes\.md|summarize|look into this/)
  })

  it("re-applies a jump after older history prepends", () => {
    // Click found the row and cleared pending. A sentinel page that started
    // at the top then prepends without restoring, so the click looks dead
    // unless the jump is still pending for that layout.
    const loadUntilTurn = vi.fn(async () => false)
    const view = render(
      <Harness ids={["tn_early", "tn_late"]} loadUntilTurn={loadUntilTurn} />,
    )
    const calls = watchScrollTop(screen.getByTestId("scroller"))
    fireEvent.click(screen.getByRole("button", { name: "jump" }))
    expect(calls.length).toBe(1)
    calls.length = 0

    view.rerender(
      <Harness ids={["tn_older", "tn_early", "tn_late"]} loadUntilTurn={loadUntilTurn} />,
    )
    expect(calls.length).toBe(1)
  })

  it("stops following a jump once the reader wheels", () => {
    const loadUntilTurn = vi.fn(async () => false)
    const view = render(
      <Harness ids={["tn_early", "tn_late"]} loadUntilTurn={loadUntilTurn} />,
    )
    const calls = watchScrollTop(screen.getByTestId("scroller"))
    fireEvent.click(screen.getByRole("button", { name: "jump" }))
    fireEvent.wheel(screen.getByTestId("scroller"), { deltaY: -40 })
    calls.length = 0

    view.rerender(
      <Harness ids={["tn_older", "tn_early", "tn_late"]} loadUntilTurn={loadUntilTurn} />,
    )
    expect(calls).toEqual([])
  })
})
