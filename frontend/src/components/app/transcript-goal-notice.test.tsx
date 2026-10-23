import { act, fireEvent, render, screen } from "@testing-library/react"
import { describe, expect, it } from "vitest"

import { Transcript } from "./transcript"
import { TraceTab } from "./trace-tab"
import { emptyTranscript, reduceEvents, type TranscriptState } from "@/lib/transcript"
import type { SwarmEvent } from "@/lib/types"
import { useApp } from "@/store/app"

function event(kind: SwarmEvent["kind"], seq: number, extra: Partial<SwarmEvent> = {}): SwarmEvent {
  return {
    kind, seq, thread_id: "thread", turn_id: "previous", agent_id: "manager",
    created_at: `2026-01-01T00:00:${String(seq).padStart(2, "0")}.000Z`,
    ...extra,
  }
}

function notices(state: TranscriptState) {
  return state.agents.manager.blocks.filter((block) => block.kind === "notice")
}

describe("human goal resume notices", () => {
  it("shows one notice across the actual old-turn resume and new-turn continuation", () => {
    act(() => useApp.setState({ locale: "zh" }))
    const resumed = event("goal_resumed", 1, { role: "human" })
    const continued = event("goal_continued", 2, { turn_id: "next" })
    const waiting = reduceEvents(emptyTranscript(), [resumed])
    const view = render(<Transcript state={waiting} loaded onSelectAgent={() => {}} />)
    expect(screen.getAllByText("继续追求目标。")).toHaveLength(1)

    const running = reduceEvents(waiting, [continued])
    view.rerender(<Transcript state={running} loaded onSelectAgent={() => {}} />)
    expect(screen.getAllByText("继续追求目标。")).toHaveLength(1)
    expect(notices(running).map((block) => block.seq)).toEqual([1, 2])
  })

  it("retains both original notices in their turns' Full log after the display is merged", () => {
    const state = reduceEvents(emptyTranscript(), [
      event("goal_resumed", 1, { role: "human" }),
      event("goal_continued", 2, { turn_id: "next" }),
    ])
    const view = render(<TraceTab turns={[]} transcript={state} focusedTurnId="previous" />)
    expect(screen.getByText("Resuming the standing objective.")).toBeInTheDocument()
    view.rerender(<TraceTab turns={[]} transcript={state} focusedTurnId="next" />)
    expect(screen.getByText("Continuing the standing objective.")).toBeInTheDocument()
  })

  it("keeps later identical continuation notices and manager reopens visible", () => {
    act(() => useApp.setState({ locale: "zh" }))
    const state = reduceEvents(emptyTranscript(), [
      event("goal_resumed", 1, { role: "human" }),
      event("goal_continued", 2, { turn_id: "next" }),
      event("goal_continued", 3, { turn_id: "later" }),
      event("goal_resumed", 4, { turn_id: "later" }),
    ])
    render(<Transcript state={state} loaded onSelectAgent={() => {}} />)
    expect(screen.getAllByText("继续追求目标。")).toHaveLength(3)
  })

  it("keeps the single notice visible in a completed session's expanded work", () => {
    act(() => useApp.setState({ locale: "zh" }))
    const state = reduceEvents(emptyTranscript(), [
      event("goal_resumed", 1, { role: "human" }),
      event("goal_continued", 2, { turn_id: "next" }),
      event("done", 3, { turn_id: "next", text: "progress" }),
    ])
    render(<Transcript state={state} loaded onSelectAgent={() => {}} />)
    fireEvent.click(screen.getByTestId("goal-session"))
    expect(screen.getAllByText("继续追求目标。")).toHaveLength(1)
    expect(notices(state)).toHaveLength(2)
  })
})
