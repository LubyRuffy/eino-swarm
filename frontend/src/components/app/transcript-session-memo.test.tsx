import { render, screen } from "@testing-library/react"
import { describe, expect, it, vi } from "vitest"

import { emptyTranscript, reduceEvents } from "@/lib/transcript"
import type { SwarmEvent } from "@/lib/types"
import { Transcript } from "./transcript"

const markdownRenders = vi.hoisted(() => vi.fn())

vi.mock("react-markdown", () => ({
  default: ({ children }: { children: string }) => {
    markdownRenders(children)
    return <p>{children}</p>
  },
}))

function event(kind: SwarmEvent["kind"], seq: number, turnId: string, text = ""): SwarmEvent {
  return {
    kind, seq, text, turn_id: turnId, thread_id: "thread", agent_id: "manager",
    created_at: new Date(Date.UTC(2026, 0, 1, 0, 0, seq)).toISOString(),
  }
}

describe("visible historical session answers", () => {
  it("does not parse a finished answer again as the next session streams tokens", () => {
    // Keeping results visible must not restore the per-token Markdown work
    // that originally led to hiding entire completed goal sessions.
    markdownRenders.mockClear()
    let state = reduceEvents(emptyTranscript(), [
      event("goal_continued", 1, "earlier"),
      event("agent_message", 2, "earlier", "earlier result"),
      event("done", 3, "earlier", "earlier result"),
      event("goal_continued", 4, "current"),
    ])
    const view = render(<Transcript state={state} loaded onSelectAgent={() => {}} />)
    expect(screen.getByText("earlier result")).toBeVisible()
    for (let token = 1; token <= 20; token++) {
      state = reduceEvents(state, [event("delta", 0, "current", `current progress ${token}`)])
      view.rerender(<Transcript state={state} loaded onSelectAgent={() => {}} />)
    }
    expect(screen.getByText("current progress 20")).toBeVisible()
    expect(screen.getByText("earlier result")).toBeVisible()
    expect(markdownRenders.mock.calls.filter(([text]) => text === "earlier result")).toHaveLength(1)
    expect(markdownRenders.mock.calls.filter(([text]) => text === "current progress 20")).toHaveLength(1)
  })
})
