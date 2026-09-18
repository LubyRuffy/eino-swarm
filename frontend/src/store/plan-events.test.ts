import { describe, expect, it } from "vitest"

import { applyPlanThreadFlags } from "./plan-events"
import type { SwarmEvent, Thread } from "@/lib/types"

function thread(extra: Partial<Thread> = {}): Thread {
  return {
    id: "th_1",
    title: "t",
    project_id: "",
    provider_id: "default",
    reasoning_effort: "",
    archived: false,
    created_at: "2026-01-01T00:00:00Z",
    last_active_at: "2026-01-01T00:00:00Z",
    running: false,
    ...extra,
  } as Thread
}

function ev(kind: string, text?: string): SwarmEvent {
  return {
    thread_id: "th_1",
    turn_id: "tn_1",
    seq: 1,
    kind,
    agent_id: "manager",
    text,
    created_at: "2026-01-01T00:00:00Z",
  }
}

describe("applyPlanThreadFlags", () => {
  it("enters planning from a plan event", () => {
    const next = applyPlanThreadFlags([thread()], "th_1", ev("plan"))
    expect(next[0]?.plan_mode).toBe(true)
  })

  it("stores the markdown from a plan_updated event", () => {
    const next = applyPlanThreadFlags(
      [thread({ plan_mode: true })],
      "th_1",
      ev("plan_updated", "# Plan\n\nDo the work.\n"),
    )
    expect(next[0]?.plan_mode).toBe(true)
    expect(next[0]?.plan_markdown).toBe("# Plan\n\nDo the work.\n")
  })

  it("leaves planning on implement or cancel", () => {
    const rows = [thread({ plan_mode: true, plan_markdown: "# Plan" })]
    expect(applyPlanThreadFlags(rows, "th_1", ev("plan_implemented"))[0]?.plan_mode).toBe(false)
    expect(applyPlanThreadFlags(rows, "th_1", ev("plan_cancelled"))[0]?.plan_mode).toBe(false)
  })

  it("ignores unrelated events", () => {
    const rows = [thread({ plan_mode: true })]
    expect(applyPlanThreadFlags(rows, "th_1", ev("done"))).toBe(rows)
  })
})
