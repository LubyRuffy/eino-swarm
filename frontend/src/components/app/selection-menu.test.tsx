import { fireEvent, render, screen } from "@testing-library/react"
import { describe, expect, it, vi } from "vitest"

import { SelectionMenu } from "./selection-menu"

function selectAll(el: HTMLElement) {
  const range = document.createRange()
  range.selectNodeContents(el)
  const sel = window.getSelection()
  sel?.removeAllRanges()
  sel?.addRange(range)
}

describe("SelectionMenu", () => {
  it("offers Add to chat after a selection inside a quote source", () => {
    const onAdd = vi.fn()
    render(
      <>
        <div data-quote-source="">alpha beta</div>
        <SelectionMenu onAdd={onAdd} />
      </>,
    )
    selectAll(screen.getByText("alpha beta"))
    fireEvent.mouseUp(document)
    expect(screen.getByRole("menuitem", { name: "Add to chat" })).toBeInTheDocument()

    fireEvent.click(screen.getByRole("menuitem", { name: "Add to chat" }))
    expect(onAdd).toHaveBeenCalledWith("alpha beta")
    expect(screen.queryByRole("menuitem", { name: "Add to chat" })).toBeNull()
  })

  it("does not appear for a selection outside a quote source", () => {
    render(
      <>
        <p>composer draft</p>
        <SelectionMenu onAdd={vi.fn()} />
      </>,
    )
    selectAll(screen.getByText("composer draft"))
    fireEvent.mouseUp(document)
    expect(screen.queryByRole("menuitem", { name: "Add to chat" })).toBeNull()
  })
})
