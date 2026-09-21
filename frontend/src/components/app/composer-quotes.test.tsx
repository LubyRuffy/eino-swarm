import { fireEvent, render, screen } from "@testing-library/react"
import { describe, expect, it, vi } from "vitest"

import { ComposerQuotes } from "./composer-quotes"

const quotes = [{ id: "q1", text: "alpha beta" }]

function reveal() {
  fireEvent.mouseEnter(screen.getByTestId("composer-quotes"))
}

describe("ComposerQuotes", () => {
  it("keeps the highlight collapsed to the count chip until hover", () => {
    render(<ComposerQuotes quotes={quotes} onChange={vi.fn()} />)
    expect(screen.getByLabelText("1 annotation")).toBeInTheDocument()
    expect(screen.getByLabelText("1 annotation")).toHaveAttribute("aria-expanded", "false")
    expect(screen.queryByTestId("quote-snippet")).toBeNull()
    expect(screen.queryByTestId("quote-details")).toBeNull()
    expect(screen.queryByText("alpha beta")).toBeNull()
    expect(screen.queryByRole("button", { name: "Edit selected text 1" })).toBeNull()
  })

  it("reveals the snippet to read, edit or drop on hover", () => {
    render(<ComposerQuotes quotes={quotes} onChange={vi.fn()} />)
    reveal()
    expect(screen.getByLabelText("1 annotation")).toHaveAttribute("aria-expanded", "true")
    expect(screen.getByTestId("quote-details")).toBeInTheDocument()
    expect(screen.getByTestId("quote-snippet")).toHaveTextContent("Selected text:")
    expect(screen.getByTestId("quote-snippet")).toHaveTextContent("alpha beta")
    expect(screen.getByRole("button", { name: "Edit selected text 1" })).toBeInTheDocument()
    expect(screen.getByRole("button", { name: "Remove selected text 1" })).toBeInTheDocument()
  })

  it("hides the snippet again when the pointer leaves", () => {
    render(<ComposerQuotes quotes={quotes} onChange={vi.fn()} />)
    const root = screen.getByTestId("composer-quotes")
    fireEvent.mouseEnter(root)
    expect(screen.getByTestId("quote-snippet")).toBeInTheDocument()
    fireEvent.mouseLeave(root)
    expect(screen.queryByTestId("quote-snippet")).toBeNull()
  })

  it("opens the snippet when the chip is focused, for a keyboard pass", () => {
    render(<ComposerQuotes quotes={quotes} onChange={vi.fn()} />)
    fireEvent.focus(screen.getByLabelText("1 annotation"))
    expect(screen.getByTestId("quote-snippet")).toHaveTextContent("alpha beta")
    fireEvent.blur(screen.getByLabelText("1 annotation"))
    expect(screen.queryByTestId("quote-snippet")).toBeNull()
  })

  it("pins the snippet on a chip click so a tap screen can reach edit", () => {
    render(<ComposerQuotes quotes={quotes} onChange={vi.fn()} />)
    fireEvent.click(screen.getByLabelText("1 annotation"))
    expect(screen.getByTestId("quote-snippet")).toBeInTheDocument()
    fireEvent.mouseLeave(screen.getByTestId("composer-quotes"))
    expect(screen.getByTestId("quote-snippet")).toBeInTheDocument()
    fireEvent.click(screen.getByLabelText("1 annotation"))
    expect(screen.queryByTestId("quote-snippet")).toBeNull()
  })

  it("edits the snippet in place from the hover panel", () => {
    const onChange = vi.fn()
    render(<ComposerQuotes quotes={quotes} onChange={onChange} />)
    reveal()
    fireEvent.click(screen.getByRole("button", { name: "Edit selected text 1" }))
    const area = screen.getByLabelText("Edit selected text 1")
    fireEvent.change(area, { target: { value: "gamma" } })
    fireEvent.keyDown(area, { key: "Enter" })
    expect(onChange).toHaveBeenCalledWith([{ id: "q1", text: "gamma" }])
  })

  it("drops the snippet from the next send", () => {
    const onChange = vi.fn()
    render(<ComposerQuotes quotes={quotes} onChange={onChange} />)
    reveal()
    fireEvent.click(screen.getByRole("button", { name: "Remove selected text 1" }))
    expect(onChange).toHaveBeenCalledWith([])
  })

  it("keeps the editor open if the pointer leaves while editing", () => {
    render(<ComposerQuotes quotes={quotes} onChange={vi.fn()} />)
    reveal()
    fireEvent.click(screen.getByRole("button", { name: "Edit selected text 1" }))
    fireEvent.mouseLeave(screen.getByTestId("composer-quotes"))
    expect(screen.getByLabelText("Edit selected text 1")).toBeInTheDocument()
  })
})
