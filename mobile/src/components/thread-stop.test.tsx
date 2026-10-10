import { fireEvent, render, screen, within } from "@testing-library/react"
import { describe, expect, it, vi } from "vitest"

import { setLocale } from "@/lib/i18n"
import type { CompactBlock } from "@/lib/transcript"
import type { ThreadDetail } from "@/lib/rpc"
import { ThreadScreen } from "./thread-screen"

function harness(detail: ThreadDetail, blocks: CompactBlock[] = []) {
  setLocale("en")
  const handlers = {
    onBack: vi.fn(), onSend: vi.fn(), onSteer: vi.fn(), onStop: vi.fn(),
    onAnswer: vi.fn(), onAnswerStructured: vi.fn(),
  }
  const view = render(<ThreadScreen detail={detail} blocks={blocks} {...handlers} />)
  const input = screen.getByRole("textbox")
  const form = within(input.closest("form")!)
  return { ...view, ...handlers, input, form }
}

describe("phone live-turn primary action", () => {
  it("stops from an empty composer, keeps clickable follow-up and steer for a draft, then returns to Send", () => {
    const live: ThreadDetail = { id: "t1", title: "live", running: { thread_id: "t1", title: "live" } }
    const view = harness(live)
    const stop = view.form.getByRole("button", { name: "Stop" })
    expect(stop).toBeEnabled()
    fireEvent.click(stop)
    expect(view.onStop).toHaveBeenCalledOnce()
    expect(view.onSend).not.toHaveBeenCalled()
    fireEvent.change(view.input, { target: { value: "   " } })
    expect(view.form.getByRole("button", { name: "Stop" })).toBeEnabled()
    fireEvent.change(view.input, { target: { value: "next" } })
    expect(view.form.queryByRole("button", { name: "Stop" })).not.toBeInTheDocument()
    fireEvent.click(view.form.getByRole("button", { name: "Follow-up" }))
    expect(view.onSend).toHaveBeenCalledWith("next")
    fireEvent.change(view.input, { target: { value: "nudge" } })
    fireEvent.click(view.form.getByRole("button", { name: "Steer" }))
    expect(view.onSteer).toHaveBeenCalledWith("nudge")
    view.rerender(<ThreadScreen detail={{ id: "t1", title: "live" }} blocks={[]}
      onBack={view.onBack} onSend={view.onSend} onSteer={view.onSteer} onStop={view.onStop}
      onAnswer={view.onAnswer} onAnswerStructured={view.onAnswerStructured} />)
    expect(view.form.queryByRole("button", { name: "Stop" })).not.toBeInTheDocument()
    expect(view.form.getByRole("button", { name: "Send" })).toBeDisabled()
  })

  it("answers an active question instead of stopping it and never offers Stop for a parked wait", () => {
    const ask: CompactBlock = { id: "q1", kind: "question", text: "Which?", pending: true, callId: "call-1", questions: [] }
    const view = harness({ id: "t1", title: "question", running: { thread_id: "t1", title: "question" } }, [ask])
    expect(view.form.queryByRole("button", { name: "Stop" })).not.toBeInTheDocument()
    fireEvent.change(view.input, { target: { value: "answer" } })
    fireEvent.click(view.form.getByRole("button", { name: "Answer" }))
    expect(view.onAnswer).toHaveBeenCalledWith("answer")
    expect(view.onStop).not.toHaveBeenCalled()
    view.rerender(<ThreadScreen detail={{ id: "t1", title: "wait", waiting: true }} blocks={[]}
      onBack={view.onBack} onSend={view.onSend} onSteer={view.onSteer} onStop={view.onStop}
      onAnswer={view.onAnswer} onAnswerStructured={view.onAnswerStructured} />)
    expect(screen.queryByRole("button", { name: "Stop" })).not.toBeInTheDocument()
  })
})
