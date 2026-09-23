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

  // A host that flags the wait on the thread and sends an empty roster is
  // the only source of that row's line and date; synthesising a bare
  // title leaves the inbox with a badge and nothing else.
  it("carries the thread's own line and date onto a row it synthesises", () => {
    const parked = thread("w", false)
    parked.waiting = true
    parked.summary = "checking again later"
    const live = collectLive([], [parked])
    expect(live[0].action).toBe("checking again later")
    expect(live[0].last_active_at).toBe(parked.last_active_at)
  })

  // The roster is the host's own answer. A thread row repeating a stale
  // line must not overwrite what the live turn is actually doing.
  it("does not let the listing overwrite a roster row the host already sent", () => {
    const parked = thread("w", false)
    parked.waiting = true
    parked.summary = "stale"
    const live = collectLive([run("w", { waiting: true, action: "fresh" })], [parked])
    expect(live).toHaveLength(1)
    expect(live[0].action).toBe("fresh")
  })

  // The host often sends the live row only on the roster. The folder still
  // needs the project, and filling it in must not rewrite the caller's row.
  it("copies the project onto a roster row that omitted it", () => {
    const row = run("a", { action: "read" })
    const th = thread("a", true)
    th.project_id = "p"
    const live = collectLive([row], [th])
    expect(live[0].project_id).toBe("p")
    expect(row.project_id).toBeUndefined()
    const named = collectLive([run("a", { project_id: "from-host" })], [th])
    expect(named[0].project_id).toBe("from-host")
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

  // A parked wait's row carries the thread's summary as its line. Reading
  // that as a live turn puts Stop and a steer box over the Waiting header
  // and the schedule banner until `open` comes back and undoes it.
  it("does not read a parked wait's line as a turn that is running", () => {
    const parked = thread("w", false)
    parked.waiting = true
    const stub = detailFromListing(
      "w",
      [run("w", { waiting: true, action: "checking again later" })],
      [parked],
    )
    expect(stub.waiting).toBe(true)
    expect(stub.running).toBeUndefined()
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
    expect(rosterFingerprint(projects, threads, [run("a", { action: "read", project_id: "p" })], false, "")).not.toBe(
      a,
    )
  })
})
