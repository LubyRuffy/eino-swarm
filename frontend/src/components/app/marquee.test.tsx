import { render, screen } from "@testing-library/react"
import { describe, expect, it } from "vitest"

import { MarqueeText } from "./marquee"

describe("MarqueeText", () => {
  it("shows the live line of text", () => {
    render(<MarqueeText text="working on the task" active />)
    expect(screen.getByTestId("marquee")).toHaveTextContent("working on the task")
  })

  it("sweeps the live line while work is in progress", () => {
    render(<MarqueeText text="working on the task" active />)
    expect(screen.getByTestId("marquee")).toHaveAttribute("data-marquee", "shimmer")
  })

  it("does not animate when the turn is idle", () => {
    render(<MarqueeText text="working on the task" />)
    expect(screen.getByTestId("marquee")).toHaveAttribute("data-marquee", "off")
  })
})
