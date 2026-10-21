import { fireEvent, render, screen } from "@testing-library/react"
import { describe, expect, it } from "vitest"

import { emptyTranscript, reduceEvents } from "@/lib/transcript"
import type { SwarmEvent } from "@/lib/types"
import { Transcript } from "./transcript"

function event(seq: number, turn: string, kind: string, text = ""): SwarmEvent {
  return {
    seq, thread_id: "thread", turn_id: turn, kind, text,
    agent_id: "manager", created_at: new Date(Date.UTC(2026, 0, 1, 0, 0, seq)).toISOString(),
  }
}

function history(session = false): SwarmEvent[] {
  return [
    event(1, "first", "user_message", "opening request"),
    ...(session ? [event(2, "first", "goal_session")] : []),
    event(3, "first", "agent_message", "original answer"),
    event(4, "first", "done", "original answer"),
    event(5, "next", "user_message", "follow up"),
    event(6, "next", "agent_message", "latest answer"),
    event(7, "next", "done", "latest answer"),
  ]
}

function transcript(events: SwarmEvent[]) {
  return <Transcript state={reduceEvents(emptyTranscript(), events)} loaded onSelectAgent={() => {}} />
}

describe("late post-turn events", () => {
  it.each([false, true])("keeps the original completion before late review feedback (goal=%s)", (session) => {
    const before = history(session)
    const late = [
      event(8, "first", "session_memory", "internal briefing"),
      event(9, "first", "memory_review", JSON.stringify({ changed: false, err: "review fixture failed" })),
    ]
    const view = render(transcript(before))
    view.rerender(transcript([...before, ...late]))
    const footers = screen.getAllByTestId("turn-boundary")
    expect(footers).toHaveLength(2)
    expect(footers.map((row) => row.querySelector("time")?.dateTime)).toEqual([
      before.find((row) => row.seq === 4)?.created_at,
      before.find((row) => row.seq === 7)?.created_at,
    ])
    const failure = screen.getByText(/Memory review failed/)
    expect(failure).toBeVisible()
    expect(footers[1].compareDocumentPosition(failure) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
    expect(screen.queryByText("internal briefing")).toBeNull()
    if (session) fireEvent.click(screen.getByTestId("goal-session"))
    expect(screen.getByText("original answer")).toBeVisible()
    expect(screen.getByText("latest answer")).toBeVisible()
  })

  it("does not manufacture a footer from a metadata-only history tail", () => {
    const events = [
      ...history(),
      event(8, "first", "session_memory", "internal briefing"),
      event(9, "first", "title", "generated title"),
    ]
    const state = reduceEvents(emptyTranscript(), events)
    state.agents.manager.blocks = state.agents.manager.blocks.filter((row) => row.seq >= 8)
    const view = render(<Transcript state={state} loaded onSelectAgent={() => {}} />)
    expect(screen.queryAllByTestId("turn-boundary")).toHaveLength(0)
    view.rerender(transcript(events))
    expect(screen.getAllByTestId("turn-boundary")).toHaveLength(2)
    expect(screen.getByText("original answer")).toBeVisible()
  })

  it("does not fold a review that arrives immediately after its goal turn", () => {
    const events = history(true).filter((row) => row.turn_id === "first")
    events.push(event(8, "first", "memory_review", JSON.stringify({ changed: false, err: "late review failed" })))
    render(transcript(events))
    const footer = screen.getByTestId("turn-boundary")
    const failure = screen.getByText(/Memory review failed/)
    expect(failure).toBeVisible()
    expect(footer.compareDocumentPosition(failure) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy()
    expect(screen.getByTestId("goal-session")).toHaveAttribute("aria-expanded", "false")
  })

  it("keeps the running turn's progress visible when an older review arrives", () => {
    const events = history().filter((row) => row.seq <= 5)
    events.push(
      { ...event(6, "next", "tool_call", 'exec({"command":"printf check"})'), tool_call_id: "call" },
      { ...event(7, "next", "tool_result", '{"stdout":"checked","exit_code":0}'), tool_call_id: "call" },
      event(8, "next", "agent_message", "current progress"),
    )
    const view = render(transcript(events))
    expect(screen.getByTestId("planning-tail")).toBeVisible()
    view.rerender(transcript([...events,
      event(9, "first", "memory_review", JSON.stringify({ changed: false, err: "review failed" })),
    ]))
    expect(screen.getByTestId("planning-tail")).toBeVisible()
    expect(screen.getAllByTestId("turn-boundary")).toHaveLength(1)
    expect(screen.getByText(/Memory review failed/)).toBeVisible()
  })
})
