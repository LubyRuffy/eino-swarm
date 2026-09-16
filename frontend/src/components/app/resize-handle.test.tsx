import { useState } from "react"
import { fireEvent, render, screen } from "@testing-library/react"
import { describe, expect, it, vi } from "vitest"

import { ResizeHandle } from "./resize-handle"

function Harness({
  edge,
  start = 256,
}: {
  edge: "left" | "right"
  start?: number
}) {
  const [width, setWidth] = useState(start)
  return (
    <div data-testid="box" style={{ width, position: "relative" }}>
      <ResizeHandle
        width={width}
        onWidthChange={setWidth}
        edge={edge}
        label="Resize"
        min={200}
        max={480}
      />
    </div>
  )
}

describe("ResizeHandle", () => {
  it("moves a right-edge strip with the arrow that points that way", () => {
    render(<Harness edge="right" />)
    const handle = screen.getByRole("separator", { name: "Resize" })
    fireEvent.keyDown(handle, { key: "ArrowRight" })
    expect(screen.getByTestId("box")).toHaveStyle({ width: "280px" })
    fireEvent.keyDown(handle, { key: "ArrowLeft" })
    expect(screen.getByTestId("box")).toHaveStyle({ width: "256px" })
  })

  it("widens a left-edge panel when the separator moves left", () => {
    render(<Harness edge="left" start={352} />)
    const handle = screen.getByRole("separator", { name: "Resize" })
    fireEvent.keyDown(handle, { key: "ArrowLeft" })
    expect(screen.getByTestId("box")).toHaveStyle({ width: "376px" })
    fireEvent.keyDown(handle, { key: "ArrowRight" })
    expect(screen.getByTestId("box")).toHaveStyle({ width: "352px" })
  })

  it("sits on the named edge above neighbouring chrome", () => {
    render(<Harness edge="right" />)
    const handle = screen.getByRole("separator", { name: "Resize" })
    expect(handle.className).toMatch(/\bz-20\b/)
    expect(handle.className).toMatch(/-right-1/)
    expect(handle.className).not.toMatch(/-left-1/)
  })

  it("does not let a drag become a text selection", () => {
    render(<Harness edge="right" />)
    const handle = screen.getByRole("separator", { name: "Resize" })
    fireEvent.pointerDown(handle, { button: 0, clientX: 256, pointerId: 1 })
    expect(document.body.style.userSelect).toBe("none")
    const blocked = new Event("selectstart", { cancelable: true })
    document.dispatchEvent(blocked)
    expect(blocked.defaultPrevented).toBe(true)
    fireEvent.pointerUp(window, { button: 0, pointerId: 1 })
    expect(document.body.style.userSelect).toBe("")
  })

  it("stops at the min and max instead of swallowing the transcript", () => {
    render(<Harness edge="right" start={200} />)
    const handle = screen.getByRole("separator", { name: "Resize" })
    fireEvent.keyDown(handle, { key: "ArrowLeft" })
    expect(screen.getByTestId("box")).toHaveStyle({ width: "200px" })
    fireEvent.keyDown(handle, { key: "ArrowRight" })
    fireEvent.keyDown(handle, { key: "ArrowRight" })
    // 200 + 24 + 24, still well under max; jump by hammering Right
    for (let i = 0; i < 20; i++) fireEvent.keyDown(handle, { key: "ArrowRight" })
    expect(screen.getByTestId("box")).toHaveStyle({ width: "480px" })
  })

  it("commits on the keyboard so a remembered width is not a live preview", () => {
    const onWidthChange = vi.fn()
    const onWidthCommit = vi.fn()
    render(
      <div style={{ position: "relative" }}>
        <ResizeHandle
          width={256}
          onWidthChange={onWidthChange}
          onWidthCommit={onWidthCommit}
          edge="right"
          label="Resize"
          min={200}
          max={480}
        />
      </div>,
    )
    fireEvent.keyDown(screen.getByRole("separator", { name: "Resize" }), {
      key: "ArrowRight",
    })
    expect(onWidthChange).toHaveBeenCalledWith(280)
    expect(onWidthCommit).toHaveBeenCalledWith(280)
  })
})
