import { describe, expect, it } from "vitest"

import {
  SIDEBAR_PREVIEW_LIMIT,
  SIDEBAR_PREVIEW_MAX_AGE_MS,
  splitSidebarPreview,
} from "./sidebar-preview"
import type { Thread } from "./types"

const NOW = new Date("2026-09-18T12:00:00Z")

function thread(
  partial: Partial<Thread> & { id: string },
  daysAgo = 0,
): Thread {
  const at = new Date(NOW.getTime() - daysAgo * 24 * 60 * 60 * 1000)
  return {
    title: partial.title ?? partial.id,
    project_id: "pj_1",
    provider_id: "default",
    reasoning_effort: "",
    archived: false,
    created_at: at.toISOString(),
    last_active_at: at.toISOString(),
    running: false,
    sort_rank: 0,
    ...partial,
  }
}

describe("splitSidebarPreview", () => {
  it("shows a short recent list in full and has nothing behind More", () => {
    const threads = [thread({ id: "a" }), thread({ id: "b" }), thread({ id: "c" })]
    expect(splitSidebarPreview(threads, { now: NOW })).toEqual({
      visible: threads,
      hidden: [],
    })
  })

  it("hides the sixth recent conversation so a busy folder stays short", () => {
    const threads = Array.from({ length: SIDEBAR_PREVIEW_LIMIT + 2 }, (_, i) =>
      thread({ id: `th_${i}` }, i * 0.1),
    )
    const { visible, hidden } = splitSidebarPreview(threads, { now: NOW })
    expect(visible.map((th) => th.id)).toEqual(
      threads.slice(0, SIDEBAR_PREVIEW_LIMIT).map((th) => th.id),
    )
    expect(hidden.map((th) => th.id)).toEqual(
      threads.slice(SIDEBAR_PREVIEW_LIMIT).map((th) => th.id),
    )
  })

  it("hides a conversation idle longer than a week even when the folder is short", () => {
    const fresh = thread({ id: "fresh" }, 1)
    const stale = thread({ id: "stale" }, 8)
    const { visible, hidden } = splitSidebarPreview([fresh, stale], { now: NOW })
    expect(visible.map((th) => th.id)).toEqual(["fresh"])
    expect(hidden.map((th) => th.id)).toEqual(["stale"])
  })

  it("keeps a conversation on the seventh day and hides it a millisecond later", () => {
    const onCutoff = thread({
      id: "edge",
      last_active_at: new Date(NOW.getTime() - SIDEBAR_PREVIEW_MAX_AGE_MS).toISOString(),
    })
    const justPast = thread({
      id: "past",
      last_active_at: new Date(
        NOW.getTime() - SIDEBAR_PREVIEW_MAX_AGE_MS - 1,
      ).toISOString(),
    })
    expect(splitSidebarPreview([onCutoff], { now: NOW }).hidden).toEqual([])
    expect(splitSidebarPreview([justPast], { now: NOW }).visible).toEqual([])
    expect(splitSidebarPreview([justPast], { now: NOW }).hidden.map((th) => th.id)).toEqual(
      ["past"],
    )
  })

  it("does not let stale rows occupy a preview slot ahead of later fresh ones", () => {
    const threads = [
      thread({ id: "new" }, 0),
      thread({ id: "old" }, 10),
      thread({ id: "also-new" }, 2),
    ]
    const { visible, hidden } = splitSidebarPreview(threads, { now: NOW })
    expect(visible.map((th) => th.id)).toEqual(["new", "also-new"])
    expect(hidden.map((th) => th.id)).toEqual(["old"])
  })

  it("keeps the open conversation in the preview so More cannot hide the row on screen", () => {
    const threads = [
      ...Array.from({ length: SIDEBAR_PREVIEW_LIMIT }, (_, i) =>
        thread({ id: `fresh_${i}` }, i * 0.1),
      ),
      thread({ id: "open" }, 20),
    ]
    const { visible, hidden } = splitSidebarPreview(threads, {
      now: NOW,
      keepIds: ["open"],
    })
    expect(visible.map((th) => th.id)).toContain("open")
    expect(hidden.map((th) => th.id)).not.toContain("open")
    expect(visible).toHaveLength(SIDEBAR_PREVIEW_LIMIT + 1)
  })

  it("does not let an old open conversation steal a slot from a recent one", () => {
    const threads = [
      thread({ id: "open-old" }, 20),
      thread({ id: "fresh" }, 1),
    ]
    const { visible, hidden } = splitSidebarPreview(threads, {
      now: NOW,
      keepIds: ["open-old"],
    })
    expect(visible.map((th) => th.id)).toEqual(["open-old", "fresh"])
    expect(hidden).toEqual([])
  })

  it("treats a missing timestamp as recent so a bad date cannot empty the folder", () => {
    const threads = [thread({ id: "broken", last_active_at: "not-a-date" })]
    expect(splitSidebarPreview(threads, { now: NOW }).visible.map((th) => th.id)).toEqual(
      ["broken"],
    )
  })
})
