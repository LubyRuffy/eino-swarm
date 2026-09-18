import { fireEvent, render, screen, within } from "@testing-library/react"
import { describe, expect, it, vi } from "vitest"

import { TurnNav } from "./turn-nav"
import type { TurnNavItem } from "@/lib/turn-nav"

HTMLElement.prototype.scrollIntoView = function () {}

const items: TurnNavItem[] = [
  { id: "tn_a", text: "first request" },
  { id: "tn_b", text: "second request" },
]

function renderNav() {
  const onJump = vi.fn()
  const scroller = { current: document.createElement("div") }
  render(<TurnNav items={items} scrollerRef={scroller} onJump={onJump} />)
  return { onJump }
}

describe("TurnNav", () => {
  it("stays hidden until there are two user turns to jump between", () => {
    const scroller = { current: document.createElement("div") }
    render(
      <TurnNav
        items={[{ id: "tn_a", text: "only" }]}
        scrollerRef={scroller}
        onJump={vi.fn()}
      />,
    )
    expect(screen.queryByTestId("turn-nav")).not.toBeInTheDocument()
  })

  it("packs two ticks into a compact cluster instead of stretching the pane", () => {
    renderNav()
    const nav = screen.getByTestId("turn-nav")
    expect(nav).not.toHaveClass("inset-y-0")
    expect(nav).toHaveClass("top-1/2")
    expect(nav).toHaveClass("z-20")
    const ticks = screen.getByTestId("turn-nav-ticks")
    expect(ticks).toHaveClass("gap-2")
    expect(ticks).not.toHaveClass("h-56")
  })

  it("names itself so a screen reader can find the jumps", () => {
    renderNav()
    expect(screen.getByRole("navigation", { name: "Jump to a message" })).toBeInTheDocument()
  })

  it("jumps from a tick without opening the list first", () => {
    const { onJump } = renderNav()
    fireEvent.click(screen.getByRole("button", { name: "first request" }))
    expect(onJump).toHaveBeenCalledWith("tn_a")
  })

  it("opens the list on hover with each user turn as a row", () => {
    renderNav()
    fireEvent.mouseEnter(screen.getByTestId("turn-nav"))
    const list = screen.getByTestId("turn-nav-list")
    expect(list).toHaveTextContent("first request")
    expect(list).toHaveTextContent("second request")
    expect(list.textContent).not.toMatch(/notes\.md|summary\.md|deadline/i)
  })

  it("jumps from a list row", () => {
    const { onJump } = renderNav()
    fireEvent.mouseEnter(screen.getByTestId("turn-nav"))
    fireEvent.click(within(screen.getByTestId("turn-nav-list")).getByRole("button", { name: "second request" }))
    expect(onJump).toHaveBeenCalledWith("tn_b")
  })

  it("moves between turns with the arrow keys", () => {
    const { onJump } = renderNav()
    fireEvent.keyDown(screen.getByTestId("turn-nav"), { key: "ArrowDown" })
    expect(onJump).toHaveBeenCalledWith("tn_b")
    fireEvent.keyDown(screen.getByTestId("turn-nav"), { key: "Home" })
    expect(onJump).toHaveBeenCalledWith("tn_a")
  })

  it("lights the latest tick while following the live edge", () => {
    const scroller = { current: document.createElement("div") }
    render(
      <TurnNav items={items} scrollerRef={scroller} onJump={vi.fn()} pinned />,
    )
    const ticks = screen.getAllByRole("button")
    expect(ticks[1]).toHaveAttribute("aria-current", "true")
    expect(ticks[0]).not.toHaveAttribute("aria-current")
  })

  it("packs a long rail into a fixed height instead of a second scrollbar", () => {
    const many = Array.from({ length: 16 }, (_, i) => ({
      id: `tn_${i}`,
      text: `turn ${i + 1}`,
    }))
    const scroller = { current: document.createElement("div") }
    render(<TurnNav items={many} scrollerRef={scroller} onJump={vi.fn()} />)
    const ticks = screen.getByTestId("turn-nav-ticks")
    expect(ticks).toHaveClass("h-56")
    expect(ticks.className).not.toMatch(/overflow-y-auto/)
    expect(screen.getAllByRole("button")[0]).toHaveClass("flex-1")
  })

  it("lets a hover row wrap two lines in a wider list", () => {
    renderNav()
    fireEvent.mouseEnter(screen.getByTestId("turn-nav"))
    const list = screen.getByTestId("turn-nav-list")
    expect(list).toHaveClass("w-96")
    expect(list.className).not.toMatch(/\bw-64\b/)
    const label = within(list).getByText("second request")
    expect(label).toHaveClass("line-clamp-2")
    expect(label.className).not.toMatch(/\btruncate\b/)
  })
})
