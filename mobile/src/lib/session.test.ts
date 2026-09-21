import { describe, expect, it } from "vitest"

import type { RemoteEvent, RemoteResponse, ThreadDetail } from "./rpc"
import { OpEvent, OpReady } from "./rpc"
import { applyPush, emptyView, markRunning, openView, prependOlder } from "./session"

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

function ready(partial?: Partial<RemoteResponse>): RemoteResponse {
  return {
    v: 1,
    id: "",
    ok: true,
    op: OpReady,
    thread_id: "t1",
    seq: 0,
    status: { running: false },
    ...partial,
  }
}

describe("phone watch session", () => {
  it("does not paint catch-up until ready, then folds it in one shot", () => {
    let view = openView(detail())
    view = applyPush(view, push(ev({ seq: 1, kind: "user_message", text: "hi" })))
    expect(view.blocks).toEqual([])
    expect(view.queued).toHaveLength(1)
    view = applyPush(view, ready({ seq: 1 }))
    expect(view.blocks).toHaveLength(1)
    expect(view.lastSeq).toBe(1)
    expect(view.caughtUp).toBe(true)
    expect(view.queued).toEqual([])
    expect(applyPush(emptyView(), push(ev({ seq: 1, kind: "user_message", text: "hi" }))).blocks).toEqual([])
  })

  it("paints ready.events in one shot without a replay", () => {
    const view = applyPush(
      openView(detail()),
      ready({
        seq: 3,
        more: true,
        events: [
          ev({ seq: 2, kind: "user_message", text: "mid" }),
          ev({ seq: 3, kind: "agent_message", text: "tail" }),
        ],
      }),
    )
    expect(view.blocks.map((b) => b.text)).toEqual(["mid", "tail"])
    expect(view.oldestSeq).toBe(2)
    expect(view.lastSeq).toBe(3)
    expect(view.hasMore).toBe(true)
    expect(view.caughtUp).toBe(true)
  })

  it("ignores another thread and duplicate seq", () => {
    let view = applyPush(openView(detail()), ready({ seq: 0 }))
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
    view = applyPush(view, push(ev({ seq: 6, kind: "goal", text: "objective" })))
    expect(view.detail?.goal_on).toBe(true)
    view = applyPush(view, push(ev({ seq: 7, kind: "plan", text: "steps" })))
    expect(view.detail?.plan_on).toBe(true)
    view = applyPush(view, push(ev({ seq: 8, kind: "done" })))
    expect(view.detail?.running).toBeUndefined()
    view = markRunning(view)
    expect(view.detail?.running?.thread_id).toBe("t1")
  })

  it("opens hasMore from ready and prepends older events above the viewport", () => {
    let view = applyPush(
      openView(detail()),
      ready({
        seq: 4,
        more: true,
        events: [ev({ seq: 4, kind: "user_message", text: "now" })],
      }),
    )
    expect(view.hasMore).toBe(true)
    expect(view.oldestSeq).toBe(4)
    view = prependOlder(
      view,
      [ev({ seq: 2, kind: "user_message", text: "old" }), ev({ seq: 4, kind: "user_message", text: "dup" })],
      false,
    )
    expect(view.hasMore).toBe(false)
    expect(view.oldestSeq).toBe(2)
    expect(view.blocks.map((b) => b.text)).toEqual(["old", "now"])
    view = prependOlder(view, [ev({ seq: 2, kind: "user_message", text: "old" })], true)
    expect(view.hasMore).toBe(false)
    view = { ...view, hasMore: true, oldestSeq: 8 }
    view = prependOlder(view, [], true, 3)
    expect(view.hasMore).toBe(true)
    expect(view.oldestSeq).toBe(3)
    view = applyPush(view, ready({ seq: 8, status: { running: false } }))
    expect(view.hasMore).toBe(false)
  })

  it("keeps a live streaming answer when older rows are prepended", () => {
    let view = applyPush(openView(detail()), ready({ seq: 0 }))
    view = applyPush(view, push(ev({ seq: 3, kind: "user_message", text: "q" })))
    view = applyPush(view, push(ev({ seq: 0, kind: "delta", text: "partial" })))
    view = prependOlder(view, [ev({ seq: 1, kind: "user_message", text: "prev" })], true)
    expect(view.blocks.map((b) => b.text)).toEqual(["prev", "q", "partial"])
    expect(view.blocks[2]?.streaming).toBe(true)
  })

  it("drops another thread's older page", () => {
    let view = applyPush(
      openView(detail()),
      ready({ seq: 4, events: [ev({ seq: 4, kind: "user_message", text: "now" })] }),
    )
    view = prependOlder(
      view,
      [ev({ seq: 1, kind: "user_message", text: "nope", thread_id: "other" })],
      true,
      1,
    )
    expect(view.blocks.map((b) => b.text)).toEqual(["now"])
    expect(view.hasMore).toBe(true)
    expect(view.oldestSeq).toBe(1)
  })
})
