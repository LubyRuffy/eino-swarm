import { describe, expect, it } from "vitest"

import { mergeClientTools } from "./local-clients"
import type { ClientTool } from "./rpc"

const recent: ClientTool = {
  id: "codex",
  more: true,
  next: "10",
  tasks: [{ id: "a", title: "fresh", updated_at: "2026-09-25T00:00:00Z", status: "running" }],
}

describe("mergeClientTools", () => {
  it("keeps an older row across a list refresh", () => {
    const opened = mergeClientTools(
      [recent],
      [{ id: "codex", more: false, tasks: [{ id: "b", title: "old", updated_at: "2026-09-01T00:00:00Z", status: "done" }] }],
      "append",
    )
    const refreshed = mergeClientTools(
      opened,
      [{ ...recent, tasks: [{ ...recent.tasks[0], status: "done" }] }],
      "replace",
    )
    expect(refreshed[0].tasks.map((task) => task.id)).toEqual(["a", "b"])
    expect(refreshed[0].tasks[0].status).toBe("done")
    expect(refreshed[0].more).toBe(false)
    expect(refreshed[0].next).toBeUndefined()
  })

  it("keeps the further more cursor across a list refresh", () => {
    const opened = mergeClientTools(
      [recent],
      [{ id: "codex", more: true, next: "4", tasks: [{ id: "b", title: "old", updated_at: "2026-09-01T00:00:00Z", status: "done" }] }],
      "append",
    )
    const refreshed = mergeClientTools(opened, [recent], "replace")
    expect(refreshed[0].next).toBe("4")
    expect(refreshed[0].more).toBe(true)
  })
})
