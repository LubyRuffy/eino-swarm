import { act, fireEvent, render, screen, waitFor } from "@testing-library/react"
import { describe, expect, it, vi } from "vitest"

import { Composer } from "./composer"
import { TooltipProvider } from "@/components/ui/tooltip"

function deferred<T>() {
  let resolve!: (value: T) => void
  const promise = new Promise<T>((done) => { resolve = done })
  return { promise, resolve }
}

function renderComposer(props: Partial<Parameters<typeof Composer>[0]> = {}) {
  return render(<TooltipProvider><Composer running={false} models={[]}
    onModelChange={vi.fn()} reasoningLevels={[]} onReasoningChange={vi.fn()}
    onSend={vi.fn()} onStop={vi.fn()} onUpload={vi.fn(async () => [])}
    focusSignal={0} {...props} /></TooltipProvider>)
}

describe("Composer accepted sends", () => {
  it("keeps the full draft until the server acknowledges the send", async () => {
    const sent = deferred<boolean>()
    const onQuotesChange = vi.fn()
    const onSend = vi.fn(() => sent.promise)
    renderComposer({ onSend, quotes: [{ id: "q1", text: "quoted context" }], onQuotesChange })
    const input = screen.getByTestId("composer-input")
    fireEvent.change(input, { target: { value: "unfinished request" } })
    fireEvent.keyDown(input, { key: "Enter", keyCode: 13 })
    expect(onSend).toHaveBeenCalledOnce()
    expect(input).toHaveValue("unfinished request")
    expect(onQuotesChange).not.toHaveBeenCalled()
    await act(async () => { sent.resolve(true); await sent.promise })
    await waitFor(() => expect(input).toHaveValue(""))
    expect(onQuotesChange).toHaveBeenCalledWith([])
  })

  it("retains the text when the send was refused", async () => {
    renderComposer({ onSend: vi.fn(async () => false) })
    const input = screen.getByTestId("composer-input")
    fireEvent.change(input, { target: { value: "retryable request" } })
    fireEvent.keyDown(input, { key: "Enter", keyCode: 13 })
    await act(async () => {})
    expect(input).toHaveValue("retryable request")
  })
})
