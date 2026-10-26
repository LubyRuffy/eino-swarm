import { act, cleanup, fireEvent, render, screen } from "@testing-library/react"
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest"

import { SelectionMenu } from "./selection-menu"
import { TerminalSessionView } from "./terminal-session"
import { TERMINAL_SELECTION_EVENT, type TerminalSelection } from "@/lib/terminal"

const mocks = vi.hoisted(() => {
  type Range = { start: { x: number; y: number }; end: { x: number; y: number } }

  class MockTerminal {
    static instances: MockTerminal[] = []
    cols = 80
    rows = 10
    buffer = { active: { viewportY: 20 } }
    selected = ""
    range: Range | undefined
    selectionListeners = new Set<() => void>()
    dataListeners = new Set<(data: string) => void>()
    write = vi.fn()
    dispose = vi.fn()
    loadAddon = vi.fn()
    clearSelection = vi.fn(() => {
      const hadSelection = Boolean(this.selected)
      this.selected = ""
      this.range = undefined
      if (hadSelection) this.emitSelection()
    })
    constructor() { MockTerminal.instances.push(this) }
    open(host: HTMLElement) {
      const terminalScreen = document.createElement("div")
      terminalScreen.className = "xterm-screen"
      terminalScreen.getBoundingClientRect = () => new DOMRect(10, 100, 800, 200)
      host.append(terminalScreen)
    }
    getSelection() { return this.selected }
    getSelectionPosition() { return this.range }
    onSelectionChange(listener: () => void) {
      this.selectionListeners.add(listener)
      return { dispose: vi.fn(() => this.selectionListeners.delete(listener)) }
    }
    onData(listener: (data: string) => void) {
      this.dataListeners.add(listener)
      return { dispose: vi.fn(() => this.dataListeners.delete(listener)) }
    }
    emitSelection() { this.selectionListeners.forEach((listener) => listener()) }
    select(text: string, notify = true) {
      this.selected = text
      this.range = { start: { x: 2, y: 22 }, end: { x: 8, y: 22 } }
      if (notify) this.emitSelection()
    }
  }

  class MockSocket {
    static OPEN = 1
    static instances: MockSocket[] = []
    readyState = MockSocket.OPEN
    binaryType = ""
    onmessage?: (event: { data: string | ArrayBuffer }) => void
    onclose?: () => void
    send = vi.fn()
    close = vi.fn()
    constructor(readonly url: string) { MockSocket.instances.push(this) }
    receive(data: string | ArrayBuffer) { this.onmessage?.({ data }) }
  }

  return { MockTerminal, MockSocket, fit: vi.fn() }
})

vi.mock("@xterm/xterm", () => ({ Terminal: mocks.MockTerminal }))
vi.mock("@xterm/addon-fit", () => ({
  FitAddon: class { fit = mocks.fit },
}))

const source = { id: "term_source", threadId: "th_source", cwd: "/stale-client-path" }
const ready = { type: "ready", cwd: "/initial folder ", shell: "/bin/test-shell" }
const expected = (text: string) => [
  "Source: integrated terminal",
  "",
  text,
  "",
  'Session: "term_source"',
  'Conversation: "th_source"',
  'Initial directory: "/initial folder "',
  'Shell: "/bin/test-shell"',
].join("\n")

const removeListeners: Array<() => void> = []
function selections() {
  const events: TerminalSelection[] = []
  const listener = (event: Event) => events.push((event as CustomEvent<TerminalSelection>).detail)
  document.addEventListener(TERMINAL_SELECTION_EVENT, listener)
  removeListeners.push(() => document.removeEventListener(TERMINAL_SELECTION_EVENT, listener))
  return events
}

beforeEach(() => {
  mocks.MockTerminal.instances.length = 0
  mocks.MockSocket.instances.length = 0
  vi.clearAllMocks()
  vi.stubGlobal("WebSocket", mocks.MockSocket)
  window.getSelection()?.removeAllRanges()
})

afterEach(() => {
  cleanup()
  removeListeners.splice(0).forEach((remove) => remove())
  vi.unstubAllGlobals()
})

describe("TerminalSessionView quoting", () => {
  it("publishes actual ready metadata and xterm cell bounds with the selected output", () => {
    const events = selections()
    const onReady = vi.fn()
    render(<TerminalSessionView session={source} visible onReady={onReady} />)
    const terminal = mocks.MockTerminal.instances[0]
    const socket = mocks.MockSocket.instances[0]
    act(() => socket.receive(JSON.stringify(ready)))
    expect(onReady).toHaveBeenCalledWith(ready.cwd)

    act(() => terminal.select("selected output"))
    expect(events).toHaveLength(1)
    expect(events[0]).toMatchObject({
      source: screen.getByTestId("terminal-session-term_source"),
      selection: {
        text: expected("selected output"),
        rect: { top: 140, left: 30, width: 60, height: 20, bottom: 160 },
      },
    })
    expect(events[0].selection?.text).not.toContain(source.cwd)
    expect(socket.url).toContain("/api/threads/th_source/terminal?")
  })

  it("publishes the settled host mouse selection without inventing missing ready fields", () => {
    const events = selections()
    render(<TerminalSessionView session={source} visible onReady={vi.fn()} />)
    const terminal = mocks.MockTerminal.instances[0]
    terminal.select("result without command", false)
    fireEvent.mouseUp(screen.getByTestId("terminal-session-term_source"))
    expect(events.at(-1)?.selection?.text).toBe([
      "Source: integrated terminal",
      "",
      "result without command",
      "",
      'Session: "term_source"',
      'Conversation: "th_source"',
    ].join("\n"))
    expect(events.at(-1)?.selection?.text).not.toMatch(/Initial directory:|Shell:|Command:|Exit/)
  })

  it("adds the frozen terminal reference once through the shared menu while output continues", () => {
    const onAdd = vi.fn()
    render(<>
      <TerminalSessionView session={source} visible onReady={vi.fn()} />
      <SelectionMenu onAdd={onAdd} />
    </>)
    const terminal = mocks.MockTerminal.instances[0]
    const socket = mocks.MockSocket.instances[0]
    act(() => socket.receive(JSON.stringify(ready)))
    const host = screen.getByTestId("terminal-session-term_source")
    fireEvent.mouseDown(host)
    act(() => terminal.select("first selected output"))
    expect(screen.queryByRole("menuitem", { name: "Add to chat" })).not.toBeInTheDocument()
    fireEvent.mouseUp(host)
    const add = screen.getByRole("menuitem", { name: "Add to chat" })

    // Output and DOM selection changes do not replace the terminal snapshot.
    terminal.select("later buffer contents", false)
    act(() => socket.receive("ongoing shell output"))
    fireEvent(document, new Event("selectionchange"))
    fireEvent.mouseDown(add)
    fireEvent.mouseUp(add)
    fireEvent.click(add)
    expect(onAdd).toHaveBeenCalledTimes(1)
    expect(onAdd).toHaveBeenCalledWith(expected("first selected output"))
    expect(terminal.write).toHaveBeenCalledWith("ongoing shell output")
    expect(terminal.clearSelection).toHaveBeenCalledTimes(1)
    expect(screen.queryByRole("menuitem", { name: "Add to chat" })).not.toBeInTheDocument()
    fireEvent(document, new Event("selectionchange"))
    expect(screen.queryByRole("menuitem", { name: "Add to chat" })).not.toBeInTheDocument()
  })

  it("does not offer an empty quote for whitespace-only terminal cells", () => {
    const events = selections()
    render(<>
      <TerminalSessionView session={source} visible onReady={vi.fn()} />
      <SelectionMenu onAdd={vi.fn()} />
    </>)
    act(() => mocks.MockTerminal.instances[0].select(" \t\r\n "))
    expect(events.at(-1)?.selection).toBeNull()
    expect(screen.queryByRole("menuitem", { name: "Add to chat" })).not.toBeInTheDocument()
  })

  it("keeps the selected snapshot while live output boxes scroll without user input", () => {
    const onAdd = vi.fn()
    render(<>
      <div data-testid="other-live-output" />
      <TerminalSessionView session={source} visible onReady={vi.fn()} />
      <SelectionMenu onAdd={onAdd} />
    </>)
    const terminal = mocks.MockTerminal.instances[0]
    const socket = mocks.MockSocket.instances[0]
    act(() => {
      socket.receive(JSON.stringify(ready))
      terminal.select("original selected output")
    })
    // Streaming thought/exec boxes follow their tail independently of the
    // transcript. Their DOM scroll events must not revoke a terminal quote.
    const otherOutput = screen.getByTestId("other-live-output")
    otherOutput.scrollTop = 100
    fireEvent.scroll(otherOutput)
    expect(screen.getByRole("menuitem", { name: "Add to chat" })).toBeVisible()

    terminal.select("later buffer contents", false)
    act(() => socket.receive("more shell output"))
    const terminalScreen = screen.getByTestId("terminal-session-term_source").querySelector(".xterm-screen")!
    terminalScreen.scrollTop = 200
    fireEvent.scroll(terminalScreen)
    fireEvent(document, new Event("selectionchange"))
    fireEvent.click(screen.getByRole("menuitem", { name: "Add to chat" }))
    expect(onAdd).toHaveBeenCalledTimes(1)
    expect(onAdd).toHaveBeenCalledWith(expected("original selected output"))
  })

  it("drops a hidden terminal selection and dismisses its menu", () => {
    const events = selections()
    const onReady = vi.fn()
    const view = (visible: boolean) => <>
      <TerminalSessionView session={source} visible={visible} onReady={onReady} />
      <SelectionMenu onAdd={vi.fn()} />
    </>
    const { rerender } = render(view(true))
    const terminal = mocks.MockTerminal.instances[0]
    act(() => terminal.select("output"))
    expect(screen.getByRole("menuitem", { name: "Add to chat" })).toBeVisible()
    rerender(view(false))
    expect(terminal.clearSelection).toHaveBeenCalledTimes(1)
    expect(events.at(-1)?.selection).toBeNull()
    expect(screen.queryByRole("menuitem", { name: "Add to chat" })).not.toBeInTheDocument()
  })

  it("keeps the current source when another terminal is closed", () => {
    const onAdd = vi.fn()
    const second = { id: "term_second", projectId: "pj_second" }
    const view = (first: boolean) => <>
      {first ? <TerminalSessionView key="first" session={source} visible onReady={vi.fn()} /> : null}
      <TerminalSessionView key="second" session={second} visible onReady={vi.fn()} />
      <SelectionMenu onAdd={onAdd} />
    </>
    const { rerender } = render(view(true))
    const [first, current] = mocks.MockTerminal.instances
    act(() => {
      first.select("old selection")
      current.select("current selection")
    })
    rerender(view(false))
    expect(first.dispose).toHaveBeenCalledTimes(1)
    fireEvent.click(screen.getByRole("menuitem", { name: "Add to chat" }))
    expect(onAdd).toHaveBeenCalledTimes(1)
    expect(onAdd).toHaveBeenCalledWith([
      "Source: integrated terminal",
      "",
      "current selection",
      "",
      'Session: "term_second"',
      'Project: "pj_second"',
    ].join("\n"))
    expect(current.clearSelection).toHaveBeenCalledTimes(1)
  })

  it("withdraws its source and disposes both subscriptions on unmount", () => {
    const events = selections()
    const { unmount } = render(<TerminalSessionView session={source} visible onReady={vi.fn()} />)
    const terminal = mocks.MockTerminal.instances[0]
    const socket = mocks.MockSocket.instances[0]
    const host = screen.getByTestId("terminal-session-term_source")
    act(() => terminal.select("output"))
    unmount()
    expect(events.at(-1)).toMatchObject({ source: host, selection: null })
    expect(terminal.selectionListeners.size).toBe(0)
    expect(terminal.dataListeners.size).toBe(0)
    expect(socket.close).toHaveBeenCalledTimes(1)
    expect(terminal.dispose).toHaveBeenCalledTimes(1)
    const count = events.length
    fireEvent.mouseUp(host)
    terminal.emitSelection()
    expect(events).toHaveLength(count)
  })
})
