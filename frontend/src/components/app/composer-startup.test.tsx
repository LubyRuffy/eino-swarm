import { fireEvent, render, screen } from "@testing-library/react"
import { describe, expect, it, vi } from "vitest"
import { Composer } from "./composer"
import { TooltipProvider } from "@/components/ui/tooltip"
function composerElement(props: Partial<Parameters<typeof Composer>[0]> = {}) {
  return <TooltipProvider><Composer running={false} models={[]} reasoningLevels={[]} onModelChange={vi.fn()} onReasoningChange={vi.fn()}
    onSend={vi.fn()} onStop={vi.fn()} onUpload={vi.fn(async () => [])} focusSignal={0} {...props} /></TooltipProvider>
}
const renderComposer = (props: Partial<Parameters<typeof Composer>[0]> = {}) => render(composerElement(props))


describe("Composer engine restoration", () => {
  it("retains an existing draft while restoration blocks send and reenables it only after readiness", () => {
    const onSend = vi.fn()
    const view = renderComposer({ onSend })
    const input = screen.getByTestId("composer-input")
    fireEvent.change(input, { target: { value: "preserved draft" } })
    view.rerender(composerElement({ onSend, restoring: true }))
    fireEvent.keyDown(input, { key: "Enter" })
    expect(screen.getByLabelText("Send")).toBeDisabled()
    expect(input).toHaveValue("preserved draft")
    expect(onSend).not.toHaveBeenCalled()
    view.rerender(composerElement({ onSend, restoring: false }))
    expect(input).toHaveValue("preserved draft")
    expect(screen.getByLabelText("Send")).toBeEnabled()
  })
})


it("offers a clickable Stop for the selected restoring conversation even when its live status is idle", () => {
  const onStop = vi.fn()
  const frame = renderComposer({ restoring: true, canStopWhileRestoring: true, onStop })
  const stop = screen.getByRole("button", { name: /^Stop$/ })
  expect(stop).toBeEnabled()
  expect(stop).toHaveClass("pointer-events-auto")
  expect(screen.getByTestId("composer-input")).toBeDisabled()
  fireEvent.click(stop)
  expect(onStop).toHaveBeenCalledOnce()
  frame.rerender(composerElement({ restoring: true, canStopWhileRestoring: false, onStop }))
  expect(screen.queryByRole("button", { name: /^Stop$/ })).toBeNull()
})
