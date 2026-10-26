import { expect, it, vi } from "vitest"

import { applyGoalDetail, goalState } from "./goal"
import { MockHost, MockLink } from "./mock-link"
import { OpOpen, OpResumeGoal, OpSend, OpWatch, type RemoteResponse } from "./rpc"
import type { ThreadDetail } from "./rpc"

for (const pause of ["goal_capped", "goal_blocked", "goal_idle"] as const) {
  it(`${pause} survives a human send until the explicit resume receipt`, () => {
    let detail: ThreadDetail = { id: "paused", title: "Paused objective", goal: "Verify the required artifact.", [pause]: true }
    const expected = pause === "goal_blocked" ? "blocked" : "paused"
    for (const kind of ["user_message", "agent_message", "done"]) {
      detail = applyGoalDetail(detail, { kind, text: "A current status response." })
      expect(goalState(detail)).toBe(expected)
    }
    detail = applyGoalDetail(detail, { kind: "goal_resumed" })
    expect(goalState(detail)).toBe("pursuing")
  })
}

it("the phone fixture distinguishes ordinary send from resume_goal", async () => {
  const prior = location.href
  history.replaceState(null, "", "/?mock=1&tick=0&history=goal-paused")
  vi.useFakeTimers()
  const host = new MockHost(0)
  const link = new MockLink(host)
  try {
    const frames: RemoteResponse[] = []
    link.onPush = (frame) => frames.push(frame)
    await link.rpc({ op: OpWatch, thread_id: "t-live" })
    expect((await link.rpc({ op: OpOpen, thread_id: "t-live" })).detail?.goal_capped).toBe(true)
    expect((await link.rpc({ op: OpSend, thread_id: "t-live", text: "Describe the current state." })).ok).toBe(true)
    await vi.runAllTimersAsync()
    expect(frames.some((frame) => frame.event?.kind === "done")).toBe(true)
    expect(frames.some((frame) => frame.event?.kind === "goal_resumed")).toBe(false)
    expect((await link.rpc({ op: OpOpen, thread_id: "t-live" })).detail?.goal_capped).toBe(true)
    expect((await link.rpc({ op: OpResumeGoal, thread_id: "t-live" })).ok).toBe(true)
    expect(frames.filter((frame) => frame.event?.kind === "goal_resumed")).toHaveLength(1)
    expect((await link.rpc({ op: OpOpen, thread_id: "t-live" })).detail?.goal_capped).toBe(false)
  } finally {
    link.close()
    host.stop()
    vi.useRealTimers()
    history.replaceState(null, "", prior)
  }
})
