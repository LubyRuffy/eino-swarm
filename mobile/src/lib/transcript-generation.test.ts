import { describe, expect, it } from "vitest"
import { applyEvent, phoneAgents, type CompactBlock } from "./transcript"
import type { RemoteEvent } from "./rpc"

function event(kind: string, seq: number, extra: Partial<RemoteEvent> = {}): RemoteEvent {
  return { thread_id: "thread", turn_id: "turn", agent_id: "worker", kind, seq, text: "", created_at: "2026-10-22T00:00:00Z", ...extra }
}
const thought = event("reasoning_delta", 0, { message_id: "first", text: "partial thought" })
const interruption = event("generation_interrupted", 4, { message_id: "first", text: JSON.stringify({ reason: "engine_restart", detail: "handoff", recovery: "pending" }) })
const recovery = { ...interruption, kind: "generation_recovered", seq: 6, text: JSON.stringify({ reason: "engine_restart", detail: "handoff", recovery: "recovered" }) }
const fold = (events: RemoteEvent[]) => events.reduce<CompactBlock[]>(applyEvent, [])

describe("phone generation interruption", () => {
  it("closes old thinking, preserves one updated notice, and keeps the same worker running", () => {
    const blocks = fold([event("spawned", 1), thought, interruption, recovery, event("reasoning_delta", 0, { message_id: "second", text: "partial thought" })])
    expect(blocks.filter((b) => b.kind === "reasoning").map((b) => b.streaming)).toEqual([false, true])
    expect(blocks.filter((b) => b.generation)).toHaveLength(1)
    expect(blocks.find((b) => b.generation)?.generation).toMatchObject({ reason: "engine_restart", recovery: "recovered" })
    expect(phoneAgents(blocks)[0].status).toBe("running")
  })
  it.each([true, false])("cannot reopen old thinking after interruption (notice first: %s)", (first) => {
    const blocks = fold(first ? [interruption, thought] : [thought, interruption, { ...thought, text: "partial thought extended" }])
    expect(blocks.find((b) => b.kind === "reasoning")?.streaming).toBe(false)
  })
  it("keeps a recovered notice when an old interruption arrives late", () => {
    const blocks = fold([thought, recovery, interruption])
    expect(blocks.filter((b) => b.generation)).toHaveLength(1)
    expect(blocks.find((b) => b.generation)?.generation?.recovery).toBe("recovered")
  })
  it.each(["resumed", "restart_resumed", "spawned"])("explains legacy thinking without inventing a cause on %s", (kind) => {
    const blocks = fold([event("spawned", 1), { ...thought, message_id: undefined }, event(kind, 5, { agent_id: kind === "spawned" ? "worker" : "manager" })])
    expect(blocks.find((b) => b.kind === "reasoning")?.streaming).toBe(false)
    expect(blocks.find((b) => b.generation)?.generation).toMatchObject({ reason: "unknown", recovery: "recovered" })
  })
})

it.each(["resumed", "restart_resumed", "spawned"])("does not invent a cause for a completed legacy thought on %s", (kind) => {
  const blocks = fold([event("spawned", 1), event("reasoning", 2, { text: "completed thought" }), event("finished", 3), event(kind, 5, { agent_id: kind === "spawned" ? "worker" : "manager" })])
  expect(blocks.some((b) => b.generation)).toBe(false)
})

it("keeps generation receipts per worker and seals only composing arguments", () => {
  const blocks = fold([
    event("tool_call_delta", 0, { message_id: "first", tool_call_id: "writing", text: "read(8)" }),
    event("tool_call", 2, { message_id: "other", tool_call_id: "executing", text: "read({})" }), interruption,
    { ...interruption, agent_id: "other-worker" },
  ])
  expect(blocks.find((b) => b.callId === "writing")?.pending).toBe(false)
  expect(blocks.find((b) => b.callId === "executing")?.pending).toBe(true)
  expect(blocks.filter((b) => b.generation).map((b) => b.agentId)).toEqual(["worker", "other-worker"])
})

it("does not revive a finished worker when old interrupted output arrives late", () => {
  const blocks = fold([thought, interruption, event("finished", 7, { text: "completed" }), { ...thought, text: "partial thought extended" }])
  expect(phoneAgents(blocks)[0].status).toBe("done")
  expect(blocks.find((b) => b.kind === "reasoning")?.streaming).toBe(false)
})

it.each(["reasoning_delta", "delta"])("keeps replacement thinking active when a first old %s follows its notice", (kind) => {
  const blocks = fold([interruption, event("reasoning_delta", 0, { message_id: "second", text: "replacement thought" }), { ...thought, kind }])
  expect(blocks.find((b) => b.messageId === "second" && b.kind === "reasoning")?.streaming).toBe(true)
  expect(blocks.find((b) => b.messageId === "first" && !b.generation)?.streaming).toBe(false)
})

it("does not reopen interrupted tool arguments or close replacement thinking on a late preview", () => {
  const preview = event("tool_call_delta", 0, { message_id: "first", tool_call_id: "tool", text: "inspect(8)" })
  const blocks = fold([preview, interruption, event("reasoning_delta", 0, { message_id: "second", text: "replacement thought" }), { ...preview, text: "inspect(16)" }])
  expect(blocks.find((b) => b.callId === "tool")?.pending).toBe(false)
  expect(blocks.find((b) => b.messageId === "second" && b.kind === "reasoning")?.streaming).toBe(true)
})

it.each(["resumed", "restart_resumed", "spawned"])("seals a pre-lifecycle identified legacy stream on %s", (kind) => {
  const blocks = fold([event("spawned", 1), thought, event(kind, 5, { agent_id: kind === "spawned" ? "worker" : "manager" }), { ...thought, text: "partial thought extended" }])
  expect(blocks.find((b) => b.kind === "reasoning")?.streaming).toBe(false)
  expect(blocks.find((b) => b.generation)).toMatchObject({ messageId: "first", generation: { reason: "unknown", recovery: "recovered" } })
})

it("keeps a finished lifecycle authoritative when sealed old output is loaded after it", () => {
  const blocks = fold([event("finished", 7, { text: "completed" }), interruption, { ...thought, kind: "reasoning", seq: 3 }])
  expect(phoneAgents(blocks)[0].status).toBe("done")
})

it("updates an inferred legacy reason from a late receipt without losing observed recovery", () => {
  const blocks = fold([thought, event("resumed", 6, { agent_id: "manager" }), interruption])
  expect(blocks.filter((b) => b.generation)).toHaveLength(1)
  expect(blocks.find((b) => b.generation)?.generation).toMatchObject({ reason: "engine_restart", recovery: "recovered" })
})
