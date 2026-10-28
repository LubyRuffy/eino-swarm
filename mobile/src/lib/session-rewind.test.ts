import { describe, expect, it } from "vitest"

import type { RemoteEvent, RemoteResponse } from "./rpc"
import { OpEvent, OpReady } from "./rpc"
import { applyPush, openView, prependOlder, type PhoneView } from "./session"

function ev(seq: number, kind: string, text = "", extra: Partial<RemoteEvent> = {}): RemoteEvent {
  return { thread_id: "t1", agent_id: "manager", seq, kind, text, created_at: "2026-01-01T00:00:00Z", ...extra }
}

function push(event: RemoteEvent): RemoteResponse {
  return { v: 1, id: "", ok: true, op: OpEvent, thread_id: "t1", seq: event.seq, event }
}

function ready(events: RemoteEvent[], more = false): RemoteResponse {
  return { v: 1, id: "", ok: true, op: OpReady, thread_id: "t1", seq: events.at(-1)?.seq ?? 0, more, events, status: { running: false } }
}

function opened(events: RemoteEvent[], more = false): PhoneView {
  return applyPush(openView({ id: "t1", title: "one" }), ready(events, more))
}

const texts = (view: PhoneView) => view.blocks.map((b) => b.text)

// Another client resent an earlier message. The phone has to drop what the
// host removed, or it keeps showing a conversation that no longer exists
// until the user leaves and reopens it.
describe("phone rewind", () => {
  it("drops the removed rows and the streaming answer when the host cuts", () => {
    let view = opened([ev(1, "user_message", "kept"), ev(2, "agent_message", "kept answer"), ev(3, "user_message", "removed")])
    view = applyPush(view, push(ev(0, "delta", "removed partial")))
    view = applyPush(view, push(ev(6, "rewound", "3")))
    expect(texts(view)).toEqual(["kept", "kept answer"])
    view = applyPush(view, push(ev(7, "user_message", "replacement")))
    view = applyPush(view, push(ev(6, "rewound", "3")))
    expect(texts(view)).toEqual(["kept", "kept answer", "replacement"])
    expect(view.lastSeq).toBe(7)
  })

  it("still cuts when the host could not store the marker", () => {
    let view = opened([ev(1, "user_message", "kept"), ev(3, "user_message", "removed")])
    view = applyPush(view, push(ev(0, "rewound", "3")))
    expect(texts(view)).toEqual(["kept"])
  })

  it("pages the rows under a cut that removed everything loaded", () => {
    let view = opened([ev(5, "user_message", "removed")], true)
    view = applyPush(view, push(ev(9, "rewound", "4")))
    expect(texts(view)).toEqual([])
    expect(view.oldestSeq).toBe(4)
    expect(view.hasMore).toBe(true)
    view = prependOlder(view, [ev(2, "user_message", "older")], false)
    expect(texts(view)).toEqual(["older"])
  })

  it("applies a marker that arrives with a later snapshot to rows already loaded", () => {
    let view = opened([ev(1, "user_message", "kept"), ev(3, "user_message", "removed")])
    view = applyPush(view, ready([ev(6, "rewound", "3"), ev(7, "user_message", "replacement")]))
    expect(texts(view)).toEqual(["kept", "replacement"])
  })

  it("leaves newer rows alone when an older page carries an old marker", () => {
    let view = opened([ev(7, "user_message", "replacement")], true)
    view = prependOlder(view, [ev(1, "user_message", "kept"), ev(6, "rewound", "3")], false)
    expect(texts(view)).toEqual(["kept", "replacement"])
  })
})
