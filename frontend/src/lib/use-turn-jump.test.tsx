import { act, fireEvent, render, screen } from "@testing-library/react"
import { useRef } from "react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import { useTurnJump } from "./use-turn-jump"

const intoView = vi.fn()
const originalScroll = HTMLElement.prototype.scrollIntoView

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

describe("useTurnJump", () => {
  beforeEach(() => {
    intoView.mockReset()
    HTMLElement.prototype.scrollIntoView = intoView
  })

  afterEach(() => {
    HTMLElement.prototype.scrollIntoView = originalScroll
  })

  it("scrolls a mounted row without paging", () => {
    const loadUntilTurn = vi.fn(async () => false)
    render(<Harness ids={["tn_early", "tn_late"]} loadUntilTurn={loadUntilTurn} />)
    fireEvent.click(screen.getByRole("button", { name: "jump" }))
    expect(loadUntilTurn).not.toHaveBeenCalled()
    expect(intoView).toHaveBeenCalledWith(
      expect.objectContaining({ behavior: "auto", block: "start" }),
    )
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
    fireEvent.click(screen.getByRole("button", { name: "jump" }))
    expect(loadUntilTurn).toHaveBeenCalledTimes(1)
    expect(intoView).not.toHaveBeenCalled()

    await act(async () => {
      finish(true)
    })
    expect(intoView).not.toHaveBeenCalled()

    view.rerender(<Harness ids={["tn_early", "tn_late"]} loadUntilTurn={loadUntilTurn} />)
    expect(intoView).toHaveBeenCalledWith(
      expect.objectContaining({ behavior: "auto", block: "start" }),
    )
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
    fireEvent.click(screen.getByRole("button", { name: "jump" }))
    await act(async () => {
      finish(false)
    })
    view.rerender(<Harness ids={["tn_early", "tn_late"]} loadUntilTurn={loadUntilTurn} />)
    expect(intoView).not.toHaveBeenCalled()
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
    fireEvent.click(screen.getByRole("button", { name: "jump" }))
    expect(intoView).toHaveBeenCalledTimes(1)
    intoView.mockClear()

    view.rerender(
      <Harness ids={["tn_older", "tn_early", "tn_late"]} loadUntilTurn={loadUntilTurn} />,
    )
    expect(intoView).toHaveBeenCalledTimes(1)
  })

  it("stops following a jump once the reader wheels", () => {
    const loadUntilTurn = vi.fn(async () => false)
    const view = render(
      <Harness ids={["tn_early", "tn_late"]} loadUntilTurn={loadUntilTurn} />,
    )
    fireEvent.click(screen.getByRole("button", { name: "jump" }))
    fireEvent.wheel(screen.getByTestId("scroller"), { deltaY: -40 })
    intoView.mockClear()

    view.rerender(
      <Harness ids={["tn_older", "tn_early", "tn_late"]} loadUntilTurn={loadUntilTurn} />,
    )
    expect(intoView).not.toHaveBeenCalled()
  })
})
