import { fireEvent, render, screen } from "@testing-library/react"
import { describe, expect, it, vi } from "vitest"

import { setLocale } from "@/lib/i18n"
import { InboxRow } from "./inbox-row"

describe("InboxRow", () => {
  it("says what a row needs before it is read", () => {
    setLocale("en")
    render(
      <InboxRow id="t1" title="a thread" detail="one line" state="running" onOpen={vi.fn()} />,
    )
    const badge = screen.getByTestId("row-state")
    expect(badge).toHaveTextContent("Running")
    expect(badge).toHaveAttribute("data-state", "running")
    expect(screen.getByText("one line")).toBeInTheDocument()
    const row = screen.getByRole("button", { name: "Open a thread" })
    expect(row).toHaveClass("py-2")
    expect(row).not.toHaveClass("min-h-16")
  })

  // A question is the one state that costs the user a turn if it is missed,
  // so it is filled, not tinted like the rest.
  it("separates a pending question from a wait and from a run", () => {
    setLocale("en")
    const { rerender } = render(
      <InboxRow id="t1" title="a thread" state="ask" onOpen={vi.fn()} />,
    )
    expect(screen.getByTestId("row-state")).toHaveTextContent("Waiting for an answer")
    rerender(<InboxRow id="t1" title="a thread" state="waiting" onOpen={vi.fn()} />)
    expect(screen.getByTestId("row-state")).toHaveTextContent("Waiting")
    rerender(<InboxRow id="t1" title="a thread" state="idle" onOpen={vi.fn()} />)
    expect(screen.queryByTestId("row-state")).not.toBeInTheDocument()
  })

  it("dates an idle row so a stale thread is not mistaken for today's", () => {
    setLocale("en")
    const at = new Date(Date.now() - 3 * 3_600_000).toISOString()
    render(<InboxRow id="t1" title="a thread" state="idle" at={at} onOpen={vi.fn()} />)
    expect(screen.getByText("3h ago")).toBeInTheDocument()
  })

  it("opens the thread it names", () => {
    setLocale("en")
    const onOpen = vi.fn()
    render(<InboxRow id="t7" title="a thread" state="idle" onOpen={onOpen} />)
    fireEvent.click(screen.getByRole("button", { name: "Open a thread" }))
    expect(onOpen).toHaveBeenCalledWith("t7")
  })

  it("keeps a long title and a long line on one row each", () => {
    setLocale("en")
    render(
      <InboxRow
        id="t1"
        title={"t".repeat(160)}
        detail={"d".repeat(160)}
        state="idle"
        onOpen={vi.fn()}
      />,
    )
    expect(screen.getByText("t".repeat(160))).toHaveClass("truncate")
    expect(screen.getByText("d".repeat(160))).toHaveClass("truncate")
  })
})
