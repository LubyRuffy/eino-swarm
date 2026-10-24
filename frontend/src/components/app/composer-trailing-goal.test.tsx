import { act, fireEvent, render, screen } from "@testing-library/react"
import { expect, it, vi } from "vitest"

import { Composer } from "./composer"
import { TooltipProvider } from "@/components/ui/tooltip"

function renderComposer(running = false, complete = false) {
  const goal = vi.fn(async () => true)
  const send = vi.fn()
  const view = render(<TooltipProvider><Composer running={running} models={[]}
    onModelChange={vi.fn()} reasoningLevels={[]} onReasoningChange={vi.fn()}
    onSetGoal={goal} onSend={send} onStop={vi.fn()} onUpload={vi.fn(async () => [])}
    focusSignal={0} goal={complete ? "The previous objective" : undefined} goalComplete={complete} /></TooltipProvider>)
  return { ...view, goal, send, input: screen.getByTestId("composer-input") }
}

for (const running of [false, true]) {
  for (const complete of [false, true]) {
    for (const submit of ["click", "enter"] as const) {
      it(`submits the preceding objective by ${submit} with running=${running}, completed objective=${complete}`, async () => {
        const { goal, send, input } = renderComposer(running, complete)
        const objective = "Inspect the assigned operation.\n保留全部验证范围。"
        fireEvent.change(input, { target: { value: `${objective} /goal` } })
        expect(screen.getByRole("button", { name: "Send" })).toBeEnabled()
        expect(screen.queryByTestId("composer-command-hint")).not.toBeInTheDocument()
        await act(async () => {
          if (submit === "click") fireEvent.click(screen.getByRole("button", { name: "Send" }))
          else fireEvent.keyDown(input, { key: "Enter", keyCode: 13 })
        })
        expect(goal).toHaveBeenCalledTimes(1)
        expect(goal).toHaveBeenCalledWith(objective)
        expect(send).not.toHaveBeenCalled()
        expect(input).toHaveValue("")
      })
    }
  }
}

it("can send the existing objective after choosing goal from the menu", async () => {
  const { goal, send, input } = renderComposer()
  const objective = "Review the current operation"
  fireEvent.change(input, { target: { value: `${objective} /` } })
  fireEvent.click(screen.getByTestId("slash-command-goal"))
  expect(input).toHaveValue(`${objective} /goal `)
  expect(screen.getByRole("button", { name: "Send" })).toBeEnabled()
  await act(async () => { fireEvent.click(screen.getByRole("button", { name: "Send" })) })
  expect(goal).toHaveBeenCalledTimes(1)
  expect(goal).toHaveBeenCalledWith(objective)
  expect(send).not.toHaveBeenCalled()
})

for (const text of ["/goal", " /goal \n"]) {
  it(`keeps a genuinely empty goal disabled: ${JSON.stringify(text)}`, async () => {
    const { goal, send, input } = renderComposer()
    fireEvent.change(input, { target: { value: text } })
    expect(screen.getByRole("button", { name: "Send" })).toBeDisabled()
    await act(async () => { fireEvent.keyDown(input, { key: "Enter", keyCode: 13 }) })
    expect(goal).not.toHaveBeenCalled()
    expect(send).not.toHaveBeenCalled()
  })
}

for (const text of ["Inspect `/goal`", "Inspect /tmp/中文/goal", "Inspect ./中文/goal", 'Inspect "the /goal task"']) {
  it(`sends literal command evidence as an ordinary message: ${text}`, async () => {
    const { goal, send, input } = renderComposer()
    fireEvent.change(input, { target: { value: text } })
    expect(screen.queryByTestId("slash-menu")).not.toBeInTheDocument()
    expect(screen.getByRole("button", { name: "Send" })).toBeEnabled()
    await act(async () => { fireEvent.click(screen.getByRole("button", { name: "Send" })) })
    expect(send).toHaveBeenCalledTimes(1)
    expect(send).toHaveBeenCalledWith(text, undefined, { steer: false })
    expect(goal).not.toHaveBeenCalled()
  })
}
