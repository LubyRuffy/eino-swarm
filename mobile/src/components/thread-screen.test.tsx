import { fireEvent, render, screen } from "@testing-library/react"
import { describe, expect, it, vi } from "vitest"

import { ThreadScreen } from "./thread-screen"

describe("ThreadScreen", () => {
  it("maps send, steer and stop onto the live thread", () => {
    const onSend = vi.fn()
    const onSteer = vi.fn()
    const onStop = vi.fn()
    render(
      <ThreadScreen
        detail={{
          id: "t1",
          title: "live",
          running: { thread_id: "t1", title: "live", action: "write" },
        }}
        blocks={[{ id: "1", kind: "answer", text: "done bit" }]}
        onBack={vi.fn()}
        onSend={onSend}
        onSteer={onSteer}
        onStop={onStop}
        onAnswer={vi.fn()}
        onAnswerStructured={vi.fn()}
      />,
    )
    fireEvent.click(screen.getByRole("button", { name: "Stop" }))
    expect(onStop).toHaveBeenCalled()
    fireEvent.change(screen.getByLabelText("Message"), {
      target: { value: "keep going" },
    })
    fireEvent.click(screen.getByRole("button", { name: "Follow-up" }))
    expect(onSend).toHaveBeenCalledWith("keep going")
    fireEvent.change(screen.getByLabelText("Message"), {
      target: { value: "nudge" },
    })
    fireEvent.click(screen.getByRole("button", { name: "Steer" }))
    expect(onSteer).toHaveBeenCalledWith("nudge")
    expect(screen.getByRole("button", { name: "Back" })).toBeInTheDocument()
    const transcript = document.querySelector("ol")
    expect(transcript).toHaveClass("min-h-0")
    expect(transcript?.closest("main")).toHaveClass("h-full")
  })

  it("shows a standing goal and the ask card", () => {
    const onAnswerStructured = vi.fn()
    render(
      <ThreadScreen
        detail={{
          id: "t1",
          title: "live",
          goal: "keep **going**",
          goal_on: true,
          plan_on: true,
        }}
        blocks={[
          {
            id: "q",
            kind: "question",
            text: "",
            pending: true,
            callId: "c1",
            questions: [
              {
                id: "q1",
                prompt: "Which?",
                options: [
                  { id: "a", label: "A" },
                  { id: "b", label: "B" },
                  { id: "other", label: "Other" },
                ],
              },
            ],
          },
        ]}
        onBack={vi.fn()}
        onSend={vi.fn()}
        onSteer={vi.fn()}
        onStop={vi.fn()}
        onAnswer={vi.fn()}
        onAnswerStructured={onAnswerStructured}
      />,
    )
    expect(screen.getByText("going").closest("strong")).toBeTruthy()
    expect(screen.queryByText(/\*\*going\*\*/)).not.toBeInTheDocument()
    expect(screen.getByText("Planning")).toBeInTheDocument()
    fireEvent.click(screen.getByRole("button", { name: "A" }))
    fireEvent.click(screen.getByTestId("ask-submit"))
    expect(onAnswerStructured).toHaveBeenCalledWith("c1", { q1: { answers: ["A"] } })
  })

  it("keeps a tool result collapsed until tapped", () => {
    render(
      <ThreadScreen
        detail={{ id: "t1", title: "live" }}
        blocks={[
          {
            id: "tool-1",
            kind: "tool",
            toolName: "exec",
            text: '{"n":1,"ok":true}',
          },
          { id: "spawn-1", kind: "spawn", text: "worker" },
        ]}
        onBack={vi.fn()}
        onSend={vi.fn()}
        onSteer={vi.fn()}
        onStop={vi.fn()}
        onAnswer={vi.fn()}
        onAnswerStructured={vi.fn()}
      />,
    )
    expect(screen.getByText("exec")).toBeInTheDocument()
    expect(document.body.textContent).not.toContain('{"n":1')
    expect(screen.queryByText("worker")).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole("button", { name: /exec/ }))
    expect(document.body.textContent).toContain('{"n":1')
  })

  it("asks for earlier rows when the transcript is pulled up", () => {
    const onOlder = vi.fn()
    render(
      <ThreadScreen
        detail={{ id: "t1", title: "live" }}
        blocks={[{ id: "1", kind: "answer", text: "now" }]}
        hasMore
        onBack={vi.fn()}
        onOlder={onOlder}
        onSend={vi.fn()}
        onSteer={vi.fn()}
        onStop={vi.fn()}
        onAnswer={vi.fn()}
        onAnswerStructured={vi.fn()}
      />,
    )
    fireEvent.click(screen.getByRole("button", { name: "Earlier" }))
    expect(onOlder).toHaveBeenCalledTimes(1)
    const transcript = document.querySelector("ol")
    if (!transcript) throw new Error("missing transcript")
    Object.defineProperty(transcript, "scrollTop", { value: 4, configurable: true })
    Object.defineProperty(transcript, "scrollHeight", { value: 400, configurable: true })
    Object.defineProperty(transcript, "clientHeight", { value: 400, configurable: true })
    fireEvent.scroll(transcript)
    expect(onOlder).toHaveBeenCalledTimes(2)
  })

  it("loads earlier rows when the finger pulls down at the top", () => {
    const onOlder = vi.fn()
    render(
      <ThreadScreen
        detail={{ id: "t1", title: "live" }}
        blocks={[{ id: "1", kind: "answer", text: "now" }]}
        hasMore
        onBack={vi.fn()}
        onOlder={onOlder}
        onSend={vi.fn()}
        onSteer={vi.fn()}
        onStop={vi.fn()}
        onAnswer={vi.fn()}
        onAnswerStructured={vi.fn()}
      />,
    )
    const transcript = document.querySelector("ol")
    if (!transcript) throw new Error("missing transcript")
    Object.defineProperty(transcript, "scrollTop", { value: 0, configurable: true })
    fireEvent.touchStart(transcript, { touches: [{ clientY: 80 }] })
    fireEvent.touchMove(transcript, { touches: [{ clientY: 140 }] })
    expect(onOlder).toHaveBeenCalledTimes(1)
  })

  it("holds the transcript until watch is caught up", () => {
    const onOlder = vi.fn()
    render(
      <ThreadScreen
        detail={{ id: "t1", title: "live" }}
        blocks={[{ id: "1", kind: "answer", text: "now" }]}
        hasMore
        caughtUp={false}
        onBack={vi.fn()}
        onOlder={onOlder}
        onSend={vi.fn()}
        onSteer={vi.fn()}
        onStop={vi.fn()}
        onAnswer={vi.fn()}
        onAnswerStructured={vi.fn()}
      />,
    )
    expect(screen.getByText("now")).toBeInTheDocument()
    expect(screen.getByRole("button", { name: "Loading" })).toBeDisabled()
    expect(document.querySelector("ol")).toHaveClass("invisible")
    const transcript = document.querySelector("ol")
    if (!transcript) throw new Error("missing transcript")
    Object.defineProperty(transcript, "scrollTop", { value: 4, configurable: true })
    Object.defineProperty(transcript, "scrollHeight", { value: 800, configurable: true })
    Object.defineProperty(transcript, "clientHeight", { value: 400, configurable: true })
    fireEvent.scroll(transcript)
    expect(onOlder).not.toHaveBeenCalled()
  })
})
