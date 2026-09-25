import { fireEvent, render, screen } from "@testing-library/react"
import { describe, expect, it, vi } from "vitest"

import { setLocale } from "@/lib/i18n"
import { ThreadScreen } from "./thread-screen"
import { applyEvent, type CompactBlock } from "@/lib/transcript"
import type { RemoteEvent } from "@/lib/rpc"

describe("ThreadScreen", () => {
  it("opens a worker's separate activity without painting it as the manager answer", () => {
    setLocale("en")
    let blocks: CompactBlock[] = []
    const events: Partial<RemoteEvent>[] = [
      { seq: 1, kind: "spawned", agent_id: "worker-1", role: "reader", text: "" },
      { seq: 2, kind: "agent_message", agent_id: "manager", text: "manager answer" },
      { seq: 3, kind: "reasoning", agent_id: "worker-1", text: "worker thought" },
      { seq: 4, kind: "tool_call", agent_id: "worker-1", tool_call_id: "c1", text: "read({})" },
      { seq: 5, kind: "agent_message", agent_id: "worker-1", text: "worker answer" },
      { seq: 6, kind: "finished", agent_id: "worker-1", text: "worker answer" },
    ]
    for (const event of events) blocks = applyEvent(blocks, {
      thread_id: "t1", created_at: "2026-09-25T00:00:00Z", text: "", ...event,
    } as RemoteEvent)
    render(<ThreadScreen detail={{ id: "t1", title: "talk" }} blocks={blocks}
      onBack={vi.fn()} onSend={vi.fn()} onSteer={vi.fn()} onStop={vi.fn()}
      onAnswer={vi.fn()} onAnswerStructured={vi.fn()} />)
    expect(screen.getByText("manager answer")).toBeInTheDocument()
    expect(screen.queryByText("worker answer")).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole("button", { name: /agents/i }))
    expect(screen.getByRole("button", { name: /reader.*worker-1.*done/i })).toBeInTheDocument()
    fireEvent.click(screen.getByRole("button", { name: /reader.*worker-1.*done/i }))
    expect(screen.getByText("worker answer")).toBeInTheDocument()
    expect(screen.queryByText("manager answer")).not.toBeInTheDocument()
    expect(screen.queryByText(/launch instruction/i)).not.toBeInTheDocument()
  })

  it("shows a failed worker's error in its own activity view", () => {
    setLocale("en")
    let blocks = applyEvent([], { thread_id: "t1", seq: 1, kind: "spawned", agent_id: "w1", role: "reader", text: "", created_at: "2026-09-25T00:00:00Z" })
    blocks = applyEvent(blocks, { thread_id: "t1", seq: 2, kind: "finished", agent_id: "w1", text: "", err: "worker timed out", created_at: "2026-09-25T00:00:01Z" })
    render(<ThreadScreen detail={{ id: "t1", title: "talk" }} blocks={blocks}
      onBack={vi.fn()} onSend={vi.fn()} onSteer={vi.fn()} onStop={vi.fn()}
      onAnswer={vi.fn()} onAnswerStructured={vi.fn()} />)
    fireEvent.click(screen.getByRole("button", { name: /agents/i }))
    fireEvent.click(screen.getByRole("button", { name: /reader.*w1.*failed/i }))
    expect(screen.getByText("worker timed out")).toBeInTheDocument()
  })
  it("adds selected transcript text to the draft and sends it as a separate quote", () => {
    setLocale("en")
    const onSend = vi.fn()
    render(
      <ThreadScreen
        detail={{ id: "t1", title: "talk" }}
        blocks={[{ id: "a1", kind: "answer", text: "alpha beta" }]}
        onBack={vi.fn()}
        onSend={onSend}
        onSteer={vi.fn()}
        onStop={vi.fn()}
        onAnswer={vi.fn()}
        onAnswerStructured={vi.fn()}
      />,
    )
    const source = screen.getByText("alpha beta")
    const range = document.createRange()
    range.selectNodeContents(source)
    window.getSelection()?.removeAllRanges()
    window.getSelection()?.addRange(range)
    fireEvent(document, new Event("selectionchange"))
    fireEvent.click(screen.getByRole("button", { name: "Add to chat" }))
    expect(screen.getByRole("textbox", { name: "Edit quote 1" })).toHaveValue("alpha beta")
    fireEvent.change(screen.getByLabelText("Message"), { target: { value: "explain" } })
    fireEvent.click(screen.getByRole("button", { name: "Send" }))
    expect(onSend).toHaveBeenCalledWith(
      "<selected_text>\nalpha beta\n</selected_text>\n\n<user_request>\nexplain\n</user_request>",
    )
  })

  it("does not quote selected composer text", () => {
    setLocale("en")
    render(
      <ThreadScreen
        detail={{ id: "t1", title: "talk" }}
        blocks={[{ id: "a1", kind: "answer", text: "alpha beta" }]}
        onBack={vi.fn()}
        onSend={vi.fn()}
        onSteer={vi.fn()}
        onStop={vi.fn()}
        onAnswer={vi.fn()}
        onAnswerStructured={vi.fn()}
      />,
    )
    const box = screen.getByLabelText("Message")
    fireEvent.change(box, { target: { value: "outside" } })
    const range = document.createRange()
    range.selectNodeContents(box)
    window.getSelection()?.removeAllRanges()
    window.getSelection()?.addRange(range)
    fireEvent(document, new Event("selectionchange"))
    expect(screen.queryByRole("button", { name: "Add to chat" })).not.toBeInTheDocument()
  })

  it("does not quote tool control text", () => {
    setLocale("en")
    render(
      <ThreadScreen
        detail={{ id: "t1", title: "talk" }}
        blocks={[{ id: "tool1", kind: "tool", text: "opened", toolName: "read_file" }]}
        onBack={vi.fn()}
        onSend={vi.fn()}
        onSteer={vi.fn()}
        onStop={vi.fn()}
        onAnswer={vi.fn()}
        onAnswerStructured={vi.fn()}
      />,
    )
    fireEvent.click(screen.getByTestId("work-fold"))
    const source = screen.getByText("read_file")
    const range = document.createRange()
    range.selectNodeContents(source)
    window.getSelection()?.removeAllRanges()
    window.getSelection()?.addRange(range)
    fireEvent(document, new Event("selectionchange"))
    expect(screen.queryByRole("button", { name: "Add to chat" })).not.toBeInTheDocument()
  })

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

  it("does not ask for another page just because the transcript is at the top", () => {
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
    // Sitting at the top must not ask again. That tripwire kept 加载中 up
    // while the last turn stayed on screen.
    expect(onOlder).toHaveBeenCalledTimes(1)
  })

  it("does not ask again when an earlier page adds nothing", () => {
    const onOlder = vi.fn()
    const props = {
      detail: { id: "t1", title: "live" },
      blocks: [{ id: "1", kind: "answer" as const, text: "now" }],
      hasMore: true,
      onBack: vi.fn(),
      onOlder,
      onSend: vi.fn(),
      onSteer: vi.fn(),
      onStop: vi.fn(),
      onAnswer: vi.fn(),
      onAnswerStructured: vi.fn(),
    }
    const view = render(<ThreadScreen {...props} loadingOlder />)
    view.rerender(<ThreadScreen {...props} />)
    fireEvent.click(screen.getByRole("button", { name: "Earlier" }))
    expect(onOlder).toHaveBeenCalledTimes(1)
    view.rerender(<ThreadScreen {...props} loadingOlder />)
    view.rerender(<ThreadScreen {...props} />)
    fireEvent.scroll(screen.getByTestId("transcript"))
    expect(onOlder).toHaveBeenCalledTimes(1)
    fireEvent.click(screen.getByRole("button", { name: "Earlier" }))
    expect(onOlder).toHaveBeenCalledTimes(2)
  })

  it("hides interrupt until a follow-up is actually waiting", () => {
    const onInterrupt = vi.fn()
    const detail = {
      id: "t1",
      title: "live",
      running: { thread_id: "t1", title: "live" },
    }
    const handlers = {
      onBack: vi.fn(),
      onSend: vi.fn(),
      onSteer: vi.fn(),
      onStop: vi.fn(),
      onAnswer: vi.fn(),
      onAnswerStructured: vi.fn(),
      onInterrupt,
    }
    const view = render(<ThreadScreen detail={detail} blocks={[]} {...handlers} />)
    expect(screen.queryByRole("button", { name: "Interrupt" })).not.toBeInTheDocument()
    expect(screen.getByRole("button", { name: "Stop" })).toBeInTheDocument()
    view.rerender(
      <ThreadScreen
        detail={detail}
        blocks={[]}
        followups={[{ id: "f1", seq: 1, text: "later" }]}
        {...handlers}
      />,
    )
    fireEvent.click(screen.getByRole("button", { name: "Interrupt" }))
    expect(onInterrupt).toHaveBeenCalledTimes(1)
    expect(onInterrupt).toHaveBeenCalledWith("f1")
    expect(screen.getByTestId("followup-queue")).toContainElement(
      screen.getByRole("button", { name: "Interrupt" }),
    )
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

  // A two-line conversation used to float under a screen of blank, with the
  // only content pinned to the top and the composer far below it.
  it("sits a short conversation on top of the composer, not under a blank screen", () => {
    render(
      <ThreadScreen
        detail={{ id: "t1", title: "live" }}
        blocks={[{ id: "1", kind: "answer", text: "now" }]}
        onBack={vi.fn()}
        onSend={vi.fn()}
        onSteer={vi.fn()}
        onStop={vi.fn()}
        onAnswer={vi.fn()}
        onAnswerStructured={vi.fn()}
      />,
    )
    const column = screen.getByTestId("transcript").firstElementChild
    expect(column).toHaveClass("justify-end")
    expect(column).toHaveClass("min-h-full")
  })

  it("offers a way back to the tail once the reader scrolls away", () => {
    render(
      <ThreadScreen
        detail={{ id: "t1", title: "live" }}
        blocks={[{ id: "1", kind: "answer", text: "now" }]}
        onBack={vi.fn()}
        onSend={vi.fn()}
        onSteer={vi.fn()}
        onStop={vi.fn()}
        onAnswer={vi.fn()}
        onAnswerStructured={vi.fn()}
      />,
    )
    const transcript = screen.getByTestId("transcript")
    expect(screen.queryByTestId("to-latest")).not.toBeInTheDocument()
    const scrollTo = vi.fn()
    Object.defineProperty(transcript, "scrollTo", { value: scrollTo, configurable: true })
    // Writable: sticking to the tail assigns scrollTop on every repaint.
    Object.defineProperty(transcript, "scrollTop", {
      value: 0,
      configurable: true,
      writable: true,
    })
    Object.defineProperty(transcript, "scrollHeight", { value: 2000, configurable: true })
    Object.defineProperty(transcript, "clientHeight", { value: 400, configurable: true })
    fireEvent.scroll(transcript)
    fireEvent.click(screen.getByTestId("to-latest"))
    expect(scrollTo).toHaveBeenCalled()
  })

  // A streaming answer leaves a few pixels of slack. A button that appears
  // for those is a button that is always on screen.
  it("does not offer that button for the slack a live answer leaves", () => {
    render(
      <ThreadScreen
        detail={{ id: "t1", title: "live" }}
        blocks={[{ id: "1", kind: "answer", text: "now" }]}
        onBack={vi.fn()}
        onSend={vi.fn()}
        onSteer={vi.fn()}
        onStop={vi.fn()}
        onAnswer={vi.fn()}
        onAnswerStructured={vi.fn()}
      />,
    )
    const transcript = screen.getByTestId("transcript")
    Object.defineProperty(transcript, "scrollTop", { value: 1440, configurable: true })
    Object.defineProperty(transcript, "scrollHeight", { value: 2000, configurable: true })
    Object.defineProperty(transcript, "clientHeight", { value: 400, configurable: true })
    fireEvent.scroll(transcript)
    expect(screen.queryByTestId("to-latest")).not.toBeInTheDocument()
  })

  it("says a follow-up is queued, and drops steer once the turn ends", () => {
    setLocale("en")
    const { rerender } = render(
      <ThreadScreen
        detail={{ id: "t1", title: "live", running: { thread_id: "t1", title: "live" } }}
        blocks={[]}
        onBack={vi.fn()}
        onSend={vi.fn()}
        onSteer={vi.fn()}
        onStop={vi.fn()}
        onAnswer={vi.fn()}
        onAnswerStructured={vi.fn()}
      />,
    )
    expect(screen.getByText("Queued after this turn")).toBeInTheDocument()
    expect(screen.getByRole("button", { name: "Steer" })).toBeInTheDocument()
    rerender(
      <ThreadScreen
        detail={{ id: "t1", title: "live" }}
        blocks={[]}
        onBack={vi.fn()}
        onSend={vi.fn()}
        onSteer={vi.fn()}
        onStop={vi.fn()}
        onAnswer={vi.fn()}
        onAnswerStructured={vi.fn()}
      />,
    )
    expect(screen.queryByText("Queued after this turn")).not.toBeInTheDocument()
    expect(screen.queryByRole("button", { name: "Steer" })).not.toBeInTheDocument()
    expect(screen.getByRole("button", { name: "Send" })).toBeInTheDocument()
  })

  // Steering a question would drop the answer the run is parked on.
  it("routes the box to the answer while a question is pending", () => {
    setLocale("en")
    const onAnswer = vi.fn()
    render(
      <ThreadScreen
        detail={{
          id: "t1",
          title: "live",
          running: { thread_id: "t1", title: "live", ask_user: true },
        }}
        blocks={[]}
        onBack={vi.fn()}
        onSend={vi.fn()}
        onSteer={vi.fn()}
        onStop={vi.fn()}
        onAnswer={onAnswer}
        onAnswerStructured={vi.fn()}
      />,
    )
    expect(screen.queryByRole("button", { name: "Steer" })).not.toBeInTheDocument()
    fireEvent.change(screen.getByLabelText("Your answer"), { target: { value: "B" } })
    fireEvent.click(screen.getByRole("button", { name: "Answer" }))
    expect(onAnswer).toHaveBeenCalledWith("B")
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
    expect(screen.queryByRole("status")).not.toBeInTheDocument()
    Object.defineProperty(transcript, "scrollTop", { value: 4, configurable: true })
    Object.defineProperty(transcript, "scrollHeight", { value: 800, configurable: true })
    Object.defineProperty(transcript, "clientHeight", { value: 400, configurable: true })
    fireEvent.scroll(transcript)
    expect(onOlder).not.toHaveBeenCalled()
  })

  it("shows loading outside the transcript when the conversation has not arrived", () => {
    setLocale("en")
    render(
      <ThreadScreen
        detail={{ id: "t1", title: "live" }}
        blocks={[]}
        caughtUp={false}
        onBack={vi.fn()}
        onSend={vi.fn()}
        onSteer={vi.fn()}
        onStop={vi.fn()}
        onAnswer={vi.fn()}
        onAnswerStructured={vi.fn()}
      />,
    )
    const status = screen.getByRole("status")
    expect(status).toHaveTextContent("Loading")
    expect(screen.queryByTestId("transcript")).not.toBeInTheDocument()
  })
})
