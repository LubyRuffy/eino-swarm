import { act, createEvent, fireEvent, render, screen } from "@testing-library/react"
import { afterEach, beforeAll, describe, expect, it, vi } from "vitest"

import { SORT_ACTIVATE_PX, useSortableList } from "./sortable"

beforeAll(() => {
  if (typeof PointerEvent === "undefined") {
    // jsdom still has no PointerEvent. MouseEvent is enough: we only read
    // clientX/Y, and the listener is on pointermove/up by name.
    globalThis.PointerEvent = class extends MouseEvent {
      pointerId: number
      constructor(type: string, init: MouseEventInit & { pointerId?: number } = {}) {
        super(type, init)
        this.pointerId = init.pointerId ?? 0
      }
    } as typeof PointerEvent
  }
  if (typeof document.elementFromPoint !== "function") {
    document.elementFromPoint = () => null
  }
})

function transfer() {
  const data: Record<string, string> = {}
  return {
    setData: (type: string, value: string) => {
      data[type] = value
    },
    getData: (type: string) => data[type] ?? "",
    effectAllowed: "move",
    dropEffect: "move",
  }
}

function Harness({
  onMove,
  onOpen,
  disabledId,
}: {
  onMove: (from: string, to: string) => void
  onOpen?: (id: string) => void
  disabledId?: string
}) {
  const sortable = useSortableList(onMove)
  return (
    <div>
      {["a", "b"].map((id) => (
        <div
          key={id}
          data-testid={`row-${id}`}
          data-id={id}
          {...sortable.bind(id, id === disabledId)}
        >
          <span data-drag-handle data-testid={`handle-${id}`}>
            grip
          </span>
          <button type="button" onClick={() => onOpen?.(id)}>
            open {id}
          </button>
          <button type="button" data-no-drag data-testid={`menu-${id}`}>
            menu {id}
          </button>
        </div>
      ))}
    </div>
  )
}

function reorder(from: HTMLElement, to: HTMLElement) {
  const dt = transfer()
  const handle = from.querySelector("[data-drag-handle]")
  if (handle) fireEvent.mouseDown(handle)
  fireEvent.dragStart(from, { dataTransfer: dt })
  fireEvent.dragOver(to, { dataTransfer: dt })
  fireEvent.drop(to, { dataTransfer: dt })
  fireEvent.dragEnd(from, { dataTransfer: dt })
}

function dispatch(type: "pointermove" | "pointerup", x: number, y: number) {
  act(() => {
    window.dispatchEvent(
      new PointerEvent(type, { clientX: x, clientY: y, bubbles: true, pointerId: 1 }),
    )
  })
}

describe("useSortableList", () => {
  afterEach(() => {
    vi.restoreAllMocks()
  })

  it("moves a row after a drag that starts on the handle", () => {
    const onMove = vi.fn()
    render(<Harness onMove={onMove} />)
    reorder(screen.getByTestId("row-b"), screen.getByTestId("row-a"))
    expect(onMove).toHaveBeenCalledWith("b", "a")
  })

  it("does not arm HTML5 draggable on a title press", () => {
    const onMove = vi.fn()
    render(<Harness onMove={onMove} />)
    const row = screen.getByTestId("row-a")
    fireEvent.mouseDown(screen.getByRole("button", { name: "open a" }))
    expect(row.draggable).toBe(false)
    fireEvent.mouseDown(screen.getByTestId("handle-a"))
    expect(row.draggable).toBe(false)
    expect(onMove).not.toHaveBeenCalled()
  })

  it("lets a title click through even if a dragstart races it", () => {
    const onMove = vi.fn()
    const onOpen = vi.fn()
    render(<Harness onMove={onMove} onOpen={onOpen} />)
    const row = screen.getByTestId("row-a")
    const title = screen.getByRole("button", { name: "open a" })
    fireEvent.mouseDown(title)
    expect(row.draggable).toBe(false)
    const start = createEvent.dragStart(row, { dataTransfer: transfer() })
    fireEvent(row, start)
    expect(start.defaultPrevented).toBe(true)
    fireEvent.click(title)
    expect(onOpen).toHaveBeenCalledWith("a")
    expect(onMove).not.toHaveBeenCalled()
  })

  it("reorders from the title after the pointer passes the activation distance", () => {
    const onMove = vi.fn()
    const onOpen = vi.fn()
    render(<Harness onMove={onMove} onOpen={onOpen} />)
    const to = screen.getByTestId("row-a")
    vi.spyOn(document, "elementFromPoint").mockReturnValue(to)
    fireEvent.pointerDown(screen.getByRole("button", { name: "open b" }), {
      button: 0,
      clientX: 10,
      clientY: 10,
      pointerId: 1,
    })
    dispatch("pointermove", 10 + SORT_ACTIVATE_PX + 1, 10)
    dispatch("pointerup", 10 + SORT_ACTIVATE_PX + 1, 10)
    expect(onMove).toHaveBeenCalledWith("b", "a")
    fireEvent.click(screen.getByRole("button", { name: "open b" }))
    expect(onOpen).not.toHaveBeenCalled()
  })

  it("still clicks the title when the pointer never reaches the threshold", () => {
    const onMove = vi.fn()
    const onOpen = vi.fn()
    render(<Harness onMove={onMove} onOpen={onOpen} />)
    const title = screen.getByRole("button", { name: "open b" })
    fireEvent.pointerDown(title, { button: 0, clientX: 10, clientY: 10, pointerId: 1 })
    dispatch("pointermove", 10 + SORT_ACTIVATE_PX - 1, 10)
    dispatch("pointerup", 10 + SORT_ACTIVATE_PX - 1, 10)
    fireEvent.click(title)
    expect(onOpen).toHaveBeenCalledWith("b")
    expect(onMove).not.toHaveBeenCalled()
  })

  it("does not start a pointer drag from the row menu", () => {
    const onMove = vi.fn()
    render(<Harness onMove={onMove} />)
    const to = screen.getByTestId("row-a")
    vi.spyOn(document, "elementFromPoint").mockReturnValue(to)
    fireEvent.pointerDown(screen.getByTestId("menu-b"), {
      button: 0,
      clientX: 10,
      clientY: 10,
      pointerId: 1,
    })
    dispatch("pointermove", 10 + SORT_ACTIVATE_PX + 1, 10)
    dispatch("pointerup", 10 + SORT_ACTIVATE_PX + 1, 10)
    expect(onMove).not.toHaveBeenCalled()
  })

  it("ignores a dragstart on the row that never armed the handle", () => {
    const onMove = vi.fn()
    render(<Harness onMove={onMove} />)
    const from = screen.getByTestId("row-b")
    const to = screen.getByTestId("row-a")
    const dt = transfer()
    const start = createEvent.dragStart(from, { dataTransfer: dt })
    fireEvent(from, start)
    expect(start.defaultPrevented).toBe(true)
    fireEvent.dragOver(to, { dataTransfer: dt })
    fireEvent.drop(to, { dataTransfer: dt })
    expect(onMove).not.toHaveBeenCalled()
  })

  it("still accepts a dragstart whose target is the handle", () => {
    const onMove = vi.fn()
    render(<Harness onMove={onMove} />)
    const from = screen.getByTestId("row-b")
    const to = screen.getByTestId("row-a")
    const dt = transfer()
    fireEvent.dragStart(screen.getByTestId("handle-b"), { dataTransfer: dt })
    fireEvent.dragOver(to, { dataTransfer: dt })
    fireEvent.drop(to, { dataTransfer: dt })
    fireEvent.dragEnd(from, { dataTransfer: dt })
    expect(onMove).toHaveBeenCalledWith("b", "a")
  })

  it("does not move when the bind is disabled", () => {
    const onMove = vi.fn()
    render(<Harness onMove={onMove} disabledId="b" />)
    const from = screen.getByTestId("row-b")
    fireEvent.mouseDown(screen.getByTestId("handle-b"))
    expect(from.draggable).toBe(false)
    reorder(from, screen.getByTestId("row-a"))
    expect(onMove).not.toHaveBeenCalled()
  })

  it("does not report a drop on the row that was picked up", () => {
    const onMove = vi.fn()
    render(<Harness onMove={onMove} />)
    const row = screen.getByTestId("row-a")
    reorder(row, row)
    expect(onMove).not.toHaveBeenCalled()
  })
})
