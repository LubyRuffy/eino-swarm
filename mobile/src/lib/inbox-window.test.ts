import { describe, expect, it } from "vitest"

import { emptyInbox, inboxThreads, reduceInbox, type InboxWindow } from "./inbox-window"
import type { ThreadView } from "./rpc"
import { readInbox } from "./completion-read"

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

  it("pages one section and hides the global more button", () => {
    const first = reduceInbox(
      emptyInbox(),
      {
        groups: [
          { id: "p", threads: [row("a")], more: true, next: "n1" },
          { id: "recent", threads: [row("r")], more: false, next: "" },
        ],
        running: [],
      },
      "replace",
    )
    expect(first.grouped).toBe(true)
    expect(first.more).toBe(false)
    expect(inboxThreads(first).map((t) => t.id)).toEqual(["a", "r"])
    const next = reduceInbox(first, { threads: [row("b")], more: false, next: "" }, "append", "p")
    expect(inboxThreads(next).map((t) => t.id)).toEqual(["a", "r", "b"])
    expect(next.groups.find((g) => g.id === "p")?.more).toBe(false)
    expect(next.groups.find((g) => g.id === "recent")?.more).toBe(false)
  })

  it("refreshes a tail row instead of listing it twice", () => {
    let window = reduceInbox(emptyInbox(), page(["a"], true, "c"), "replace")
    window = reduceInbox(window, page(["b"]), "append")
    window = reduceInbox(window, { threads: [row("b", "renamed")], running: [] }, "append")
    expect(inboxThreads(window).map((t) => t.title)).toEqual(["a", "renamed"])
  })
  it("retains independent completion attention across global and section pagination", () => {
    const attention = Array.from({ length: 8 }, (_, i) => ({ ...row(`done-${i}`), unread: true, completed_seq: i + 1 }))
    const first = reduceInbox(emptyInbox(), { ...page(["a"]), attention }, "replace")
    expect(first.attention).toEqual(attention)
    expect(reduceInbox(first, page(["older"]), "append").attention).toEqual(attention)
    const grouped = reduceInbox(first, { groups: [{ id: "recent", threads: [row("a")] }], attention }, "replace")
    expect(grouped.attention).toEqual(attention)
    expect(reduceInbox(grouped, page(["older"]), "append", "recent").attention).toEqual(attention)
    expect(reduceInbox(grouped, { groups: [], attention: [] }, "replace").attention).toEqual([])
    expect(reduceInbox(first, page(["a"]), "replace").attention).toEqual([])
  })

})

describe("completion attention pages", () => {
  const completed = (id: string, seq = 1) => ({ ...row(id), unread: true, completed_seq: seq })
  it("keeps completion page cursors separate from project and global pagination", () => {
    const first = reduceInbox(emptyInbox(), { ...page(["a"], true, "ordinary"), attention: [completed("done-a")],
      attention_more: true, attention_cursor: "completion-next" }, "replace")
    expect(first.groups.find((g) => g.id === "attention")).toEqual({ id: "attention", more: true, cursor: "completion-next" })
    expect(first.cursor).toBe("ordinary")
    expect(first.more).toBe(true)
  })

  it("appends only unread completion rows and preserves ordinary list and live state", () => {
    const first = reduceInbox(emptyInbox(), { projects: [{ id: "p", name: "Project" }],
      groups: [{ id: "p", threads: [row("a")], more: true, next: "project-next" }],
      running: [{ thread_id: "live", title: "Live", action: "Working" }],
      attention: [completed("done-a")], attention_more: true, attention_cursor: "completion-next" }, "replace")
    const next = reduceInbox(first, { attention: [completed("done-a", 2), completed("done-b"), { ...completed("read"), unread: false }],
      attention_more: false }, "append", "attention")
    expect(next.attention.map((r) => [r.id, r.completed_seq])).toEqual([["done-a", 2], ["done-b", 1]])
    expect(next.head).toEqual(first.head)
    expect(next.tail).toEqual(first.tail)
    expect(next.running).toEqual(first.running)
    expect(next.projects).toEqual(first.projects)
    expect(next.grouped).toBe(true)
    expect(next.groups.find((g) => g.id === "p")).toEqual(first.groups.find((g) => g.id === "p"))
    expect(next.groups.find((g) => g.id === "attention")?.more).toBe(false)
  })

  it("reduces a single attention page without treating an unobserved tail as authoritative", () => {
    let window = reduceInbox(emptyInbox(), { attention: [completed("a"), completed("b")], attention_more: true, attention_cursor: "c1" }, "replace")
    window = reduceInbox(window, { attention: [completed("c"), completed("d")], attention_more: true, attention_cursor: "c2" }, "append", "attention")
    window = reduceInbox(window, { attention: [completed("b", 3), completed("new")], attention_more: true, attention_cursor: "new-first" }, "replace")
    expect(window.attention.map((r) => r.id)).toEqual(["b", "new"])
    expect(window.groups.find((g) => g.id === "attention")?.cursor).toBe("new-first")
    expect(window.attention.find((r) => r.id === "b")?.completed_seq).toBe(3)
  })

  it("keeps the attention cursor and loaded rows while appending an ordinary page", () => {
    let window = reduceInbox(emptyInbox(), { attention: [completed("a")], attention_more: true, attention_cursor: "head" }, "replace")
    window = reduceInbox(window, { attention: [completed("b")], attention_more: true, attention_cursor: "tail" }, "append", "attention")
    window = reduceInbox(window, { threads: [row("ordinary")] }, "append")
    expect(window.attention.map((r) => r.id)).toEqual(["a", "b"])
    expect(window.groups.find((g) => g.id === "attention")?.cursor).toBe("tail")
  })

  it("refreshes loaded tail rows from ordinary listings and removes remotely read completions", () => {
    let window = reduceInbox(emptyInbox(), { attention: [completed("a")], attention_more: true }, "replace")
    window = reduceInbox(window, { attention: [completed("tail")], attention_more: false }, "append", "attention")
    window = reduceInbox(window, { attention: [completed("a")], threads: [{ ...completed("tail"), unread: false }] }, "replace")
    expect(window.attention.map((r) => r.id)).toEqual(["a"])
  })

  it("represents a budget-limited attention page with its independent More cursor", () => {
    const first = reduceInbox(emptyInbox(), { attention: [completed("a")], attention_more: true, attention_cursor: "previous" }, "replace")
    const next = reduceInbox(first, { attention: [], attention_more: true, attention_cursor: "opaque:first" }, "replace")
    expect(next.attention).toEqual([])
    expect(next.groups.find((g) => g.id === "attention")).toEqual({ id: "attention", more: true, cursor: "opaque:first" })
    const empty = reduceInbox(emptyInbox(), { attention_more: true, attention_cursor: "opaque:first" }, "replace")
    expect(empty.groups.find((g) => g.id === "attention")).toEqual({ id: "attention", more: true, cursor: "opaque:first" })
  })

  it("moves a loaded tail into the first page without duplicates and uses a fresh cursor once no tail remains", () => {
    let window = reduceInbox(emptyInbox(), { attention: [completed("a")], attention_more: true, attention_cursor: "first" }, "replace")
    window = reduceInbox(window, { attention: [completed("b")], attention_more: false }, "append", "attention")
    window = reduceInbox(window, { attention: [completed("b"), completed("a")], attention_more: true, attention_cursor: "refreshed" }, "replace")
    expect(window.attention.map((r) => r.id)).toEqual(["b", "a"])
    expect(window.groups.find((g) => g.id === "attention")?.cursor).toBe("refreshed")
  })
})


it("does not relight an acknowledged completion from a delayed inbox snapshot", () => {
  const completed = { ...row("done"), unread: true, completed_seq: 12 }
  const initial = reduceInbox(emptyInbox(), { threads: [completed], attention: [completed] }, "replace")
  const read = readInbox(initial, { id: "done", title: "Done", unread: false, completed_seq: 12 })
  const stale = reduceInbox(read, { threads: [completed], attention: [completed] }, "replace")
  expect(stale.attention).toEqual([])
  expect(stale.head[0].unread).toBe(false)
  const newer = reduceInbox(stale, { attention: [{ ...completed, completed_seq: 14 }] }, "replace")
  expect(newer.attention).toHaveLength(1)
  expect(reduceInbox(emptyInbox(), { attention: [completed] }, "replace").attention).toHaveLength(1)
})
