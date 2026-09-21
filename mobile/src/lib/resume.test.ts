import { describe, expect, it } from "vitest"

import { collectLive, detailFromListing, pickResumeThread, rosterFingerprint } from "./resume"
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

  it("treats a parked wait as live so bind does not hide a hung-looking thread", () => {
    const parked = thread("w", false)
    parked.waiting = true
    const live = collectLive([run("w", { waiting: true })], [parked, thread("idle", false)])
    expect(live.map((r) => r.thread_id)).toEqual(["w"])
    expect(live[0].waiting).toBe(true)
    expect(pickResumeThread("", [], [parked])).toBe("w")
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

describe("detailFromListing", () => {
  it("paints a stub from the inbox so a tap does not wait on watch", () => {
    const parked = thread("w", false)
    parked.waiting = true
    parked.title = "parked"
    expect(detailFromListing("w", [run("w", { waiting: true })], [parked])).toEqual({
      id: "w",
      title: "parked",
      waiting: true,
    })
    expect(
      detailFromListing("a", [run("a", { turn_id: "tu", action: "read" })], [thread("a", true)]),
    ).toEqual({
      id: "a",
      title: "thread a",
      running: { thread_id: "a", title: "thread a", turn_id: "tu", action: "read" },
    })
    expect(detailFromListing("missing", [], [])).toEqual({
      id: "missing",
      title: "missing",
    })
  })
})

describe("rosterFingerprint", () => {
  it("stays the same when a 2s inbox poll repeats the roster so a tap is not eaten", () => {
    const projects = [{ id: "p", name: "P" }]
    const threads = [thread("a", false)]
    const running = [run("a", { action: "read" })]
    const a = rosterFingerprint(projects, threads, running, false, "")
    const b = rosterFingerprint(projects, threads, running, false, "")
    expect(a).toBe(b)
    expect(rosterFingerprint(projects, threads, [run("a", { action: "exec" })], false, "")).not.toBe(a)
  })
})
