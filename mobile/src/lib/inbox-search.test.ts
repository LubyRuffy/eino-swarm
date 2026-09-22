import { describe, expect, it } from "vitest"

import { searchRunning, searchThreads } from "./inbox-search"
import type { RunningView, ThreadView } from "./rpc"

function thread(over: Partial<ThreadView>): ThreadView {
  return {
    id: "t1",
    title: "alpha",
    running: false,
    last_active_at: "2026-01-01T00:00:00Z",
    ...over,
  }
}

describe("inbox search", () => {
  it("keeps every row when the box is empty or whitespace", () => {
    const rows = [thread({}), thread({ id: "t2", title: "beta" })]
    expect(searchThreads(rows, "")).toBe(rows)
    expect(searchThreads(rows, "   ")).toBe(rows)
    expect(searchRunning([{ thread_id: "t1", title: "alpha" }], " ")).toHaveLength(1)
  })

  it("matches a title whatever case it was typed in", () => {
    const rows = [thread({ title: "Alpha Beta" }), thread({ id: "t2", title: "gamma" })]
    expect(searchThreads(rows, "ALPHA").map((r) => r.id)).toEqual(["t1"])
    expect(searchThreads(rows, "  beta ").map((r) => r.id)).toEqual(["t1"])
    expect(searchThreads(rows, "delta")).toHaveLength(0)
  })

  // The id is on screen whenever a conversation has no title yet, so a
  // search that ignored it would fail to find a row the user can read.
  it("matches the id a titleless row falls back to", () => {
    const rows = [thread({ id: "th-9f", title: "" })]
    expect(searchThreads(rows, "9f")).toHaveLength(1)
  })

  it("matches the line the row paints, not the tool envelope behind it", () => {
    const rows = [
      thread({ id: "t1", summary: `report_schedule({"findings":"the export grew"})` }),
    ]
    expect(searchThreads(rows, "the export grew")).toHaveLength(1)
    expect(searchThreads(rows, "report_schedule")).toHaveLength(0)
  })

  it("filters live rows on their action preview too", () => {
    const live: RunningView[] = [
      { thread_id: "t1", title: "alpha", action: "reading a file" },
      { thread_id: "t2", title: "beta", action: "" },
    ]
    expect(searchRunning(live, "reading").map((r) => r.thread_id)).toEqual(["t1"])
    expect(searchRunning(live, "beta").map((r) => r.thread_id)).toEqual(["t2"])
  })
})
