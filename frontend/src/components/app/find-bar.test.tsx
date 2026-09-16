import { fireEvent, render, screen } from "@testing-library/react"
import { createRef } from "react"
import { describe, expect, it, vi } from "vitest"

import { FindBar } from "./find-bar"

function renderBar(
  props: Partial<Parameters<typeof FindBar>[0]> = {},
) {
  const inputRef = createRef<HTMLInputElement>()
  const onQuery = vi.fn()
  const onNext = vi.fn()
  const onClose = vi.fn()
  render(
    <FindBar
      query=""
      index={0}
      total={0}
      inputRef={inputRef}
      onQuery={onQuery}
      onNext={onNext}
      onClose={onClose}
      {...props}
    />,
  )
  return { inputRef, onQuery, onNext, onClose }
}

describe("FindBar", () => {
  it("names the field so a screen reader and a test can find it", () => {
    renderBar()
    expect(screen.getByRole("search")).toBeInTheDocument()
    expect(screen.getByLabelText("Find in conversation")).toBeInTheDocument()
  })

  it("hides the count until there is a query", () => {
    renderBar({ query: "" })
    expect(screen.queryByTestId("conversation-find-count")).toBeNull()
  })

  it("numbers the current hit the way the bar in Codex does", () => {
    renderBar({ query: "needle", index: 0, total: 8 })
    expect(screen.getByTestId("conversation-find-count")).toHaveTextContent("1 / 8 results")
  })

  it("says when nothing matches", () => {
    renderBar({ query: "zzz", index: 0, total: 0 })
    expect(screen.getByTestId("conversation-find-count")).toHaveTextContent("No results")
    expect(screen.getByRole("button", { name: "Next match" })).toBeDisabled()
  })

  it("steps with Enter and Shift+Enter, and closes on Escape", () => {
    const { onNext, onClose } = renderBar({ query: "needle", total: 3 })
    const input = screen.getByLabelText("Find in conversation")
    fireEvent.keyDown(input, { key: "Enter" })
    fireEvent.keyDown(input, { key: "Enter", shiftKey: true })
    fireEvent.keyDown(input, { key: "Escape" })
    expect(onNext).toHaveBeenNthCalledWith(1, 1)
    expect(onNext).toHaveBeenNthCalledWith(2, -1)
    expect(onClose).toHaveBeenCalled()
  })

  it("does not step on an IME confirming Enter", () => {
    const { onNext } = renderBar({ query: "needle", total: 2 })
    fireEvent.keyDown(screen.getByLabelText("Find in conversation"), {
      key: "Enter",
      keyCode: 229,
    })
    expect(onNext).not.toHaveBeenCalled()
  })

  it("steps from the chevrons and closes from the button", () => {
    const { onNext, onClose } = renderBar({ query: "needle", total: 4 })
    fireEvent.click(screen.getByRole("button", { name: "Next match" }))
    fireEvent.click(screen.getByRole("button", { name: "Previous match" }))
    fireEvent.click(screen.getByRole("button", { name: "Close find" }))
    expect(onNext).toHaveBeenCalledWith(1)
    expect(onNext).toHaveBeenCalledWith(-1)
    expect(onClose).toHaveBeenCalled()
  })
})
