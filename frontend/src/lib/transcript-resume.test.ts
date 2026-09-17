import { describe, expect, it } from "vitest"

import {
  MANAGER_ID,
  emptyTranscript,
  reduceEvents,
  type TranscriptState,
} from "./transcript"
import type { SwarmEvent } from "./types"

let seq = 0
function ev(partial: Partial<SwarmEvent> & { kind: string }): SwarmEvent {
  const stored = partial.kind !== "delta" && partial.kind !== "reasoning_delta"
  return {
    thread_id: "th_1",
    turn_id: partial.turn_id ?? "tn_1",
    seq: partial.seq ?? (stored ? ++seq : 0),
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

describe("resume closes dead in-flight tools", () => {
  // Kill/quit does not finish a tool call. The process is gone, so the row
  // must not keep spinning while the turn continues.
  it("closes tools left pending when a crashed turn is resumed", () => {
    const state = fold([
      ev({ kind: "user_message", text: "hi" }),
      ev({ kind: "tool_call", text: "exec({})", tool_call_id: "c1" }),
      ev({ kind: "tool_call", text: "read({})", tool_call_id: "c2" }),
      ev({ kind: "tool_result", text: "ok", tool_call_id: "c2" }),
      ev({ kind: "resumed", text: "the previous run was interrupted; continuing" }),
    ])
    const tools = manager(state).blocks.filter((b) => b.kind === "tool")
    expect(tools).toHaveLength(2)
    expect(tools[0].tool).toMatchObject({
      pending: false,
      failed: true,
      result: "the previous process stopped",
    })
    expect(tools[1].tool).toMatchObject({ pending: false, failed: false, result: "ok" })
    expect(state.running).toBe(true)
    expect(JSON.stringify(state)).not.toMatch(/notes\.md|summarize|look into this/)
  })

  it("closes a leftover worker's in-flight tool without treating the worker as done", () => {
    const state = fold([
      ev({ kind: "user_message", text: "hi" }),
      ev({
        kind: "spawned",
        agent_id: "worker-1",
        role: "worker",
        text: "do the assigned work",
      }),
      ev({ kind: "tool_call", agent_id: "worker-1", text: "exec({})", tool_call_id: "c1" }),
      ev({ kind: "resumed", text: "the previous run was interrupted; continuing" }),
    ])
    expect(state.agents["worker-1"].status).toBe("running")
    const [tool] = state.agents["worker-1"].blocks.filter((b) => b.kind === "tool")
    expect(tool.tool).toMatchObject({
      pending: false,
      failed: true,
      result: "the previous process stopped",
    })
    expect(state.running).toBe(true)
  })
})
