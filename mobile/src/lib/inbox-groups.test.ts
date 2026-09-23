import { describe, expect, it } from "vitest"

import { groupInbox } from "./inbox-groups"
import type { RunningView, ThreadView } from "./rpc"

function thread(id: string, extra?: Partial<ThreadView>): ThreadView {
  return {
    id,
    title: "thread " + id,
    running: false,
    last_active_at: "2026-01-01T00:00:00Z",
    ...extra,
  }
}

function live(id: string, extra?: Partial<RunningView>): RunningView {
  return { thread_id: id, title: "thread " + id, ...extra }
}

const projects = [
  { id: "p", name: "work" },
  { id: "q", name: "notes" },
]

describe("groupInbox", () => {
  it("lists a live conversation under its project even when the idle page omitted it", () => {
    const groups = groupInbox(
      projects,
      [thread("idle", { project_id: "p", title: "idle" })],
      [live("hot", { project_id: "p", title: "hot", action: "reading" })],
    )
    const work = groups.find((g) => g.id === "p")
    expect(work?.threads.map((th) => th.id)).toEqual(["hot", "idle"])
    expect(work?.threads[0].summary).toBe("reading")
    expect(groups.find((g) => g.id === "q")?.threads).toEqual([])
    expect(groups.some((g) => g.id === "")).toBe(false)
  })

  it("does not paint the same project conversation twice inside the folder", () => {
    const groups = groupInbox(
      projects,
      [thread("hot", { project_id: "p", title: "hot" })],
      [live("hot", { project_id: "p", title: "hot" })],
    )
    expect(groups.find((g) => g.id === "p")?.threads).toHaveLength(1)
  })

  it("keeps a live conversation with no project off Recents", () => {
    const groups = groupInbox(
      projects,
      [thread("loose", { title: "loose" }), thread("hot", { title: "hot" })],
      [live("hot", { title: "hot" })],
    )
    const recent = groups.find((g) => g.id === "")
    expect(recent?.threads.map((th) => th.id)).toEqual(["loose"])
    expect(groups.find((g) => g.id === "p")?.threads).toEqual([])
  })

  it("does not invent a folder for a project this PC did not list", () => {
    const groups = groupInbox(projects, [], [live("hot", { project_id: "gone", title: "hot" })])
    expect(groups.flatMap((g) => g.threads)).toEqual([])
  })
})
