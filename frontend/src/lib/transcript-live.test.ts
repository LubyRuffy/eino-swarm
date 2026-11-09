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
    partial.kind === "tool_call_delta" ||
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
    generation: partial.generation,
    message_id: partial.message_id,
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
  it("keeps reused manager MCP call IDs separate across turns, including in one live batch", () => {
    const name = "mcp_local_check_012345abcdef"
    const first = fold([
      ev({ kind: "user_message", text: "First check" }),
      ev({ kind: "tool_call", text: `${name}({})`, tool_call_id: "reused" }),
      ev({ kind: "tool_result", text: "first result", tool_call_id: "reused" }),
      ev({ kind: "done" }),
    ])
    const next = fold(collapseLiveEvents([
      ev({ kind: "user_message", text: "Second check", turn_id: "tn_2" }),
      ev({ kind: "tool_call_delta", text: `${name}(2)`, tool_call_id: "reused", turn_id: "tn_2" }),
      ev({ kind: "tool_call", text: `${name}({})`, tool_call_id: "reused", turn_id: "tn_2" }),
      ev({ kind: "tool_delta", text: "second progress", tool_call_id: "reused", turn_id: "tn_2" }),
      ev({ kind: "tool_delta", text: "late first progress", tool_call_id: "reused" }),
      ev({ kind: "tool_call_delta", text: `${name}(2)`, tool_call_id: "reused", turn_id: "tn_2" }),
    ]), first)
    const calls = manager(next).blocks.filter((b) => b.kind === "tool")
    expect(calls).toMatchObject([
      { turnId: "tn_1", tool: { callId: "reused", pending: false, result: "first result" } },
      { turnId: "tn_2", tool: { callId: "reused", pending: true, progress: "second progress" } },
    ])
    expect(calls[1].tool?.writing).toBeUndefined()
    const settled = fold([ev({ kind: "tool_result", text: "second result", tool_call_id: "reused", turn_id: "tn_2" })], next)
    expect(manager(settled).blocks.filter((b) => b.kind === "tool").map((b) => b.tool?.result)).toEqual([
      "first result", "second result",
    ])
  })

  it("settles a parked worker's MCP call under the next manager turn only for its original generation", () => {
    const actor = { agent_id: "worker-1", generation: 2 }
    const call = { ...actor, tool_call_id: "same-call" }
    const parked = fold([
      ev({ kind: "user_message", text: "Start the worker" }),
      ev({ kind: "spawned", ...actor }),
      ev({ kind: "tool_call", ...call, text: "mcp_local_check_012345abcdef({})" }),
      ev({ kind: "done" }),
      ev({ kind: "goal_continued", turn_id: "tn_2" }),
    ])
    const stale = fold([
      ev({ kind: "tool_delta", ...call, generation: 1, turn_id: "tn_2", text: "different generation" }),
      ev({ kind: "tool_result", ...call, generation: 1, turn_id: "tn_2", text: "different result" }),
    ], parked)
    const tools = (state: TranscriptState) => state.agents[actor.agent_id].blocks.filter((b) => b.kind === "tool")
    expect(tools(stale)).toMatchObject([{ turnId: "tn_1", tool: { pending: true } }])
    expect(tools(stale)[0].tool?.progress).toBeUndefined()

    const live = fold([ev({ kind: "tool_delta", ...call, turn_id: "tn_2", text: "still working" })], stale)
    expect(tools(live)).toMatchObject([{ turnId: "tn_1", tool: { pending: true, progress: "still working" } }])
    const settled = fold([
      ev({ kind: "tool_result", ...call, turn_id: "tn_2", text: "final result" }),
      ev({ kind: "tool_delta", ...call, turn_id: "tn_2", text: "late snapshot" }),
    ], live)
    expect(tools(settled)).toMatchObject([{ turnId: "tn_1", tool: { pending: false, result: "final result" } }])
    expect(tools(settled)[0].tool?.progress).toBeUndefined()
  })

  it("keeps a worker's MCP call draft when its durable invocation arrives under a new manager turn", () => {
    const actor = { agent_id: "worker-1", generation: 1 }
    const call = { ...actor, tool_call_id: "c1" }
    const name = "mcp_local_check_012345abcdef"
    const state = fold([
      ev({ kind: "user_message", text: "Start the worker" }),
      ev({ kind: "spawned", ...actor }),
      ev({ kind: "tool_call_delta", ...call, message_id: "m1", text: `${name}(2)` }),
      ev({ kind: "done" }),
      ev({ kind: "goal_continued", turn_id: "tn_2" }),
      ev({ kind: "tool_call", ...call, turn_id: "tn_2", message_id: "m1", text: `${name}({})` }),
      ev({ kind: "tool_result", ...call, turn_id: "tn_2", text: "finished" }),
    ])
    const tools = state.agents[call.agent_id].blocks.filter((b) => b.kind === "tool")
    expect(tools).toMatchObject([{ turnId: "tn_1", tool: { pending: false, args: "{}", result: "finished" } }])
    expect(tools[0].tool?.writing).toBeUndefined()
  })

  it("settles one MCP call without replacing another's progress or replaying a late snapshot", () => {
    const tool = "mcp_local_check_012345abcdef"
    const done = '{"content":[{"type":"text","text":"finished"}]}'
    const state = fold(collapseLiveEvents([
      ev({ kind: "user_message", text: "Check both sources" }),
      ev({ kind: "tool_call", text: `${tool}({})`, tool_call_id: "c1" }),
      ev({ kind: "tool_call", text: `${tool}({})`, tool_call_id: "c2" }),
      ev({ kind: "tool_delta", text: "Reading first source", tool_call_id: "c1" }),
      ev({ kind: "tool_delta", text: "Reading second source", tool_call_id: "c2" }),
      ev({ kind: "tool_result", text: done, tool_call_id: "c1" }),
      ev({ kind: "tool_delta", text: "Late first-source snapshot", tool_call_id: "c1" }),
      ev({ kind: "tool_delta", text: "Checking second source", tool_call_id: "c2" }),
    ]))
    const calls = manager(state).blocks.filter((b) => b.kind === "tool").map((b) => b.tool)
    expect(calls).toMatchObject([
      { callId: "c1", pending: false, result: done },
      { callId: "c2", pending: true, progress: "Checking second source" },
    ])
    expect(calls[0]?.progress).toBeUndefined()
    expect(calls[1]?.result).toBeUndefined()
  })

  it("does not promote MCP progress to the result when the owning turn stops", () => {
    const state = fold([
      ev({ kind: "user_message", text: "Check source" }),
      ev({ kind: "tool_call", text: "mcp_local_check_012345abcdef({})", tool_call_id: "c1" }),
      ev({ kind: "tool_delta", text: '{"error":{"code":"session_expired"}}', tool_call_id: "c1" }),
      ev({ kind: "error", err: "interrupted" }),
    ])
    const [call] = manager(state).blocks.filter((b) => b.kind === "tool")
    expect(call.tool).toMatchObject({ pending: false, failed: true, result: "interrupted" })
    expect(call.tool?.progress).toBeUndefined()
  })

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

  it("paints one row while a tool call is still being written", () => {
    const mid = fold([
      ev({ kind: "user_message", text: "hi" }),
      ev({ kind: "tool_call_delta", text: "spawn_agent(8)", tool_call_id: "c1" }),
      ev({ kind: "tool_call_delta", text: "spawn_agent(40)", tool_call_id: "c1" }),
    ])
    const live = manager(mid).blocks.filter((b) => b.kind === "tool")
    expect(live).toHaveLength(1)
    expect(live[0].tool).toMatchObject({ name: "spawn_agent", pending: true, writing: 40 })
    const done = fold(
      [ev({ kind: "tool_call", text: 'spawn_agent({"role":"writer"})', tool_call_id: "c1" })],
      mid,
    )
    const tools = manager(done).blocks.filter((b) => b.kind === "tool")
    expect(tools).toHaveLength(1)
    expect(tools[0].tool?.writing).toBeUndefined()
    expect(tools[0].tool?.args).toContain("writer")
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
