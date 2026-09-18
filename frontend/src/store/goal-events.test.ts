import { describe, expect, it } from "vitest"

import { applyGoalThreadFlags, parseGoalReason } from "./goal-events"
import type { SwarmEvent, Thread } from "@/lib/types"

const thread = (over: Partial<Thread> = {}): Thread => ({
  id: "th_1",
  title: "t",
  project_id: "",
  provider_id: "default",
  reasoning_effort: "",
  archived: false,
  created_at: "",
  last_active_at: "",
  running: false,
  ...over,
})

const ev = (kind: SwarmEvent["kind"], text?: string): SwarmEvent => ({
  kind,
  seq: 1,
  thread_id: "th_1",
  turn_id: "tn_1",
  agent_id: "manager",
  text,
  created_at: "",
})

describe("applyGoalThreadFlags", () => {
  it("holds auto-continue after a no-progress continuation", () => {
    const next = applyGoalThreadFlags([thread()], "th_1", ev("goal_idle"))
    expect(next[0]?.goal_idle).toBe(true)
  })

  it("clears the hold when pursuit continues", () => {
    const next = applyGoalThreadFlags(
      [thread({ goal_idle: true })],
      "th_1",
      ev("goal_continued"),
    )
    expect(next[0]?.goal_idle).toBe(false)
  })

  it("leaves unrelated events alone", () => {
    const rows = [thread({ goal: "keep going" })]
    expect(applyGoalThreadFlags(rows, "th_1", ev("done"))).toBe(rows)
  })
})

describe("parseGoalReason", () => {
  it("reads JSON and falls back to the raw text", () => {
    expect(parseGoalReason('{"reason":"needs an external change"}')).toBe(
      "needs an external change",
    )
    expect(parseGoalReason("plain")).toBe("plain")
    expect(parseGoalReason("")).toBe("")
  })
})
