import { describe, expect, it } from "vitest"

import { collectLive, pickResumeThread } from "./resume"
import type { RunningView, ThreadView } from "./rpc"

function thread(id: string, running: boolean): ThreadView {
  return { id, title: "thread " + id, running, last_active_at: "2026-01-01T00:00:00Z" }
}

function run(id: string, extra?: Partial<RunningView>): RunningView {
  return { thread_id: id, title: "thread " + id, ...extra }
}

describe("collectLive", () => {
  it("merges the roster with the listing flag and drops duplicates", () => {
    const live = collectLive(
      [run("a", { action: "read" }), run("a")],
      [thread("a", true), thread("b", true), thread("c", false)],
    )
    expect(live.map((r) => r.thread_id)).toEqual(["a", "b"])
    expect(live[0].action).toBe("read")
  })
})

describe("pickResumeThread", () => {
  it("prefers the last id when that thread is still running", () => {
    expect(pickResumeThread("b", [run("a"), run("b")], [thread("b", false)])).toBe("b")
  })

  it("opens the first live turn when last is idle or unknown", () => {
    expect(pickResumeThread("z", [run("a")], [thread("z", false)])).toBe("a")
    expect(pickResumeThread("z", [], [thread("a", true)])).toBe("a")
  })

  it("reopens the last thread when nothing is running", () => {
    expect(pickResumeThread("a", [], [thread("a", false), thread("b", false)])).toBe("a")
    expect(pickResumeThread("old", [], [thread("a", false)])).toBe("old")
  })

  it("stays on the inbox when there is no last thread and nothing live", () => {
    expect(pickResumeThread("", [], [thread("a", false)])).toBe("")
    expect(pickResumeThread("", [], [])).toBe("")
  })
})
