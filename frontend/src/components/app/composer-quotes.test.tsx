import { fireEvent, render, screen } from "@testing-library/react"
import { describe, expect, it, vi } from "vitest"

import { ComposerQuotes } from "./composer-quotes"

const quotes = [{ id: "q1", text: "alpha beta" }]

describe("ComposerQuotes", () => {
  it("shows a truncated chip and the annotation count without a hover dump", () => {
    render(<ComposerQuotes quotes={quotes} onChange={vi.fn()} />)
    expect(screen.getByLabelText("1 annotation")).toBeInTheDocument()
    expect(screen.getByTestId("quote-snippet")).toHaveTextContent("Selected text:")
    expect(screen.getByTestId("quote-snippet")).toHaveTextContent("alpha beta")
    expect(screen.queryByTestId("quote-card")).toBeNull()
  })

  it("lets a click edit or drop the snippet", () => {
    render(<ComposerQuotes quotes={quotes} onChange={vi.fn()} />)
    expect(screen.getByRole("button", { name: "Edit selected text 1" })).toBeInTheDocument()
    expect(screen.getByRole("button", { name: "Remove selected text 1" })).toBeInTheDocument()
  })

  it("edits the snippet in place", () => {
    const onChange = vi.fn()
    render(<ComposerQuotes quotes={quotes} onChange={onChange} />)
    fireEvent.click(screen.getByRole("button", { name: "Edit selected text 1" }))
    const area = screen.getByLabelText("Edit selected text 1")
    fireEvent.change(area, { target: { value: "gamma" } })
    fireEvent.keyDown(area, { key: "Enter" })
    expect(onChange).toHaveBeenCalledWith([{ id: "q1", text: "gamma" }])
  })

  it("drops the snippet from the next send", () => {
    const onChange = vi.fn()
    render(<ComposerQuotes quotes={quotes} onChange={onChange} />)
    fireEvent.click(screen.getByRole("button", { name: "Remove selected text 1" }))
    expect(onChange).toHaveBeenCalledWith([])
  })
})
