import { fireEvent, render, screen } from "@testing-library/react"
import { describe, expect, it, vi } from "vitest"

import { SlashMenu } from "./slash-menu"
import type { SlashCommand } from "@/lib/slash"

const items: SlashCommand[] = [
  { id: "goal", name: "goal", description: "Set a goal to keep pursuing" },
  {
    id: "compact",
    name: "compact",
    description: "Compact this chat's context",
    hint: "12% full",
  },
]

describe("SlashMenu", () => {
  it("lists commands with their descriptions and the highlighted row", () => {
    const onSelect = vi.fn()
    render(
      <div className="relative">
        <SlashMenu
          items={items}
          activeIndex={1}
          onHover={vi.fn()}
          onSelect={onSelect}
        />
      </div>,
    )
    expect(screen.getByTestId("slash-menu")).toBeTruthy()
    expect(screen.getByTestId("slash-command-goal").getAttribute("aria-selected")).toBe(
      "false",
    )
    expect(screen.getByTestId("slash-command-compact").getAttribute("aria-selected")).toBe(
      "true",
    )
    expect(screen.getByText("12% full")).toBeTruthy()
    fireEvent.click(screen.getByTestId("slash-command-goal"))
    expect(onSelect).toHaveBeenCalledWith(items[0])
  })

  it("keeps the textarea focused by cancelling mousedown on a row", () => {
    render(
      <div className="relative">
        <SlashMenu
          items={items}
          activeIndex={0}
          onHover={vi.fn()}
          onSelect={vi.fn()}
        />
      </div>,
    )
    const row = screen.getByTestId("slash-command-goal")
    const ev = new MouseEvent("mousedown", { bubbles: true, cancelable: true })
    row.dispatchEvent(ev)
    expect(ev.defaultPrevented).toBe(true)
  })
})
