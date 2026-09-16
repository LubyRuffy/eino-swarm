import { fireEvent, render, screen } from "@testing-library/react"
import { describe, expect, it, vi } from "vitest"

import { ComposerQuotes } from "./composer-quotes"

const quotes = [{ id: "q1", text: "alpha beta" }]

describe("ComposerQuotes", () => {
  it("hides the card until the chip is hovered or opened", () => {
    render(<ComposerQuotes quotes={quotes} onChange={vi.fn()} />)
    expect(screen.getByLabelText("1 annotation")).toBeInTheDocument()
    expect(screen.queryByTestId("quote-card")).toBeNull()
  })

  it("shows the quoted text on hover so it can be edited or dropped", () => {
    render(<ComposerQuotes quotes={quotes} onChange={vi.fn()} />)
    fireEvent.mouseEnter(screen.getByLabelText("1 annotation").parentElement as HTMLElement)
    expect(screen.getByTestId("quote-card")).toHaveTextContent("Selected text:")
    expect(screen.getByTestId("quote-card")).toHaveTextContent("alpha beta")
  })

  it("lets a click on the chip open the same card (keyboard and tests)", () => {
    render(<ComposerQuotes quotes={quotes} onChange={vi.fn()} />)
    fireEvent.click(screen.getByLabelText("1 annotation"))
    expect(screen.getByRole("button", { name: "Edit selected text 1" })).toBeInTheDocument()
    expect(screen.getByRole("button", { name: "Remove selected text 1" })).toBeInTheDocument()
  })

  it("edits the snippet in place", () => {
    const onChange = vi.fn()
    render(<ComposerQuotes quotes={quotes} onChange={onChange} />)
    fireEvent.click(screen.getByLabelText("1 annotation"))
    fireEvent.click(screen.getByRole("button", { name: "Edit selected text 1" }))
    const area = screen.getByLabelText("Edit selected text 1")
    fireEvent.change(area, { target: { value: "gamma" } })
    fireEvent.keyDown(area, { key: "Enter" })
    expect(onChange).toHaveBeenCalledWith([{ id: "q1", text: "gamma" }])
  })

  it("drops the snippet from the next send", () => {
    const onChange = vi.fn()
    render(<ComposerQuotes quotes={quotes} onChange={onChange} />)
    fireEvent.click(screen.getByLabelText("1 annotation"))
    fireEvent.click(screen.getByRole("button", { name: "Remove selected text 1" }))
    expect(onChange).toHaveBeenCalledWith([])
  })
})
