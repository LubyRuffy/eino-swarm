import { describe, expect, it } from "vitest"

import { setLocale } from "./i18n"
import type { RemoteEvent, RemoteResponse } from "./rpc"
import { applyPush, openView, prependOlder } from "./session"
import { applyEvent } from "./transcript"

const complete = (text: string): RemoteEvent => ({
  thread_id: "t1", seq: 2, kind: "goal_complete", agent_id: "manager",
  text, created_at: "2026-10-23T00:00:00Z",
})

describe("completed goal history", () => {
  it.each([
    ["en", "Standing objective completed."],
    ["zh", "目标已完成。"],
  ] as const)("shows a %s completion notice for actual JSON and legacy payloads", (locale, notice) => {
    setLocale(locale)
    for (const payload of ['{"summary":"Current evidence verified."}', "{}", "", "legacy completion"]) {
      expect(applyEvent([], complete(payload))).toMatchObject([{ kind: "notice", text: notice }])
    }
    setLocale("en")
  })

  it("retains one completion notice and durable state through live, cold and paged replay", () => {
    setLocale("en")
    const event = complete('{"summary":"Current evidence verified."}')
    const ready: RemoteResponse = { v: 1, id: "", ok: true, op: "ready", thread_id: "t1", seq: 2, events: [event] }
    let live = applyPush(openView({ id: "t1", title: "Task", goal: "Verify current work" }), { ...ready, seq: 1, events: [] })
    const push: RemoteResponse = { v: 1, id: "", ok: true, op: "event", thread_id: "t1", event }
    live = applyPush(live, push)
    live = applyPush(live, push)
    const cold = applyPush(openView({ id: "t1", title: "Task", goal: "Verify current work", goal_complete: true }), ready)
    const paged = prependOlder(live, [event], false)
    for (const view of [live, cold, paged]) {
      expect(view.detail).toMatchObject({ goal: "Verify current work", goal_complete: true })
      expect(view.blocks.filter((block) => block.text === "Standing objective completed.")).toHaveLength(1)
    }
  })
})
