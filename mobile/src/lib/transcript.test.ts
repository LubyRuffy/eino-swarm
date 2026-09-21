import { describe, expect, it } from "vitest"

import { applyEvent, pendingAsk, type CompactBlock } from "./transcript"
import type { RemoteEvent } from "./rpc"

function ev(partial: Partial<RemoteEvent> & Pick<RemoteEvent, "kind" | "seq">): RemoteEvent {
  return {
    thread_id: "t",
    text: "",
    created_at: "2026-01-01T00:00:00Z",
    ...partial,
  }
}

describe("compact transcript", () => {
  it("splits user and assistant and streams deltas into one answer", () => {
    let blocks: CompactBlock[] = []
    blocks = applyEvent(blocks, ev({ seq: 1, kind: "user_message", text: "hello" }))
    blocks = applyEvent(blocks, ev({ seq: 2, kind: "delta", text: "he" }))
    blocks = applyEvent(blocks, ev({ seq: 3, kind: "delta", text: "hello" }))
    blocks = applyEvent(blocks, ev({ seq: 4, kind: "agent_message", text: "hello" }))
    expect(blocks.map((b) => b.kind)).toEqual(["user", "answer"])
    expect(blocks[1].text).toBe("hello")
    expect(blocks[1].streaming).toBe(false)
  })

  it("folds ask_user into a pending question then settles on result", () => {
    let blocks: CompactBlock[] = []
    const args =
      '{"questions":[{"id":"q1","prompt":"Which?","options":[{"id":"a","label":"A"},{"id":"b","label":"B"}]}]}'
    blocks = applyEvent(
      blocks,
      ev({
        seq: 5,
        kind: "tool_call",
        tool_call_id: "c1",
        text: `ask_user(${args})`,
      }),
    )
    const card = pendingAsk(blocks)
    expect(card?.pending).toBe(true)
    expect(card?.questions).toHaveLength(1)
    expect(card?.questions?.[0].options.some((o) => o.id === "other")).toBe(true)
    blocks = applyEvent(
      blocks,
      ev({ seq: 6, kind: "tool_result", tool_call_id: "c1", text: "{}" }),
    )
    expect(pendingAsk(blocks)).toBeUndefined()
  })

  it("keeps tool rows collapsed and spawned without a body", () => {
    let blocks: CompactBlock[] = []
    blocks = applyEvent(
      blocks,
      ev({ seq: 7, kind: "tool_call", tool_call_id: "w", text: "read({})" }),
    )
    blocks = applyEvent(
      blocks,
      ev({ seq: 8, kind: "spawned", role: "researcher", text: "" }),
    )
    blocks = applyEvent(blocks, ev({ seq: 9, kind: "error", err: "boom" }))
    expect(blocks[0].kind).toBe("tool")
    expect(blocks[0].pending).toBe(true)
    expect(blocks[1].kind).toBe("spawn")
    expect(blocks[1].text).toBe("researcher")
    expect(blocks[2].kind).toBe("error")
  })

  it("does not leak unknown kinds into the transcript", () => {
    const blocks = applyEvent([], ev({ seq: 10, kind: "not_a_kind", text: "ghost" }))
    expect(blocks).toEqual([])
  })

  it("folds a live tool_delta into the pending tool row", () => {
    let blocks: CompactBlock[] = []
    blocks = applyEvent(
      blocks,
      ev({ seq: 11, kind: "tool_call", tool_call_id: "c", text: "read({})" }),
    )
    blocks = applyEvent(
      blocks,
      ev({ seq: 12, kind: "tool_delta", tool_call_id: "c", text: "line" }),
    )
    expect(blocks).toHaveLength(1)
    expect(blocks[0].text).toBe("line")
  })

  it("keeps goal and plan notices without dumping empty kinds", () => {
    let blocks: CompactBlock[] = []
    blocks = applyEvent(blocks, ev({ seq: 13, kind: "goal", text: "keep going" }))
    blocks = applyEvent(blocks, ev({ seq: 14, kind: "plan", text: "" }))
    expect(blocks.map((b) => b.kind)).toEqual(["notice"])
  })

  it("folds schedule kinds to one-liners instead of the payload JSON", () => {
    const payload = JSON.stringify({
      id: "sch_x",
      kind: "thread",
      title: "periodic",
      prompt: "do the long check then cat status.json",
    })
    let blocks: CompactBlock[] = []
    blocks = applyEvent(blocks, ev({ seq: 20, kind: "schedule", text: payload }))
    blocks = applyEvent(
      blocks,
      ev({ seq: 21, kind: "user_message", text: "This turn is a scheduled check.\n\nkeep going" }),
    )
    blocks = applyEvent(blocks, ev({ seq: 22, kind: "schedule_fired", text: payload }))
    blocks = applyEvent(blocks, ev({ seq: 23, kind: "schedule_report", text: payload }))
    blocks = applyEvent(blocks, ev({ seq: 24, kind: "schedule_skipped", text: payload }))
    blocks = applyEvent(blocks, ev({ seq: 25, kind: "schedule_cancelled", text: "sch_x" }))
    expect(blocks.map((b) => b.kind)).toEqual(["notice", "notice", "notice"])
    expect(blocks.map((b) => b.text)).toEqual([
      "A wait is armed.",
      "Scheduled check.",
      "A wait was cancelled.",
    ])
    expect(blocks.some((b) => b.text.includes("status.json"))).toBe(false)
    expect(blocks.some((b) => b.text.includes("sch_x"))).toBe(false)
  })

  it("keeps report tool args after a quiet envelope result so the chip can preview findings", () => {
    const args = JSON.stringify({ findings: "one thing changed", quiet: false })
    let blocks: CompactBlock[] = []
    blocks = applyEvent(
      blocks,
      ev({
        seq: 30,
        kind: "tool_call",
        tool_call_id: "c",
        text: `report_schedule(${args})`,
      }),
    )
    blocks = applyEvent(
      blocks,
      ev({
        seq: 31,
        kind: "tool_result",
        tool_call_id: "c",
        text: JSON.stringify({ ok: true, quiet: false }),
      }),
    )
    expect(blocks).toHaveLength(1)
    expect(blocks[0].kind).toBe("tool")
    expect(blocks[0].toolName).toBe("report_schedule")
    expect(blocks[0].args).toBe(args)
    expect(blocks[0].text).toBe(JSON.stringify({ ok: true, quiet: false }))
    expect(blocks[0].pending).toBe(false)
  })

  it("does not paint a progress pulse as a notice", () => {
    const pulse = JSON.stringify({
      elapsed_ms: 0,
      agents: [{ agent_id: "w1", role: "worker", status: "done", elapsed_ms: 0 }],
    })
    const blocks = applyEvent([], ev({ seq: 40, kind: "progress", text: pulse }))
    expect(blocks).toEqual([])
  })

  it("skips packed cap and compact envelopes instead of dumping them", () => {
    let blocks: CompactBlock[] = []
    blocks = applyEvent(
      blocks,
      ev({ seq: 41, kind: "max_iterations", text: JSON.stringify({ limit: 8, extend_by: 8 }) }),
    )
    blocks = applyEvent(
      blocks,
      ev({ seq: 42, kind: "compacted", text: JSON.stringify({ through_seq: 9 }) }),
    )
    blocks = applyEvent(blocks, ev({ seq: 43, kind: "goal", text: "keep going" }))
    expect(blocks.map((b) => b.kind)).toEqual(["notice"])
    expect(blocks[0].text).toBe("keep going")
    expect(blocks.some((b) => b.text.includes("elapsed_ms"))).toBe(false)
    expect(blocks.some((b) => b.text.includes("through_seq"))).toBe(false)
  })
})
