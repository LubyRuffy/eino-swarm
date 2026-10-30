import { expect, it, vi } from "vitest"

import { applyGoalDetail, goalState } from "./goal"
import { MockHost, MockLink } from "./mock-link"
import { OpOpen, OpRead, OpResumeGoal, OpSend, OpWatch, type RemoteResponse } from "./rpc"
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

for (const fixture of [
  { history: "goal-paused", pause: "goal_capped", state: "paused" },
  { history: "goal-blocked", pause: "goal_blocked", state: "blocked" },
  { history: "goal-idle", pause: "goal_idle", state: "paused" },
] as const) {
  it(`${fixture.history}: RPC reads retain the pause through a completed send until explicit resume`, async () => {
    const prior = location.href
    history.replaceState(null, "", `/?mock=1&tick=0&history=${fixture.history}`)
    vi.useFakeTimers()
    const host = new MockHost(0)
    const link = new MockLink(host)
    try {
      const frames: RemoteResponse[] = []
      link.onPush = (frame) => frames.push(frame)
      const ready = await link.rpc({ op: OpWatch, thread_id: "t-live", include_detail: true })
      expect(ready.ok).toBe(true)
      expect(ready.detail?.[fixture.pause]).toBe(true)
      const initial = (await link.rpc({ op: OpOpen, thread_id: "t-live" })).detail
      expect(initial?.[fixture.pause]).toBe(true)
      expect(initial && goalState(initial)).toBe(fixture.state)
      const reason = initial?.goal_block_reason
      if (fixture.pause === "goal_blocked") {
        expect(reason?.trim()).toBeTruthy()
      }
      const assertPaused = (detail: ThreadDetail | undefined) => {
        expect(detail?.[fixture.pause]).toBe(true)
        expect(detail && goalState(detail)).toBe(fixture.state)
        expect(detail?.goal_block_reason).toBe(reason)
        expect(detail?.goal_complete).not.toBe(true)
      }
      // Read acknowledgements return authoritative detail too. A fixture that
      // only paints a pause in its event seed loses it at this actual boundary.
      const beforeRead = await link.rpc({ op: OpRead, thread_id: "t-live", completed_seq: 0 })
      expect(beforeRead.ok).toBe(true)
      assertPaused(beforeRead.detail)

      const message = "Describe the current state."
      expect((await link.rpc({ op: OpSend, thread_id: "t-live", text: message })).ok).toBe(true)
      await vi.runAllTimersAsync()
      expect(frames.filter((frame) => frame.event?.kind === "user_message" && frame.event.text === message)).toHaveLength(1)
      const completed = frames.filter((frame) => frame.event?.kind === "done")
      expect(completed).toHaveLength(1)
      expect(frames.filter((frame) => frame.event?.kind === "goal_resumed")).toHaveLength(0)
      const afterSend = (await link.rpc({ op: OpOpen, thread_id: "t-live" })).detail
      assertPaused(afterSend)
      expect(afterSend?.running).toBeUndefined()
      const completedSeq = completed[0].event?.seq ?? 0
      expect(completedSeq).toBeGreaterThan(0)
      expect(afterSend?.completed_seq).toBe(completedSeq)
      const afterRead = await link.rpc({ op: OpRead, thread_id: "t-live", completed_seq: completedSeq })
      expect(afterRead.ok).toBe(true)
      assertPaused(afterRead.detail)

      expect((await link.rpc({ op: OpResumeGoal, thread_id: "t-live" })).ok).toBe(true)
      expect(frames.filter((frame) => frame.event?.kind === "goal_resumed")).toHaveLength(1)
      await vi.runAllTimersAsync()
      const resumed = (await link.rpc({ op: OpOpen, thread_id: "t-live" })).detail
      expect(resumed && goalState(resumed)).toBe("pursuing")
      for (const pause of ["goal_capped", "goal_blocked", "goal_idle"] as const) {
        expect(Boolean(resumed?.[pause])).toBe(false)
      }
      expect(resumed?.goal_block_reason ?? "").toBe("")
      const resumedRead = await link.rpc({ op: OpRead, thread_id: "t-live", completed_seq: resumed?.completed_seq ?? 0 })
      expect(resumedRead.ok).toBe(true)
      expect(resumedRead.detail && goalState(resumedRead.detail)).toBe("pursuing")
      expect(resumedRead.detail?.goal_block_reason ?? "").toBe("")
      expect(frames.filter((frame) => frame.event?.kind === "goal_resumed")).toHaveLength(1)
    } finally {
      link.close()
      host.stop()
      vi.useRealTimers()
      history.replaceState(null, "", prior)
    }
  })
}
