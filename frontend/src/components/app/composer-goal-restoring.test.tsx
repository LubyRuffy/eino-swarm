import { act, fireEvent, render, screen } from "@testing-library/react"
import { expect, it, vi } from "vitest"
import { Composer } from "./composer"
import { TooltipProvider } from "@/components/ui/tooltip"

function element(draftKey: string, onEditGoal: (text: string) => Promise<boolean>, restoring = false) {
  return <TooltipProvider><Composer draftKey={draftKey} goal="Existing objective" restoring={restoring}
    running={false} models={[]} reasoning="" reasoningLevels={[]}
    onModelChange={vi.fn()} onReasoningChange={vi.fn()} onSend={vi.fn()} onStop={vi.fn()} onUpload={vi.fn()}
    onEditGoal={onEditGoal} onClearGoal={vi.fn()} focusSignal={0} /></TooltipProvider>
}

it("passes restoration to the goal controls and keeps an old acknowledgement out of the next conversation", async () => {
  let acknowledge!: (ok: boolean) => void
  const oldEdit = vi.fn(() => new Promise<boolean>((resolve) => { acknowledge = resolve }))
  const nextEdit = vi.fn().mockResolvedValue(true)
  const { rerender } = render(element("thread:old-owner", oldEdit))
  fireEvent.click(screen.getByTestId("goal-text"))
  fireEvent.change(screen.getByTestId("goal-edit"), { target: { value: "First conversation edit" } })
  fireEvent.keyDown(screen.getByTestId("goal-edit"), { key: "Enter" })
  rerender(element("thread:next-owner", nextEdit))
  expect(screen.queryByTestId("goal-edit")).toBeNull()
  fireEvent.click(screen.getByTestId("goal-text"))
  fireEvent.change(screen.getByTestId("goal-edit"), { target: { value: "Next conversation edit" } })
  await act(async () => { acknowledge(true) })
  expect(screen.getByTestId("goal-edit")).toHaveValue("Next conversation edit")
  expect(nextEdit).not.toHaveBeenCalled()
  rerender(element("thread:next-owner", nextEdit, true))
  expect(screen.getByTestId("goal-edit")).toBeDisabled()
  expect(screen.getByRole("button", { name: "Clear goal" })).toBeDisabled()
})
