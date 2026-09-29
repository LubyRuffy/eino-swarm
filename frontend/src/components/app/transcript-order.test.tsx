import { render, screen } from "@testing-library/react"
import { describe, expect, it } from "vitest"

import type { Block, TranscriptState } from "@/lib/transcript"
import { emptyTranscript } from "@/lib/transcript"

import { Transcript } from "./transcript"

// A memory notice or a session row can carry an earlier turn id after the
// next turn has already started. The log stays chronological, but the same
// turn id is no longer one contiguous group. Keying the row on that id makes
// React reuse the first slice's DOM for the later slice, so the top of a
// fully loaded thread is a thought with the user's message further down.

function row(partial: Pick<Block, "id" | "kind" | "text" | "turnId" | "seq">): Block {
  return {
    agentId: "manager",
    at: "2026-09-28T00:00:00.000Z",
    ...partial,
  }
}

function state(blocks: Block[]): TranscriptState {
  return {
    ...emptyTranscript(),
    agentOrder: ["manager"],
    agents: {
      manager: {
        id: "manager",
        role: "manager",
        status: "done",
        activity: "",
        blocks,
      },
    },
    turns: [
      { id: "tn_a", userText: "opening request", status: "done", agentIds: [] },
      { id: "tn_b", userText: "follow up", status: "done", agentIds: [] },
      { id: "tn_c", userText: "latest ask", status: "done", agentIds: [] },
    ],
    lastSeq: blocks.at(-1)?.seq ?? 0,
  }
}

const full = [
  row({ id: "u1", kind: "user", text: "opening request", turnId: "tn_a", seq: 1 }),
  row({ id: "a1", kind: "answer", text: "first reply", turnId: "tn_a", seq: 2 }),
  row({ id: "u2", kind: "user", text: "follow up", turnId: "tn_b", seq: 3 }),
  row({ id: "a2", kind: "answer", text: "second reply", turnId: "tn_b", seq: 4 }),
  row({ id: "n1", kind: "notice", text: "kept a note", turnId: "tn_a", seq: 5 }),
  row({ id: "a3", kind: "answer", text: "continued reply", turnId: "tn_b", seq: 6 }),
  row({ id: "u3", kind: "user", text: "latest ask", turnId: "tn_c", seq: 7 }),
  row({ id: "a4", kind: "answer", text: "latest reply", turnId: "tn_c", seq: 8 }),
]

describe("transcript order when a turn id comes back", () => {
  it("keeps the first user message at the top after older history arrives", () => {
    // Each rerender is an older page landing in front. A shared turn id
    // used to leave the previous slice's DOM at the top, so the first
    // send sat further down and a later one was painted twice.
    const view = render(<Transcript state={state(full.slice(6))} loaded onSelectAgent={() => {}} />)
    view.rerender(<Transcript state={state(full.slice(4))} loaded onSelectAgent={() => {}} />)
    view.rerender(<Transcript state={state(full.slice(2))} loaded onSelectAgent={() => {}} />)
    view.rerender(<Transcript state={state(full)} loaded onSelectAgent={() => {}} />)

    expect(screen.getAllByTestId("user-message").map((node) => node.textContent)).toEqual([
      "opening request",
      "follow up",
      "latest ask",
    ])
    const col = document.querySelector(".content-column")
    const tops = [...(col?.children ?? [])].map((el) => el.textContent ?? "")
    expect(tops[0] ?? "").toContain("opening request")
    expect(tops.filter((text) => text.includes("follow up"))).toHaveLength(1)
    expect(tops.filter((text) => text.includes("continued reply"))).toHaveLength(1)
  })
})
