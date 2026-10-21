import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react"
import { useState } from "react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import { App } from "./App"
import { Dialog, DialogContent, DialogTitle } from "@/components/ui/dialog"
import { api } from "@/lib/api"
import { useApp } from "@/store/app"

vi.mock("@/components/app/terminal-panel", () => ({ TerminalPanel: () => null }))

const initialState = useApp.getState()

function Preview() {
  const [open, setOpen] = useState(true)
  return open ? (
    <Dialog open onOpenChange={setOpen}>
      <DialogContent aria-describedby={undefined}>
        <DialogTitle>Image preview</DialogTitle>
      </DialogContent>
    </Dialog>
  ) : null
}

describe("App Escape handling", () => {
  beforeEach(() => {
    vi.spyOn(api, "clients").mockResolvedValue({ enabled: false, tools: [] })
    useApp.setState({
      boot: vi.fn(async () => {}),
      interrupt: vi.fn(async () => {}),
      newThread: vi.fn(async () => undefined),
      status: { running: true },
    })
  })

  afterEach(() => {
    cleanup()
    useApp.setState(initialState)
    vi.restoreAllMocks()
  })

  it("closes a mounted image dialog before Escape can stop the running turn", async () => {
    const interrupt = useApp.getState().interrupt
    render(<><App /><Preview /></>)

    fireEvent.keyDown(screen.getByRole("dialog"), { key: "Escape" })
    expect(interrupt).not.toHaveBeenCalled()
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull())

    fireEvent.keyDown(document.body, { key: "Escape" })
    expect(interrupt).toHaveBeenCalledOnce()
  })

  it("closes the image dialog before a find bar underneath it", async () => {
    const interrupt = useApp.getState().interrupt
    const { rerender } = render(<><App /></>)
    fireEvent.keyDown(document.body, { key: "f", ctrlKey: true })
    expect(screen.getByTestId("conversation-find")).toBeInTheDocument()

    rerender(<><App /><Preview /></>)
    fireEvent.keyDown(screen.getByRole("dialog"), { key: "Escape" })
    expect(screen.getByTestId("conversation-find")).toBeInTheDocument()
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull())
    expect(interrupt).not.toHaveBeenCalled()

    fireEvent.keyDown(screen.getByLabelText("Find in conversation"), { key: "Escape" })
    expect(screen.queryByTestId("conversation-find")).toBeNull()
    expect(interrupt).not.toHaveBeenCalled()
    fireEvent.keyDown(document.body, { key: "Escape" })
    expect(interrupt).toHaveBeenCalledOnce()
  })

  it("ignores closed dialog remnants when stopping the running turn", () => {
    const interrupt = useApp.getState().interrupt
    render(<><App /><div role="dialog" data-state="closed" /></>)

    fireEvent.keyDown(document.body, { key: "Escape" })
    expect(interrupt).toHaveBeenCalledOnce()
  })

  it("keeps the new-conversation shortcut unchanged while a dialog is open", async () => {
    const newThread = useApp.getState().newThread
    render(<><App /><Preview /></>)

    await act(async () => {
      fireEvent.keyDown(screen.getByRole("dialog"), { key: "n", ctrlKey: true })
    })
    expect(newThread).toHaveBeenCalledOnce()
  })
})
