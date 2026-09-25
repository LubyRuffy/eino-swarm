import { describe, expect, it } from "vitest"

import { mergeClientTools, type ClientTool } from "./local-clients"

const recent: ClientTool = {
  id: "claude",
  more: true,
  next: "10",
  tasks: [{ id: "a", title: "fresh", updated_at: "2026-09-25T00:00:00Z", status: "running" }],
}

describe("mergeClientTools", () => {
  it("keeps a more-page row when the recent page refreshes", () => {
    const withOlder = mergeClientTools([], [recent], "replace")
    const opened = mergeClientTools(
      withOlder,
      [{ id: "claude", more: false, tasks: [{ id: "b", title: "old", updated_at: "2026-09-01T00:00:00Z", status: "done" }] }],
      "append",
    )
    const refreshed = mergeClientTools(
      opened,
      [{ ...recent, tasks: [{ id: "a", title: "fresh", updated_at: "2026-09-25T00:00:00Z", status: "done" }] }],
      "replace",
    )
    const claude = refreshed.find((tool) => tool.id === "claude")
    expect(claude?.tasks.map((task) => task.id)).toEqual(["a", "b"])
    expect(claude?.tasks[0].status).toBe("done")
    expect(claude?.more).toBe(false)
    expect(claude?.next).toBeUndefined()
  })

  it("keeps the further more cursor when the recent page refreshes", () => {
    const opened = mergeClientTools(
      [recent],
      [{ id: "claude", more: true, next: "4", tasks: [{ id: "b", title: "old", updated_at: "2026-09-01T00:00:00Z", status: "done" }] }],
      "append",
    )
    const refreshed = mergeClientTools(opened, [recent], "replace")
    expect(refreshed[0].tasks.map((task) => task.id)).toEqual(["a", "b"])
    expect(refreshed[0].more).toBe(true)
    expect(refreshed[0].next).toBe("4")
  })

  it("does not invent a title the page never sent", () => {
    const merged = mergeClientTools([], [recent], "replace")
    expect(merged[0].tasks.some((task) => task.title.includes("notes.md"))).toBe(false)
  })
})
