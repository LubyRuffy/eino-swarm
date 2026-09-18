import { act, fireEvent, render, screen } from "@testing-library/react"
import { afterEach, describe, expect, it, vi } from "vitest"

import { TerminalPanel } from "./terminal-panel"
import { resetTerminalStore, useTerminal } from "@/store/terminal"

vi.mock("./terminal-session", () => ({
  TerminalSessionView: ({ session }: { session: { id: string; cwd?: string } }) => (
    <div data-testid={`term-session-${session.id}`}>{session.cwd ?? session.id}</div>
  ),
}))

afterEach(() => {
  act(() => {
    resetTerminalStore()
  })
})

describe("TerminalPanel", () => {
  it("stays unmounted until a session exists", () => {
    render(<TerminalPanel onNew={vi.fn()} />)
    expect(screen.queryByTestId("terminal-panel")).not.toBeInTheDocument()
  })

  it("shows a tab per spawn and keeps the newest selected", () => {
    act(() => {
      useTerminal.getState().spawn({ threadId: "th_1" })
      useTerminal.getState().setCwd("term_1", "/tmp/alpha")
      useTerminal.getState().spawn({ threadId: "th_1" })
      useTerminal.getState().setCwd("term_2", "/tmp/beta")
    })
    render(<TerminalPanel onNew={vi.fn()} />)
    expect(screen.getByTestId("terminal-panel")).toBeVisible()
    expect(screen.getByText("alpha")).toBeInTheDocument()
    expect(screen.getByText("beta")).toBeInTheDocument()
    expect(screen.getByTestId("term-session-term_2")).toBeInTheDocument()
  })

  it("asks for another session on + so a second click gets the current project dir", () => {
    act(() => {
      useTerminal.getState().spawn({ threadId: "th_1" })
    })
    const onNew = vi.fn()
    render(<TerminalPanel onNew={onNew} />)
    fireEvent.click(screen.getByRole("button", { name: "New terminal" }))
    expect(onNew).toHaveBeenCalled()
  })

  it("hides the panel without dropping the session list", () => {
    act(() => {
      useTerminal.getState().spawn({ threadId: "th_1" })
    })
    render(<TerminalPanel onNew={vi.fn()} />)
    fireEvent.click(screen.getByRole("button", { name: "Close terminal" }))
    expect(useTerminal.getState().open).toBe(false)
    expect(useTerminal.getState().sessions).toHaveLength(1)
  })
})
