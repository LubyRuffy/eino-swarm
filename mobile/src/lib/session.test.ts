import { describe, expect, it } from "vitest"

import type { RemoteEvent, RemoteResponse, ThreadDetail } from "./rpc"
import { OpEvent, OpReady } from "./rpc"
import { applyPush, emptyView, markRunning, openView } from "./session"

function detail(partial?: Partial<ThreadDetail>): ThreadDetail {
  return { id: "t1", title: "one", ...partial }
}

function ev(partial: Partial<RemoteEvent> & Pick<RemoteEvent, "kind" | "seq">): RemoteEvent {
  return {
    thread_id: "t1",
    text: "",
    created_at: "2026-01-01T00:00:00Z",
    ...partial,
  }
}

function push(event: RemoteEvent, thread_id = "t1"): RemoteResponse {
  return {
    v: 1,
    id: "",
    ok: true,
    op: OpEvent,
    thread_id,
    seq: event.seq,
    event,
  }
}

describe("phone watch session", () => {
  it("applies catch-up before React would have flushed detail", () => {
    const view = openView(detail())
    const next = applyPush(view, push(ev({ seq: 1, kind: "user_message", text: "hi" })))
    expect(next.blocks).toHaveLength(1)
    expect(next.lastSeq).toBe(1)
    expect(applyPush(emptyView(), push(ev({ seq: 1, kind: "user_message", text: "hi" }))).blocks).toEqual([])
  })

  it("ignores another thread and duplicate seq", () => {
    let view = openView(detail())
    view = applyPush(view, push(ev({ seq: 2, kind: "user_message", text: "a" })))
    view = applyPush(view, push(ev({ seq: 2, kind: "user_message", text: "a" })))
    view = applyPush(view, push(ev({ seq: 3, kind: "delta", text: "nope" }), "other"))
    expect(view.blocks).toHaveLength(1)
    expect(view.lastSeq).toBe(2)
  })

  it("tracks running, title, goal and plan from the same kinds as desktop", () => {
    let view = openView(detail())
    view = applyPush(view, {
      v: 1,
      id: "",
      ok: true,
      op: OpReady,
      thread_id: "t1",
      seq: 4,
      status: { running: true, turn_id: "tu", awaiting_answer: false },
    })
    expect(view.detail?.running?.turn_id).toBe("tu")
    view = applyPush(view, push(ev({ seq: 5, kind: "title", text: "named" })))
    expect(view.detail?.title).toBe("named")
    view = applyPush(view, push(ev({ seq: 6, kind: "goal", text: "ship it" })))
    expect(view.detail?.goal_on).toBe(true)
    view = applyPush(view, push(ev({ seq: 7, kind: "plan", text: "steps" })))
    expect(view.detail?.plan_on).toBe(true)
    view = applyPush(view, push(ev({ seq: 8, kind: "done" })))
    expect(view.detail?.running).toBeUndefined()
    view = markRunning(view)
    expect(view.detail?.running?.thread_id).toBe("t1")
  })
})
