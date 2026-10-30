import { describe, expect, it } from "vitest"

import { memoryChangeView } from "./memory-diff"

const args = JSON.stringify({ action: "replace", old_text: "selector", content: "requested note" })

describe("memoryChangeView", () => {
  it("uses the complete recorded entry instead of the substring selector", () => {
    const view = memoryChangeView(args, JSON.stringify({ success: true, changed: true,
      before: "complete selector and its condition", after: "stored normalized note", entries: ["unrelated note"],
    }))
    expect(view).toMatchObject({ diff: { path: "MEMORY.md", added: 1, removed: 1 } })
    expect(view.diff?.hunks[0].lines).toEqual([
      { op: "del", text: "complete selector and its condition" },
      { op: "add", text: "stored normalized note" },
    ])
    expect(view.noticeKey).toBeUndefined()
  })

  it.each([
    ["add", "", "stored note", 1, 0],
    ["remove", "stored note", "", 0, 1],
  ])("shows a recorded %s with its known empty side", (action, before, after, added, removed) => {
    const view = memoryChangeView(JSON.stringify({ action }), JSON.stringify({ success: true, before, after }))
    expect(view.diff).toMatchObject({ added, removed })
  })

  it("does not invent an addition for an accepted duplicate", () => {
    expect(memoryChangeView(JSON.stringify({ action: "add", content: "existing note" }),
      JSON.stringify({ success: true, changed: false, before: "", after: "existing note" })))
      .toEqual({ noticeKey: "tool.memoryUnchanged" })
  })

  it("labels old records as request fragments and never derives a before-image from entries", () => {
    const view = memoryChangeView(args, JSON.stringify({ success: true, entries: ["newer note"] }))
    expect(view.noticeKey).toBe("tool.memoryPreview")
    expect(view.diff?.hunks[0].lines).toEqual([
      { op: "del", text: "selector" }, { op: "add", text: "requested note" },
    ])
  })

  it.each([undefined, "error: refused", JSON.stringify({ success: false, before: "untrusted", after: "untrusted" }),
    JSON.stringify({ success: true, before: "incomplete" })])("keeps incomplete receipts honest: %s", (result) => {
    const view = memoryChangeView(args, result)
    expect(view.noticeKey).toBe(result?.includes('"success":true') ? "tool.memoryPreview" : "tool.requestedChange")
    expect(view.diff?.hunks[0].lines[0].text).toBe("selector")
  })

  it("does not treat an externally flagged failure as a saved change", () => {
    const view = memoryChangeView(args, JSON.stringify({ success: true, before: "saved", after: "saved" }), true)
    expect(view.noticeKey).toBe("tool.requestedChange")
    expect(view.diff?.removed).toBe(1)
  })

  it("preserves request whitespace and normalizes only the action", () => {
    const view = memoryChangeView(JSON.stringify({ action: " REPLACE ", old_text: "  old\nline", content: "  new\nline" }))
    expect(view.diff?.hunks[0].lines).toEqual([
      { op: "del", text: "  old" }, { op: "add", text: "  new" }, { op: "eq", text: "line" },
    ])
    expect(memoryChangeView(JSON.stringify({ action: "remove", old_text: "old", content: 7 })).diff?.added).toBe(0)
    expect(memoryChangeView(JSON.stringify({ action: "add", content: "new" })).diff?.removed).toBe(0)
  })

  it.each(["broken", "[]", "null", "{}", '{"action":"list"}', '{"action":"add","content":1}',
    '{"action":"add","content":" "}', '{"action":"replace","old_text":"old"}',
    '{"action":"replace","old_text":1,"content":"new"}', '{"action":"remove","old_text":" "}'])
  ("does not invent changes from invalid arguments: %s", (raw) => {
    expect(memoryChangeView(raw)).toEqual({})
  })
})
