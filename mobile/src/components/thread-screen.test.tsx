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
  })

  it("shows a standing goal and the ask card", () => {
    const onAnswerStructured = vi.fn()
    render(
      <ThreadScreen
        detail={{
          id: "t1",
          title: "live",
          goal: "keep going",
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
    expect(screen.getByText("keep going")).toBeInTheDocument()
    expect(screen.getByText("Planning")).toBeInTheDocument()
    fireEvent.click(screen.getByRole("button", { name: "A" }))
    fireEvent.click(screen.getByTestId("ask-submit"))
    expect(onAnswerStructured).toHaveBeenCalledWith("c1", { q1: { answers: ["A"] } })
  })
})
