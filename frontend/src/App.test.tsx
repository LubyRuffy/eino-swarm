import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react"
import { useState } from "react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import { App } from "./App"
import { Dialog, DialogContent, DialogTitle } from "@/components/ui/dialog"
import { api } from "@/lib/api"
import { useApp } from "@/store/app"
import { useTraceFocus } from "@/store/trace-focus"

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

  it("closes an image preview without stopping the turn on another Escape", async () => {
    const interrupt = useApp.getState().interrupt
    render(<><App /><Preview /></>)

    fireEvent.keyDown(screen.getByRole("dialog"), { key: "Escape" })
    expect(interrupt).not.toHaveBeenCalled()
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull())

    fireEvent.keyDown(document.body, { key: "Escape" })
    expect(interrupt).not.toHaveBeenCalled()
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
    expect(interrupt).not.toHaveBeenCalled()
  })

  it("keeps the running turn after Escape with closed dialog remnants", () => {
    const interrupt = useApp.getState().interrupt
    render(<><App /><div role="dialog" data-state="closed" /></>)

    fireEvent.keyDown(document.body, { key: "Escape" })
    expect(interrupt).not.toHaveBeenCalled()
  })

  it("does not turn plain or repeated Escape into a task stop", () => {
    const interrupt = useApp.getState().interrupt
    render(<App />)
    const input = screen.getByTestId("composer-input")
    fireEvent.keyDown(input, { key: "Escape" })
    fireEvent.keyDown(document.body, { key: "Escape" })
    fireEvent.keyDown(document.body, { key: "Escape", repeat: true })
    expect(interrupt).not.toHaveBeenCalled()
  })

  it("still stops a running turn from its explicit Stop button", () => {
    const interrupt = useApp.getState().interrupt
    render(<App />)
    fireEvent.click(screen.getByRole("button", { name: "Stop" }))
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

  it("discards an old side reply on navigation and closes the new panel without stopping the task", async () => {
    useApp.setState({ activeId: "side-a", locale: "en" })
    vi.spyOn(api, "composerDraft").mockImplementation(async (key) => ({
      key, revision: 1, text: "", files: [], quotes: [], state: "editing",
    }))
    vi.spyOn(api, "library").mockResolvedValue({
      dir: "", enabled: false, memory: { text: "", entries: [], chars: 0, limit: 0, rev: "" }, skills: [],
    })
    let complete!: (answer: { answer: string }) => void
    const response = new Promise<{ answer: string }>((resolve) => { complete = resolve })
    const ask = vi.spyOn(api, "askSideChat").mockReturnValue(response)
    await act(async () => { render(<App />) })

    fireEvent.click(screen.getByRole("button", { name: "Open side chat" }))
    const question = screen.getByRole("textbox", { name: "Side chat question" })
    expect(question).toHaveFocus()
    fireEvent.change(question, { target: { value: "Check the current evidence" } })
    fireEvent.keyDown(question, { key: "Enter" })
    const [owner, body, signal] = ask.mock.calls[0]
    expect(owner).toBe("side-a")
    expect(body.question).toBe("Check the current evidence")
    expect(signal.aborted).toBe(false)

    await act(async () => { useApp.setState({ activeId: "side-b" }) })
    expect(signal.aborted).toBe(true)
    expect(screen.queryByRole("complementary", { name: "Side chat" })).toBeNull()
    const trigger = screen.getByRole("button", { name: "Open side chat" })
    fireEvent.click(trigger)
    await act(async () => { complete({ answer: "Old task reply" }); await response })
    expect(screen.queryByText("Old task reply")).toBeNull()
    const newQuestion = screen.getByRole("textbox", { name: "Side chat question" })
    expect(newQuestion).toHaveValue("")

    fireEvent.keyDown(newQuestion, { key: "Escape" })
    expect(screen.queryByRole("complementary", { name: "Side chat" })).toBeNull()
    expect(trigger).toHaveFocus()
    fireEvent.click(trigger)
    await act(async () => { useTraceFocus.getState().open("turn-b") })
    expect(screen.queryByRole("complementary", { name: "Side chat" })).toBeNull()
    expect(screen.getByRole("tab", { name: "Trace" })).toHaveAttribute("aria-selected", "true")
    expect(useApp.getState().interrupt).not.toHaveBeenCalled()
    expect(useApp.getState().status.running).toBe(true)
  })
})
