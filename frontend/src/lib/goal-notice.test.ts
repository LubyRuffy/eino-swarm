import { describe, expect, it } from "vitest"

import { visibleGoalNotices } from "./goal-notice"
import type { Block } from "./transcript"

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
