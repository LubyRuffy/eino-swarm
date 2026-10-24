import { describe, expect, it } from "vitest"

import { visibleGoalNotices } from "./goal-notice"
import { emptyTranscript, reduceEvents, type Block } from "./transcript"
import type { SwarmEvent } from "./types"

function block(seq: number, extra: Partial<Block> = {}): Block {
  return {
    id: String(seq), seq, agentId: "manager", kind: "notice", text: "same notice",
    turnId: "previous", at: "2026-01-01T00:00:00.000Z", ...extra,
  }
}

const resumed = () => block(1, { role: "human", goalNoticeKind: "goal_resumed" })
const continued = () => block(3, { turnId: "next", goalNoticeKind: "goal_continued" })

describe("visibleGoalNotices", () => {
  it("merges only the chat projection and retains the new turn's continuation", () => {
    const blocks = [resumed(), continued()]
    const before = structuredClone(blocks)
    expect(visibleGoalNotices(blocks)).toEqual([blocks[1]])
    expect(blocks).toEqual(before)
  })

  it("ignores wording and elapsed time when pairing event identities", () => {
    const start = { ...resumed(), text: "a different locale", at: "2026-01-01T00:00:00.000Z" }
    const next = { ...continued(), text: "another translation", at: "2026-01-02T00:00:00.000Z" }
    expect(visibleGoalNotices([start, next])).toEqual([next])
  })

  it("passes quiet metadata through without separating the two visible notices", () => {
    const metadata = block(2, { kind: "title", quiet: true, text: "metadata" })
    const next = continued()
    expect(visibleGoalNotices([resumed(), metadata, next])).toEqual([metadata, next])
  })

  for (const outcome of [{ changed: true, notes: { add: 1 }, notify: "on" },
                         { changed: false, err: "review failed", notify: "on" }]) {
    it(`keeps a late ${outcome.err ? "failed" : "changed"} review visible without splitting one human resume`, () => {
      const events: SwarmEvent[] = [
        { kind: "goal_resumed", seq: 1, turn_id: "previous", role: "human" },
        { kind: "memory_review", seq: 2, turn_id: "older", agent_id: "memory-reviewer", text: JSON.stringify(outcome) },
        { kind: "goal_continued", seq: 3, turn_id: "next" },
      ].map((event) => ({ thread_id: "thread", agent_id: "manager", created_at: "2026-01-01T00:00:00.000Z", ...event }))
      const state = reduceEvents(emptyTranscript(), events)
      const rows = state.agents.manager.blocks
      const review = rows.find((row) => row.review)!
      expect(review.quiet).toBe(false)
      expect(visibleGoalNotices(rows).map((row) => row.seq)).toEqual([2, 3])
      expect(visibleGoalNotices(rows)[0]).toBe(review)
      expect(rows.map((row) => row.seq)).toEqual([1, 2, 3])
      expect(state.running).toBe(true)
    })
  }

  for (const kind of ["goal_edited", "error"] as const) {
    it(`still separates resumes around an actual ${kind} event`, () => {
      const events: SwarmEvent[] = [
        { kind: "goal_resumed", seq: 1, turn_id: "previous", role: "human" },
        { kind, seq: 2, turn_id: "previous", err: kind === "error" ? "turn failed" : undefined },
        { kind: "goal_continued", seq: 3, turn_id: "next" },
      ].map((event) => ({ thread_id: "thread", agent_id: "manager", created_at: "2026-01-01T00:00:00.000Z", ...event }))
      const rows = reduceEvents(emptyTranscript(), events).agents.manager.blocks
      expect(visibleGoalNotices(rows)).toBe(rows)
      expect(rows.filter((row) => row.goalNoticeKind)).toHaveLength(2)
    })
  }

  for (const kind of ["user", "steer", "error", "answer", "tool", "notice"] as const) {
    it(`does not merge across an intervening ${kind} row`, () => {
      const rows = [resumed(), block(2, { kind }), continued()]
      expect(visibleGoalNotices(rows)).toBe(rows)
    })
  }

  for (const role of [undefined, "manager"]) {
    it(`keeps a manager reopen with role ${String(role)}`, () => {
      const rows = [{ ...resumed(), role }, continued()]
      expect(visibleGoalNotices(rows)).toBe(rows)
    })
  }

  it("preserves a standalone resume, standalone continuation and empty transcript", () => {
    for (const rows of [[], [resumed()], [continued()]]) expect(visibleGoalNotices(rows)).toBe(rows)
  })

  it("keeps identical notice text that has no goal event metadata", () => {
    const rows = [block(1, { role: "human" }), block(2, { turnId: "next" })]
    expect(visibleGoalNotices(rows)).toBe(rows)
  })

  it("keeps same-turn, missing-turn and reversed-sequence events separate", () => {
    for (const next of [
      { ...continued(), turnId: "previous" },
      { ...continued(), turnId: "" },
      { ...continued(), seq: 1 },
    ]) {
      const rows = [resumed(), next]
      expect(visibleGoalNotices(rows)).toBe(rows)
    }
    const unpersisted = [{ ...resumed(), seq: 0 }, continued()]
    expect(visibleGoalNotices(unpersisted)).toBe(unpersisted)
  })

  it("pairs each resume at most once and preserves later identical continuations", () => {
    const first = continued()
    const later = block(4, { turnId: "later", goalNoticeKind: "goal_continued" })
    const independent = block(5, { role: "human", goalNoticeKind: "goal_resumed" })
    expect(visibleGoalNotices([resumed(), first, later, independent])).toEqual([first, later, independent])
  })

  it("does not claim that two consecutive human resumes were one action", () => {
    const first = resumed()
    const second = block(2, { role: "human", goalNoticeKind: "goal_resumed" })
    const next = continued()
    expect(visibleGoalNotices([first, second, next])).toEqual([first, next])
  })
})
