import { describe, expect, it } from "vitest"

import { emptyInbox, inboxThreads, reduceInbox, type InboxWindow } from "./inbox-window"
import type { ThreadView } from "./rpc"

function row(id: string, title = id): ThreadView {
  return { id, title, running: false, last_active_at: "2026-01-01T00:00:00Z" }
}

function page(ids: string[], more = false, next = "") {
  return { threads: ids.map((id) => row(id)), running: [], more, next }
}

describe("reduceInbox", () => {
  it("replaces the first page until more has extended the window", () => {
    const first = reduceInbox(emptyInbox(), page(["a", "b"], true, "c1"), "replace")
    expect(inboxThreads(first).map((t) => t.id)).toEqual(["a", "b"])
    const again = reduceInbox(first, page(["b", "d"], true, "c2"), "replace")
    expect(inboxThreads(again).map((t) => t.id)).toEqual(["b", "d"])
    expect(again.cursor).toBe("c2")
  })

  // The poll is another first page. Rows More already loaded are not on
  // that page, and they are not deleted just because the head moved.
  it("keeps the tail when a later first page drops and adds rows", () => {
    let window: InboxWindow = reduceInbox(emptyInbox(), page(["a", "b"], true, "c1"), "replace")
    window = reduceInbox(window, page(["c"], false, ""), "append")
    expect(inboxThreads(window).map((t) => t.id)).toEqual(["a", "b", "c"])
    window = reduceInbox(window, page(["b", "d"], true, "c9"), "replace")
    expect(inboxThreads(window).map((t) => t.id)).toEqual(["b", "d", "c"])
    expect(window.more).toBe(false)
    expect(window.cursor).toBe("")
  })

  it("does not keep a first-page row that the next first page no longer has", () => {
    let window = reduceInbox(emptyInbox(), page(["a", "b"]), "replace")
    window = reduceInbox(window, page(["b"]), "replace")
    expect(inboxThreads(window).map((t) => t.id)).toEqual(["b"])
  })

  it("moves a tail row into the head once the first page includes it", () => {
    let window = reduceInbox(emptyInbox(), page(["a"], true, "c"), "replace")
    window = reduceInbox(window, page(["b"]), "append")
    window = reduceInbox(window, page(["b", "a"]), "replace")
    expect(inboxThreads(window).map((t) => t.id)).toEqual(["b", "a"])
    expect(window.tail).toEqual([])
  })

  it("refreshes a tail row instead of listing it twice", () => {
    let window = reduceInbox(emptyInbox(), page(["a"], true, "c"), "replace")
    window = reduceInbox(window, page(["b"]), "append")
    window = reduceInbox(window, { threads: [row("b", "renamed")], running: [] }, "append")
    expect(inboxThreads(window).map((t) => t.title)).toEqual(["a", "renamed"])
  })
})
