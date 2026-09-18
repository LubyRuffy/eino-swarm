import { describe, expect, it } from "vitest"

import {
  MANAGER_ID,
  collapseLiveEvents,
  emptyTranscript,
  reduceEvents,
  type TranscriptState,
} from "./transcript"
import type { SwarmEvent } from "./types"

let seq = 0
function ev(partial: Partial<SwarmEvent> & { kind: string }): SwarmEvent {
  const live =
    partial.kind === "delta" ||
    partial.kind === "reasoning_delta" ||
    partial.kind === "tool_delta" ||
    partial.kind === "progress" ||
    partial.kind === "usage" ||
    partial.kind === "rewound"
  return {
    thread_id: "th_1",
    turn_id: partial.turn_id ?? "tn_1",
    seq: partial.seq ?? (live ? 0 : ++seq),
    kind: partial.kind,
    agent_id: partial.agent_id ?? MANAGER_ID,
    role: partial.role,
    text: partial.text,
    tool_call_id: partial.tool_call_id,
    err: partial.err,
    images: partial.images,
    created_at: partial.created_at ?? new Date(1700000000000 + seq * 1000).toISOString(),
  }
}

function fold(events: SwarmEvent[], from?: TranscriptState) {
  return reduceEvents(from ?? emptyTranscript(), events)
}

function manager(state: TranscriptState) {
  return state.agents[MANAGER_ID]
}

describe("live tool output", () => {
  it("fills a pending call from a live delta without closing it", () => {
    const live = JSON.stringify({ stdout: "chunk-one", stderr: "" })
    const state = fold([
      ev({ kind: "user_message", text: "hi" }),
      ev({ kind: "tool_call", text: "exec({})", tool_call_id: "c1" }),
      ev({ kind: "tool_delta", text: live, tool_call_id: "c1" }),
    ])
    const [tool] = manager(state).blocks.filter((b) => b.kind === "tool")
    expect(tool.tool?.pending).toBe(true)
    expect(tool.tool?.result).toBe(live)
    expect(manager(state).blocks.filter((b) => b.kind === "notice")).toHaveLength(0)
  })

  it("keeps two live calls from overwriting each other", () => {
    const state = fold([
      ev({ kind: "user_message", text: "hi" }),
      ev({ kind: "tool_call", text: "exec({})", tool_call_id: "c1" }),
      ev({ kind: "tool_call", text: "exec({})", tool_call_id: "c2" }),
      ev({
        kind: "tool_delta",
        text: JSON.stringify({ stdout: "alpha", stderr: "" }),
        tool_call_id: "c1",
      }),
      ev({
        kind: "tool_delta",
        text: JSON.stringify({ stdout: "beta", stderr: "" }),
        tool_call_id: "c2",
      }),
    ])
    const tools = manager(state).blocks.filter((b) => b.kind === "tool")
    expect(tools[0].tool?.pending).toBe(true)
    expect(tools[0].tool?.result).toContain("alpha")
    expect(tools[1].tool?.pending).toBe(true)
    expect(tools[1].tool?.result).toContain("beta")
  })
})

describe("collapseLiveEvents tool deltas", () => {
  it("keeps the latest snapshot per live tool call", () => {
    const collapsed = collapseLiveEvents([
      ev({ kind: "tool_delta", text: "a", tool_call_id: "c1" }),
      ev({ kind: "tool_delta", text: "x", tool_call_id: "c2" }),
      ev({ kind: "tool_delta", text: "ab", tool_call_id: "c1" }),
    ])
    expect(collapsed.map((e) => `${e.tool_call_id}:${e.text}`)).toEqual([
      "c2:x",
      "c1:ab",
    ])
  })
})
