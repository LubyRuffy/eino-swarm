import { fireEvent, render, screen } from "@testing-library/react"
import { useRef } from "react"
import { describe, expect, it } from "vitest"

import { useOutputTail } from "./output-scroll"

function Box({ text, follow }: { text: string; follow: boolean }) {
  const ref = useRef<HTMLPreElement>(null)
  useOutputTail(ref, text, follow)
  return (
    <pre data-testid="tool-output" ref={ref}>
      {text}
    </pre>
  )
}

function overflow(el: HTMLElement, height = 1000, view = 288) {
  Object.defineProperty(el, "scrollHeight", { configurable: true, get: () => height })
  Object.defineProperty(el, "clientHeight", { configurable: true, get: () => view })
}

describe("useOutputTail", () => {
  it("keeps a following box on the live edge as text grows", () => {
    const { rerender } = render(<Box text={"a\n"} follow />)
    const el = screen.getByTestId("tool-output")
    overflow(el)
    rerender(<Box text={"a\n".repeat(40)} follow />)
    expect(el.scrollTop).toBe(1000)
  })

  it("does not yank after the reader wheels up", () => {
    const { rerender } = render(<Box text={"a\n"} follow />)
    const el = screen.getByTestId("tool-output")
    overflow(el)
    rerender(<Box text={"a\n".repeat(20)} follow />)
    expect(el.scrollTop).toBe(1000)
    fireEvent.wheel(el, { deltaY: -40 })
    el.scrollTop = 0
    rerender(<Box text={"a\n".repeat(40)} follow />)
    expect(el.scrollTop).toBe(0)
  })

  it("re-pins after the reader wheels back to the live edge", () => {
    const { rerender } = render(<Box text={"a\n"} follow />)
    const el = screen.getByTestId("tool-output")
    overflow(el)
    rerender(<Box text={"a\n".repeat(20)} follow />)
    fireEvent.wheel(el, { deltaY: -40 })
    el.scrollTop = 0
    rerender(<Box text={"a\n".repeat(30)} follow />)
    expect(el.scrollTop).toBe(0)
    el.scrollTop = 700
    fireEvent.wheel(el, { deltaY: 40 })
    fireEvent.scroll(el)
    rerender(<Box text={"a\n".repeat(40)} follow />)
    expect(el.scrollTop).toBe(1000)
  })

  it("leaves a finished box where it is", () => {
    const { rerender } = render(<Box text={"a\n"} follow />)
    const el = screen.getByTestId("tool-output")
    overflow(el)
    rerender(<Box text={"a\n".repeat(40)} follow />)
    expect(el.scrollTop).toBe(1000)
    el.scrollTop = 0
    rerender(<Box text={"a\n".repeat(40)} follow={false} />)
    expect(el.scrollTop).toBe(0)
  })
})
