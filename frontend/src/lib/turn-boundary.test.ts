import { describe, expect, it } from "vitest"
import { turnSlices } from "./turn-boundary"
import type { Block, TurnState } from "./transcript"

function row(seq: number, turnId = "first", kind: Block["kind"] = "answer", at = "2026-01-01T00:00:01Z"): Block {
  return { id: `block-${seq}`, seq, turnId, kind, at, agentId: "manager", text: `content ${seq}` }
}

function turn(patch: Partial<TurnState> = {}): TurnState {
  return { id: "first", status: "done", userText: "", agentIds: [], completedSeq: 5,
    endedAt: "2026-01-01T00:00:05Z", ...patch }
}

describe("turnSlices", () => {
  it("keeps event order and assigns one footer to the last pre-terminal slice", () => {
    const blocks = [row(1), row(2, "next"), row(3), row(6, "first", "notice"), row(7, "next")]
    const slices = turnSlices(blocks, [turn(), turn({ id: "next", completedSeq: 8 })])
    expect(slices.map((s) => [s.turnId, s.showFooter, s.afterCompletion])).toEqual([
      ["first", false, false], ["next", false, false], ["first", true, false],
      ["first", false, true], ["next", true, false],
    ])
    expect(slices.flatMap((s) => s.blocks)).toEqual(blocks)
    expect(blocks.map((b) => b.id)).toEqual(["block-1", "block-2", "block-3", "block-6", "block-7"])
  })

  it("uses persisted terminal sequence even when timestamps disagree", () => {
    const slices = turnSlices([row(3, "first", "answer", "2027-01-01Z"), row(6)], [turn()])
    expect(slices.map((s) => [s.afterCompletion, s.showFooter])).toEqual([[false, true], [true, false]])
  })

  it("falls back to instant comparisons for legacy turns without a terminal sequence", () => {
    const slices = turnSlices([
      row(1, "first", "answer", "2026-01-01T08:00:05+08:00"),
      row(2, "first", "notice", "2026-01-01T00:00:06Z"),
    ], [turn({ completedSeq: undefined })])
    expect(slices.map((s) => [s.afterCompletion, s.showFooter])).toEqual([[false, true], [true, false]])
  })

  it("does not create a slice for omitted metadata or quiet rows", () => {
    expect(turnSlices([
      row(6, "first", "session_memory"), row(7, "first", "title"),
      { ...row(8, "first", "notice"), quiet: true },
      { ...row(9, "first", "tool"), tool: { name: "close_agent", pending: false, callId: "close", args: "" } },
    ], [turn()])).toEqual([])
    const [slice] = turnSlices([row(1), row(2, "first", "title"), row(3)], [turn()])
    expect(slice.blocks.map((b) => b.seq)).toEqual([1, 3])
    expect(slice.showFooter).toBe(true)
  })

  it.each([
    turn({ status: "running" }), turn({ endedAt: undefined }), turn({ quiet: true }),
  ])("does not invent completion for a running, unfinished or quiet turn", (state) => {
    expect(turnSlices([row(1)], [state])[0].showFooter).toBe(false)
  })

  it("keeps unhydrated history visible without inventing a completion", () => {
    expect(turnSlices([row(1)], [])).toEqual([
      { turnId: "first", blocks: [row(1)], showFooter: false, afterCompletion: false },
    ])
  })

  it.each(["error", "cancelled"] as const)("retains a %s turn boundary and its terminal notice", (status) => {
    const [slice] = turnSlices([row(5, "first", status === "error" ? "error" : "notice")], [turn({ status })])
    expect(slice.showFooter).toBe(true)
    expect(slice.afterCompletion).toBe(false)
  })
})
