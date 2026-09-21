import { describe, expect, it } from "vitest"

import { applyGoalDetail, formatGoalAge, goalState, parseGoalReason } from "./goal"

describe("goalState", () => {
  it("is pursuing until a hold, block, or complete lands", () => {
    expect(goalState({})).toBeUndefined()
    expect(goalState({ goal: "  " })).toBeUndefined()
    expect(goalState({ goal: "keep going" })).toBe("pursuing")
    expect(goalState({ goal: "keep going", goal_complete: true })).toBe("done")
    expect(goalState({ goal: "keep going", goal_blocked: true })).toBe("blocked")
    expect(goalState({ goal: "keep going", goal_capped: true })).toBe("paused")
    expect(goalState({ goal: "keep going", goal_idle: true })).toBe("paused")
  })
})

describe("formatGoalAge", () => {
  it("prints the elapsed clock the banner shows", () => {
    const from = new Date("2026-09-21T00:00:00Z")
    expect(formatGoalAge(from, new Date("2026-09-21T00:00:05Z"))).toBe("5s")
    expect(formatGoalAge(from, new Date("2026-09-21T01:02:03Z"))).toBe("1h 2m 3s")
    expect(formatGoalAge(from, new Date("2026-09-22T10:00:00Z"))).toBe("1d 10h 0m 0s")
  })
})

describe("parseGoalReason", () => {
  it("reads a packed reason and leaves plain text alone", () => {
    expect(parseGoalReason('{"reason":"needs an external change"}')).toBe(
      "needs an external change",
    )
    expect(parseGoalReason("plain")).toBe("plain")
    expect(parseGoalReason("")).toBe("")
  })
})

describe("applyGoalDetail", () => {
  it("keeps the banner on after complete so Done is not a disappearing strip", () => {
    let d = applyGoalDetail(
      { id: "t1", title: "one" },
      { kind: "goal", text: "keep going", created_at: "2026-09-21T00:00:00Z" },
    )
    expect(d.goal_on).toBe(true)
    expect(d.goal).toBe("keep going")
    expect(d.goal_started_at).toBe("2026-09-21T00:00:00Z")
    d = applyGoalDetail(d, { kind: "goal_complete", text: "" })
    expect(d.goal_on).toBe(true)
    expect(d.goal_complete).toBe(true)
    d = applyGoalDetail(d, { kind: "goal_resumed", text: "" })
    expect(d.goal_complete).toBe(false)
    expect(d.goal_blocked).toBe(false)
    d = applyGoalDetail(d, { kind: "goal_blocked", text: '{"reason":"stuck"}' })
    expect(d.goal_blocked).toBe(true)
    expect(d.goal_block_reason).toBe("stuck")
    d = applyGoalDetail(d, { kind: "goal_capped", text: "" })
    expect(d.goal_capped).toBe(true)
    d = applyGoalDetail(d, { kind: "goal_idle", text: "" })
    expect(d.goal_idle).toBe(true)
    d = applyGoalDetail(d, { kind: "goal_edited", text: "tighter" })
    expect(d.goal).toBe("tighter")
    d = applyGoalDetail(d, { kind: "goal_continued", text: "" })
    expect(d.goal_idle).toBe(false)
  })
})
