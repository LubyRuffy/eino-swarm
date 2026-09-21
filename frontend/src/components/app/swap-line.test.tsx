import { render, screen } from "@testing-library/react"
import { describe, expect, it } from "vitest"

import { SwapLine } from "./swap-line"

describe("SwapLine", () => {
  it("shows the current line", () => {
    render(<SwapLine itemKey="a" text="reading notes" active />)
    expect(screen.getByTestId("swap-line")).toHaveAttribute("data-swap-key", "a")
    expect(screen.getByTestId("swap-line")).toHaveTextContent("reading notes")
  })

  it("keeps the same key when only the text changes", () => {
    const { rerender } = render(<SwapLine itemKey="a" text="first" active />)
    rerender(<SwapLine itemKey="a" text="first then more" active />)
    expect(screen.getByTestId("swap-line")).toHaveAttribute("data-swap-key", "a")
    expect(screen.getByTestId("swap-line")).toHaveTextContent("first then more")
  })

  it("slides to a new activity when the key changes", () => {
    const { rerender } = render(<SwapLine itemKey="think" text="looking" active />)
    rerender(<SwapLine itemKey="tool" text="Reading notes.md" active />)
    expect(screen.getByTestId("swap-line")).toHaveAttribute("data-swap-key", "tool")
    const marquees = screen.getAllByTestId("marquee")
    expect(marquees.at(-1)).toHaveTextContent("Reading notes.md")
  })

  it("keeps the live line on MarqueeText so overflow still scrolls left to right", () => {
    render(<SwapLine itemKey="a" text="Editing appearance.ts" active />)
    expect(screen.getByTestId("marquee")).toHaveTextContent("Editing appearance.ts")
    expect(screen.getByTestId("marquee")).toHaveAttribute("data-marquee", "shimmer")
  })
})
