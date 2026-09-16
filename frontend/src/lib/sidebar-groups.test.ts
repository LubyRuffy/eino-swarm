import { describe, expect, it } from "vitest"

import { sidebarBuckets, threadSections } from "./sidebar-groups"
import type { Thread } from "./types"

function thread(partial: Partial<Thread> & { id: string }): Thread {
  return {
    title: partial.title ?? partial.id,
    project_id: "",
    provider_id: "default",
    reasoning_effort: "",
    archived: false,
    created_at: "2026-09-16T00:00:00Z",
    last_active_at: partial.last_active_at ?? new Date().toISOString(),
    running: false,
    sort_rank: 0,
    ...partial,
  }
}

describe("threadSections", () => {
  it("groups an auto-sorted list by last activity, not creation", () => {
    const yesterday = new Date()
    yesterday.setDate(yesterday.getDate() - 1)
    const sections = threadSections([
      thread({ id: "today", last_active_at: new Date().toISOString() }),
      thread({
        id: "older",
        last_active_at: yesterday.toISOString(),
        created_at: new Date().toISOString(),
      }),
    ])
    expect(sections.map((s) => s.label)).toEqual(["Today", "Yesterday"])
    expect(sections[0].threads.map((t) => t.id)).toEqual(["today"])
    expect(sections[1].threads.map((t) => t.id)).toEqual(["older"])
  })

  it("drops day headers once any row has a dragged rank", () => {
    const sections = threadSections([
      thread({ id: "pinned", sort_rank: 1000, last_active_at: new Date().toISOString() }),
      thread({ id: "auto", sort_rank: 0, last_active_at: new Date().toISOString() }),
    ])
    expect(sections).toHaveLength(1)
    expect(sections[0].label).toBeUndefined()
    expect(sections[0].threads.map((t) => t.id)).toEqual(["pinned", "auto"])
  })
})

describe("sidebarBuckets", () => {
  it("puts project topics under their project and the rest in Recents", () => {
    const buckets = sidebarBuckets([
      thread({ id: "in-a", project_id: "pj_a", last_active_at: "2026-09-16T12:00:00Z" }),
      thread({ id: "loose", project_id: "", last_active_at: "2026-09-16T11:00:00Z" }),
      thread({ id: "in-b", project_id: "pj_b", last_active_at: "2026-09-16T10:00:00Z" }),
    ])
    expect(buckets.recents.map((t) => t.id)).toEqual(["loose"])
    expect(buckets.byProject.pj_a.map((t) => t.id)).toEqual(["in-a"])
    expect(buckets.byProject.pj_b.map((t) => t.id)).toEqual(["in-b"])
    expect(buckets.pinned).toEqual([])
  })

  it("tracks a pinned project topic at the top without taking it out of the project", () => {
    const buckets = sidebarBuckets([
      thread({
        id: "watch",
        project_id: "pj_a",
        pinned: true,
        pinned_at: "2026-09-16T12:00:00Z",
        last_active_at: "2026-09-16T09:00:00Z",
      }),
      thread({
        id: "other",
        project_id: "pj_a",
        last_active_at: "2026-09-16T11:00:00Z",
      }),
    ])
    expect(buckets.pinned.map((t) => t.id)).toEqual(["watch"])
    expect(buckets.byProject.pj_a.map((t) => t.id)).toEqual(["other", "watch"])
  })

  it("orders pins by when they were pinned, newest first", () => {
    const buckets = sidebarBuckets([
      thread({
        id: "older",
        project_id: "pj_a",
        pinned: true,
        pinned_at: "2026-09-16T10:00:00Z",
        last_active_at: "2026-09-16T12:00:00Z",
      }),
      thread({
        id: "newer",
        project_id: "pj_b",
        pinned: true,
        pinned_at: "2026-09-16T11:00:00Z",
        last_active_at: "2026-09-16T08:00:00Z",
      }),
    ])
    expect(buckets.pinned.map((t) => t.id)).toEqual(["newer", "older"])
  })

  it("sorts Recents and a project independently so their ranks cannot scramble each other", () => {
    const buckets = sidebarBuckets([
      thread({
        id: "recent-dragged",
        project_id: "",
        sort_rank: 1000,
        last_active_at: "2026-09-16T10:00:00Z",
      }),
      thread({
        id: "recent-new",
        project_id: "",
        sort_rank: 0,
        last_active_at: "2026-09-16T12:00:00Z",
      }),
      thread({
        id: "proj-dragged",
        project_id: "pj_a",
        sort_rank: 1000,
        last_active_at: "2026-09-16T09:00:00Z",
      }),
      thread({
        id: "proj-new",
        project_id: "pj_a",
        sort_rank: 0,
        last_active_at: "2026-09-16T11:00:00Z",
      }),
    ])
    expect(buckets.recents.map((t) => t.id)).toEqual(["recent-new", "recent-dragged"])
    expect(buckets.byProject.pj_a.map((t) => t.id)).toEqual(["proj-new", "proj-dragged"])
  })

  it("does not let an idle unranked row sit above a ranked one that was just used", () => {
    const buckets = sidebarBuckets([
      thread({
        id: "idle",
        project_id: "",
        sort_rank: 0,
        last_active_at: "2026-09-16T03:00:00Z",
      }),
      thread({
        id: "active",
        project_id: "",
        sort_rank: 1000,
        last_active_at: "2026-09-16T08:00:00Z",
      }),
    ])
    expect(buckets.recents.map((t) => t.id)).toEqual(["active", "idle"])
  })
})
