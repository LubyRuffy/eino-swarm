import { fireEvent, render, screen } from "@testing-library/react"
import { expect, it, vi } from "vitest"

import { Composer } from "./composer"
import { TooltipProvider } from "@/components/ui/tooltip"

function renderComposer(props: Partial<Parameters<typeof Composer>[0]> = {}) {
  render(<TooltipProvider><Composer running={false} models={[]}
    onModelChange={vi.fn()} reasoningLevels={[]} onReasoningChange={vi.fn()}
    onSend={vi.fn()} onStop={vi.fn()} onUpload={vi.fn(async () => [])}
    focusSignal={0} {...props} /></TooltipProvider>)
  return screen.getByTestId("composer-input")
}

// The engine reads a command token after other text as prose, so the same
// line from the phone is a message. Acting on it here dropped the words
// before it, and /compact cleared the whole draft.
it.each(["hello /goal keep going", "hello /plan keep going", "hello /compact keep going", "hello /compact "])(
  "sends a command token after other text as the message: %s",
  (text) => {
    const onSend = vi.fn()
    const onSetGoal = vi.fn()
    const onSetPlan = vi.fn()
    const onCompact = vi.fn()
    const input = renderComposer({ onSend, onSetGoal, onSetPlan, onCompact })
    fireEvent.change(input, { target: { value: text } })
    fireEvent.keyDown(input, { key: "Enter", keyCode: 13 })
    expect(onSend).toHaveBeenCalledWith(text.trim(), undefined, expect.anything())
    expect(onSetGoal).not.toHaveBeenCalled()
    expect(onSetPlan).not.toHaveBeenCalled()
    expect(onCompact).not.toHaveBeenCalled()
  },
)

it("does not offer plan after existing text", () => {
  const input = renderComposer()
  fireEvent.change(input, { target: { value: "hello /" } })
  expect(screen.getByTestId("slash-command-goal")).toBeTruthy()
  expect(screen.getByTestId("slash-command-compact")).toBeTruthy()
  expect(screen.queryByTestId("slash-command-plan")).toBeNull()
})
