import { fireEvent, render, screen } from "@testing-library/react"
import { describe, expect, it, vi } from "vitest"

import { setLocale } from "@/lib/i18n"
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
    const transcript = screen.getByTestId("transcript")
    expect(transcript).toHaveClass("min-h-0")
    expect(transcript).toHaveClass("overflow-y-scroll")
    expect(transcript).toHaveClass("overflow-x-hidden")
    expect(transcript).toHaveClass("min-w-0")
    expect(transcript.closest("main")).toHaveClass("h-full")
    expect(transcript.closest("main")).toHaveClass("min-w-0")
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
    expect(screen.getByTestId("goal-text")).toHaveTextContent("keep **going**")
    expect(screen.getByTestId("goal-text")).toHaveClass("truncate")
    expect(screen.getByText("Planning")).toBeInTheDocument()
    fireEvent.click(screen.getByRole("button", { name: "A" }))
    fireEvent.click(screen.getByTestId("ask-submit"))
    expect(onAnswerStructured).toHaveBeenCalledWith("c1", { q1: { answers: ["A"] } })
  })

  it("paints pursuing and a parked wait so a silent composer is not a freeze", () => {
    const onRunNow = vi.fn()
    const onCancelWait = vi.fn()
    const onResumeGoal = vi.fn()
    render(
      <ThreadScreen
        detail={{
          id: "t1",
          title: "live",
          goal: "keep going",
          goal_on: true,
          goal_started_at: "2026-09-21T00:00:00Z",
          waiting: true,
          wake: {
            id: "sch_1",
            title: "wake",
            prompt: "Continue the wait.",
            next_run_at: "2026-09-21T02:00:00Z",
          },
        }}
        blocks={[{ id: "1", kind: "notice", text: "A wait is armed." }]}
        onBack={vi.fn()}
        onSend={vi.fn()}
        onSteer={vi.fn()}
        onStop={vi.fn()}
        onAnswer={vi.fn()}
        onAnswerStructured={vi.fn()}
        onRunNow={onRunNow}
        onCancelWait={onCancelWait}
        onResumeGoal={onResumeGoal}
      />,
    )
    expect(screen.getByTestId("thread-status")).toHaveTextContent("Waiting")
    expect(screen.getByTestId("goal-banner")).toHaveTextContent("Pursuing")
    expect(screen.getByTestId("goal-banner")).toHaveTextContent("keep going")
    expect(screen.getByTestId("goal-banner")).toHaveTextContent("Parked until the next check")
    expect(screen.getByTestId("schedule-banner")).toHaveTextContent("Waiting")
    expect(screen.getByTestId("schedule-banner")).toHaveTextContent("wake")
    fireEvent.click(screen.getByRole("button", { name: "Run now" }))
    fireEvent.click(screen.getByRole("button", { name: "Cancel wait" }))
    expect(onRunNow).toHaveBeenCalled()
    expect(onCancelWait).toHaveBeenCalled()
    expect(screen.queryByRole("button", { name: "Start goal" })).not.toBeInTheDocument()
  })

  it("truncates a long goal so the wait banner and composer stay on screen", () => {
    render(
      <ThreadScreen
        detail={{
          id: "t1",
          title: "live",
          goal: ["keep going", "first slice of the plan.", "later slice of the plan."].join("\n\n"),
          goal_on: true,
          waiting: true,
          wake: {
            id: "sch_1",
            title: "wake",
            next_run_at: "2026-09-21T02:00:00Z",
          },
        }}
        blocks={[]}
        onBack={vi.fn()}
        onSend={vi.fn()}
        onSteer={vi.fn()}
        onStop={vi.fn()}
        onAnswer={vi.fn()}
        onAnswerStructured={vi.fn()}
        onRunNow={vi.fn()}
        onCancelWait={vi.fn()}
      />,
    )
    const text = screen.getByTestId("goal-text")
    expect(text).toHaveClass("truncate")
    expect(screen.getByTestId("goal-banner").querySelectorAll("p")).toHaveLength(2)
    expect(screen.getByTestId("schedule-banner")).toBeInTheDocument()
    expect(screen.getByRole("button", { name: "Run now" })).toBeInTheDocument()
    expect(screen.getByRole("button", { name: "Send" })).toBeInTheDocument()
  })

  it("offers Start on a paused goal", () => {
    const onResumeGoal = vi.fn()
    render(
      <ThreadScreen
        detail={{
          id: "t1",
          title: "live",
          goal: "keep going",
          goal_on: true,
          goal_capped: true,
        }}
        blocks={[]}
        onBack={vi.fn()}
        onSend={vi.fn()}
        onSteer={vi.fn()}
        onStop={vi.fn()}
        onAnswer={vi.fn()}
        onAnswerStructured={vi.fn()}
        onResumeGoal={onResumeGoal}
      />,
    )
    expect(screen.getByTestId("goal-banner")).toHaveTextContent("Paused")
    fireEvent.click(screen.getByRole("button", { name: "Start goal" }))
    expect(onResumeGoal).toHaveBeenCalled()
  })

  it("shows Done and Blocked without a live wait", () => {
    const { rerender } = render(
      <ThreadScreen
        detail={{
          id: "t1",
          title: "live",
          goal: "keep going",
          goal_on: true,
          goal_complete: true,
        }}
        blocks={[]}
        onBack={vi.fn()}
        onSend={vi.fn()}
        onSteer={vi.fn()}
        onStop={vi.fn()}
        onAnswer={vi.fn()}
        onAnswerStructured={vi.fn()}
        onResumeGoal={vi.fn()}
      />,
    )
    expect(screen.getByTestId("goal-banner")).toHaveTextContent("Done")
    rerender(
      <ThreadScreen
        detail={{
          id: "t1",
          title: "live",
          goal: "keep going",
          goal_on: true,
          goal_blocked: true,
          goal_block_reason: "the last turn failed",
        }}
        blocks={[]}
        onBack={vi.fn()}
        onSend={vi.fn()}
        onSteer={vi.fn()}
        onStop={vi.fn()}
        onAnswer={vi.fn()}
        onAnswerStructured={vi.fn()}
      />,
    )
    expect(screen.getByTestId("goal-banner")).toHaveTextContent("Blocked")
    expect(screen.getByTestId("goal-banner")).toHaveTextContent("The last turn failed.")
    rerender(
      <ThreadScreen
        detail={{
          id: "t1",
          title: "live",
          goal: "keep going",
          goal_on: true,
          goal_blocked: true,
          goal_block_reason: "",
        }}
        blocks={[]}
        onBack={vi.fn()}
        onSend={vi.fn()}
        onSteer={vi.fn()}
        onStop={vi.fn()}
        onAnswer={vi.fn()}
        onAnswerStructured={vi.fn()}
      />,
    )
    expect(screen.getByTestId("goal-banner")).toHaveTextContent("Blocked")
    expect(screen.getByTestId("goal-banner")).not.toHaveTextContent(
      "The last turn failed.",
    )
  })

  it("uses the wait prompt when the title is empty and shows an idle hold", () => {
    render(
      <ThreadScreen
        detail={{
          id: "t1",
          title: "live",
          goal: "keep going",
          goal_on: true,
          goal_idle: true,
          waiting: true,
          wake: { id: "sch_1", prompt: "Continue the wait.", next_run_at: "not-a-date" },
        }}
        blocks={[]}
        onBack={vi.fn()}
        onSend={vi.fn()}
        onSteer={vi.fn()}
        onStop={vi.fn()}
        onAnswer={vi.fn()}
        onAnswerStructured={vi.fn()}
        onResumeGoal={vi.fn()}
        onRunNow={vi.fn()}
        onCancelWait={vi.fn()}
      />,
    )
    expect(screen.getByTestId("goal-banner")).toHaveTextContent("Paused")
    expect(screen.getByTestId("schedule-banner")).toHaveTextContent("Continue the wait.")
    expect(screen.getByTestId("schedule-banner")).toHaveTextContent("not-a-date")
  })

  it("keeps a tool result collapsed until tapped", () => {
    setLocale("en")
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
    expect(screen.getByTestId("work-fold")).toHaveTextContent("1 tool")
    expect(screen.queryByText("exec")).not.toBeInTheDocument()
    expect(screen.queryByText("worker")).not.toBeInTheDocument()
    expect(document.body.textContent).not.toContain('{"n":1')
    fireEvent.click(screen.getByTestId("work-fold"))
    expect(screen.getByText("exec")).toBeInTheDocument()
    fireEvent.click(screen.getByRole("button", { name: /exec/ }))
    expect(document.body.textContent).toContain('{"n":1')
  })

  it("keeps Earlier outside the scroller so a live-edge tail can still page", () => {
    render(
      <ThreadScreen
        detail={{ id: "t1", title: "live" }}
        blocks={[{ id: "1", kind: "answer", text: "now" }]}
        hasMore
        onBack={vi.fn()}
        onOlder={vi.fn()}
        onSend={vi.fn()}
        onSteer={vi.fn()}
        onStop={vi.fn()}
        onAnswer={vi.fn()}
        onAnswerStructured={vi.fn()}
      />,
    )
    const earlier = screen.getByRole("button", { name: "Earlier" })
    const transcript = screen.getByTestId("transcript")
    expect(transcript.contains(earlier)).toBe(false)
    expect(transcript.tagName).not.toBe("OL")
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
    const transcript = screen.getByTestId("transcript")
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
    const transcript = screen.getByTestId("transcript")
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
    const transcript = screen.getByTestId("transcript")
    expect(transcript).toHaveClass("invisible")
    Object.defineProperty(transcript, "scrollTop", { value: 4, configurable: true })
    Object.defineProperty(transcript, "scrollHeight", { value: 800, configurable: true })
    Object.defineProperty(transcript, "clientHeight", { value: 400, configurable: true })
    fireEvent.scroll(transcript)
    expect(onOlder).not.toHaveBeenCalled()
  })
})
