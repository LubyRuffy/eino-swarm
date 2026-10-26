import { act, fireEvent, render, screen } from "@testing-library/react"
import { describe, expect, it, vi } from "vitest"

import { SelectionMenu } from "./selection-menu"
import { TERMINAL_SELECTION_EVENT, type TerminalSelection } from "@/lib/terminal"

function selectAll(el: HTMLElement) {
  const range = document.createRange()
  range.selectNodeContents(el)
  const sel = window.getSelection()
  sel?.removeAllRanges()
  sel?.addRange(range)
}

describe("SelectionMenu", () => {
  function terminalSelection(source: HTMLElement, text: string | null, clear = vi.fn()) {
    act(() => document.dispatchEvent(new CustomEvent<TerminalSelection>(TERMINAL_SELECTION_EVENT, {
      detail: {
        source, clear,
        selection: text === null ? null : {
          text, rect: { top: 200, bottom: 220, left: 100, width: 40, height: 20 },
        },
      },
    })))
    return clear
  }

  it("shares one menu between terminal and transcript selections and clears the correct source", () => {
    const onAdd = vi.fn()
    render(<><div data-quote-source="">transcript</div><div data-testid="terminal" /><SelectionMenu onAdd={onAdd} /></>)
    selectAll(screen.getByText("transcript"))
    fireEvent.mouseUp(document)
    const terminal = screen.getByTestId("terminal")
    const clear = terminalSelection(terminal, "terminal provenance\noutput")
    expect(screen.getAllByRole("menuitem", { name: "Add to chat" })).toHaveLength(1)
    // Another hidden terminal's cleanup cannot dismiss this terminal's menu.
    terminalSelection(document.createElement("div"), null)
    fireEvent.click(screen.getByRole("menuitem", { name: "Add to chat" }))
    expect(onAdd).toHaveBeenCalledTimes(1)
    expect(onAdd).toHaveBeenCalledWith("terminal provenance\noutput")
    expect(clear).toHaveBeenCalledOnce()
    expect(screen.queryByRole("menuitem")).toBeNull()

    terminalSelection(terminal, "old terminal selection")
    selectAll(screen.getByText("transcript"))
    fireEvent.mouseDown(screen.getByText("transcript"))
    fireEvent.mouseUp(screen.getByText("transcript"))
    fireEvent.click(screen.getByRole("menuitem", { name: "Add to chat" }))
    expect(onAdd).toHaveBeenLastCalledWith("transcript")
  })

  it("keeps terminal snapshots on programmatic scroll but dismisses on user movement or escape", () => {
    render(<><div data-testid="terminal" /><SelectionMenu onAdd={vi.fn()} /></>)
    const terminal = screen.getByTestId("terminal")
    fireEvent.mouseDown(terminal)
    terminalSelection(terminal, "output")
    expect(screen.queryByRole("menuitem")).toBeNull()
    fireEvent.mouseUp(terminal)
    expect(screen.getByRole("menuitem")).toBeVisible()
    fireEvent.scroll(terminal)
    expect(screen.getByRole("menuitem")).toBeVisible()
    fireEvent.scroll(document)
    expect(screen.getByRole("menuitem")).toBeVisible()
    fireEvent.keyUp(terminal, { key: "Escape" })
    expect(screen.queryByRole("menuitem")).toBeNull()
    for (const dismiss of [
      () => fireEvent.wheel(document),
      () => fireEvent.touchMove(document),
      () => fireEvent.resize(window),
      () => fireEvent.mouseUp(document),
      () => terminalSelection(terminal, null),
    ]) {
      terminalSelection(terminal, "output")
      expect(screen.getByRole("menuitem")).toBeVisible()
      dismiss()
      expect(screen.queryByRole("menuitem")).toBeNull()
    }
  })
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

  // Streaming tokens replace text nodes and auto-follow scrolls. Both used
  // to look like "the selection is gone" and flash Add to chat then hide it.
  it("keeps Add to chat when a stream collapses the native selection", () => {
    render(
      <>
        <div data-quote-source="">alpha beta</div>
        <SelectionMenu onAdd={vi.fn()} />
      </>,
    )
    selectAll(screen.getByText("alpha beta"))
    fireEvent.mouseUp(document)
    expect(screen.getByRole("menuitem", { name: "Add to chat" })).toBeInTheDocument()

    window.getSelection()?.removeAllRanges()
    fireEvent(document, new Event("selectionchange"))
    fireEvent.scroll(document)
    expect(screen.getByRole("menuitem", { name: "Add to chat" })).toBeInTheDocument()
  })

  it("dismisses Add to chat on a wheel or a click away", () => {
    render(
      <>
        <div data-quote-source="">alpha beta</div>
        <SelectionMenu onAdd={vi.fn()} />
      </>,
    )
    selectAll(screen.getByText("alpha beta"))
    fireEvent.mouseUp(document)
    fireEvent.wheel(document, { deltaY: 40 })
    expect(screen.queryByRole("menuitem", { name: "Add to chat" })).toBeNull()

    selectAll(screen.getByText("alpha beta"))
    fireEvent.mouseUp(document)
    window.getSelection()?.removeAllRanges()
    fireEvent.mouseUp(document)
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
