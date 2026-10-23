import { fireEvent, render, screen } from "@testing-library/react"
import { describe, expect, it, vi } from "vitest"

import { Composer } from "./composer"
import { TooltipProvider } from "@/components/ui/tooltip"

function renderComposer(props: Partial<Parameters<typeof Composer>[0]> = {}) {
  return render(<TooltipProvider><Composer running={false} models={[]} reasoningLevels={[]}
    onModelChange={vi.fn()} onReasoningChange={vi.fn()} onSend={vi.fn()}
    onStop={vi.fn()} onUpload={vi.fn(async () => [])} focusSignal={0} {...props}
  /></TooltipProvider>)
}

describe("trailing goal submission", () => {
  it.each([false, true])("replaces a completed objective with the full preceding task, running=%s", (running) => {
    const goal = vi.fn()
    const send = vi.fn()
    renderComposer({ running, goal: "The previous objective", goalComplete: true, onSetGoal: goal, onSend: send })
    const input = screen.getByTestId("composer-input")
    fireEvent.change(input, { target: { value: "检查新的 operation /goal" } })
    expect(screen.getByRole("button", { name: "Send" })).toBeEnabled()
    fireEvent.keyDown(input, { key: "Enter", keyCode: 13 })
    expect(goal).toHaveBeenCalledTimes(1)
    expect(goal).toHaveBeenCalledWith("检查新的 operation")
    expect(send).not.toHaveBeenCalled()
  })

  it("submits a task after picking its trailing goal marker from the menu", () => {
    const goal = vi.fn()
    renderComposer({ onSetGoal: goal })
    const input = screen.getByTestId("composer-input")
    fireEvent.change(input, { target: { value: "Inspect the operation /" } })
    fireEvent.click(screen.getByTestId("slash-command-goal"))
    expect(input).toHaveValue("Inspect the operation /goal ")
    fireEvent.keyDown(input, { key: "Enter", keyCode: 13 })
    expect(goal).toHaveBeenCalledTimes(1)
    expect(goal).toHaveBeenCalledWith("Inspect the operation")
  })

  it("leaves only /goal waiting for the missing objective", () => {
    const goal = vi.fn()
    renderComposer({ onSetGoal: goal })
    const input = screen.getByTestId("composer-input")
    fireEvent.change(input, { target: { value: " /goal " } })
    expect(screen.getByRole("button", { name: "Send" })).toBeDisabled()
    fireEvent.keyDown(input, { key: "Enter", keyCode: 13 })
    expect(goal).not.toHaveBeenCalled()
    expect(input).toHaveValue(" /goal ")
  })
})
